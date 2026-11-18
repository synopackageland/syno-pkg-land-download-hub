package gopeed

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type Job struct {
	Subfolder        string       `json:"subfolder,omitempty"`
	ControlPending   string       `json:"controlPending,omitempty"`
	SavePausePending bool         `json:"savePausePending,omitempty"`
	Preparation      *Preparation `json:"preparation,omitempty"`
	// Kind is the SourceKind of new non-HTTP tasks; empty means HTTP, as in
	// every record written before multi-file support.
	Kind string `json:"kind,omitempty"`
	// Hash is the BT info hash; one active task per hash shares the engine's
	// torrent storage, so it is also the stage identity.
	Hash string `json:"hash,omitempty"`
	// FileCount is the torrent's file count; Select lists the file indexes
	// the owner chose, nil meaning every file.
	FileCount int   `json:"fileCount,omitempty"`
	Select    []int `json:"select,omitempty"`
	// Files is the durable multi-file publication plan. A job without Files
	// is the original single-file record (Filename, SaveParams, SaveJobID).
	Files []multisave.Item `json:"files,omitempty"`
	// Requests are the file requests of an extension preview, kept only
	// until its files are submitted as their own jobs.
	Requests []extensionFile `json:"requests,omitempty"`
	// Media is the yt-dlp request and run state of Kind "media" tasks.
	Media               *MediaJob `json:"media,omitempty"`
	SaveCancelRequested bool      `json:"saveCancelRequested,omitempty"`
	DeleteRequested     bool      `json:"deleteRequested,omitempty"`
	DeletePhase         string    `json:"deletePhase,omitempty"`
	DeleteKept          bool      `json:"deleteKept,omitempty"`
	DeleteError         string    `json:"deleteError,omitempty"`
	SubmissionRID       string    `json:"submissionRid,omitempty"`
	// BatchKey is the caller's key of the batch link that created the job,
	// and BatchKind the kind the link was reported as.
	BatchKey     string               `json:"batchKey,omitempty"`
	BatchKind    string               `json:"batchKind,omitempty"`
	SaveParams   *delegate.MoveParams `json:"saveParams,omitempty"`
	DelegationID string               `json:"delegationId,omitempty"`
	SaveJobID    string               `json:"saveJobId,omitempty"`
	// SaveAttempt counts the owner's retries of a failed single-file save;
	// each attempt is a new SDK operation.
	SaveAttempt  int    `json:"saveAttempt,omitempty"`
	SaveStarted  bool   `json:"saveStarted,omitempty"`
	SaveState    string `json:"saveState,omitempty"`
	SaveSize     int64  `json:"saveSize,omitempty"`
	SaveError    string `json:"saveError,omitempty"`
	SaveReason   string `json:"saveReason,omitempty"`
	ManualPaused bool   `json:"manualPaused,omitempty"`
	Owner        string `json:"owner"`
	Stage        string `json:"stage"`
	Destination  string `json:"destination"`
	Filename     string `json:"filename"`
	SavedName    string `json:"savedName,omitempty"`
	Published    bool   `json:"published"`
	Uncertain    bool   `json:"uncertain"`
}
type State struct {
	MediaPreferences map[string]MediaPreferences `json:"mediaPreferences,omitempty"`
	Removals         []Removal                   `json:"removals,omitempty"`
	Grants           map[string]string           `json:"grants,omitempty"`
	Jobs             map[string]Job              `json:"jobs"`
	Resolved         map[string]Job              `json:"resolved"`
	Folders          map[string]string           `json:"folders"`
}

func randomID() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	return hex.EncodeToString(b[:])
}
func (s *Service) withState(ctx context.Context, fn func(*State) (any, error)) (any, error) {
	if e := os.MkdirAll(s.Data, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(s.Data, "jobs.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	// Folder permission checks and source resolution can exceed one second on
	// a busy NAS. Keep admission bounded while allowing other users to finish.
	deadline := time.NewTimer(15 * time.Second)
	defer deadline.Stop()
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) {
			return nil, e
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, refused("busy")
		case <-time.After(10 * time.Millisecond):
		}
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	st, e := s.readState()
	if e != nil {
		return nil, e
	}
	before, e := json.Marshal(st)
	if e != nil {
		return nil, e
	}
	result, e := fn(&st)
	if e != nil {
		return nil, e
	}
	b, e := json.Marshal(st)
	if e != nil {
		return nil, e
	}
	// The original UI polls every second. Read-only requests must not replace
	// and fsync the shared state file on every poll.
	if bytes.Equal(before, b) {
		return result, nil
	}
	return result, s.writeState(b)
}

// writeState atomically persists bytes while the caller holds jobs.lock.
func (s *Service) writeState(b []byte) error {
	if len(b) > binaries.MaxJournalBytes {
		return refused("queue_limit")
	}
	tmp, e := os.CreateTemp(s.Data, ".jobs-*")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if e = tmp.Chmod(0600); e == nil {
		_, e = tmp.Write(b)
	}
	if e == nil {
		e = tmp.Sync()
	}
	closeErr := tmp.Close()
	if e == nil {
		e = closeErr
	}
	if e == nil {
		e = os.Rename(tmp.Name(), filepath.Join(s.Data, "jobs.json"))
	}
	if e == nil {
		d, x := os.Open(s.Data)
		if x == nil {
			e = d.Sync()
			d.Close()
		} else {
			e = x
		}
	}
	return e
}
func decodeStrict(raw json.RawMessage, v any) error {
	d := json.NewDecoder(bytesReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return refused("invalid_request")
	}
	return nil
}

// readState loads one complete atomic jobs.json version. Writers still hold
// jobs.lock through writeState's fsync/rename; this helper does not mutate it.
func (s *Service) readState() (State, error) {
	st := State{Jobs: map[string]Job{}, Resolved: map[string]Job{}, Folders: map[string]string{}}
	p := filepath.Join(s.Data, "jobs.json")
	if b, e := os.ReadFile(p); e == nil {
		if len(b) > binaries.MaxJournalBytes || json.Unmarshal(b, &st) != nil || st.Jobs == nil || st.Resolved == nil || st.Folders == nil {
			return st, refused("state_invalid")
		}
	} else if !errors.Is(e, os.ErrNotExist) {
		return st, e
	}
	if st.Grants == nil {
		st.Grants = map[string]string{}
	}
	return st, nil
}
