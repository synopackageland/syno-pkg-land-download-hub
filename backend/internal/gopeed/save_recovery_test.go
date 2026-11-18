package gopeed

import (
	"context"
	"fmt"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestC3JournalDeadlineMustRemainPendingAndConvergeCommitted(t *testing.T) {
	s, _ := stagedDelegate(t)
	state := "queued"
	moves := 0
	store := filepath.Join(t.TempDir(), "background")
	sdk := recoverySDK(s, store, appExecutor(func(ctx context.Context, op string, _ map[string]string, _, out any) error {
		if ctx.Err() != nil {
			return &api.Error{Code: "canceled"}
		}
		if op == "files.moveFromStaging" {
			moves++
		}
		result := delegate.OperationResult{JobID: "gopeed-mine", State: state}
		if state == "committed" {
			result.Receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
		}
		return encodeResult(out, result)
	}))
	s.Delegate = sdk
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(store, "lock"), os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	s.saveTimeout = 50 * time.Millisecond
	err = s.SaveCompleted(context.Background())
	before := readJob(t, s)
	syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	state = "committed"
	s.saveTimeout = 0
	restarted := New(s.Data)
	restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, sdk
	if err2 := restarted.SaveCompleted(context.Background()); err2 != nil {
		t.Fatal(err2)
	}
	after := readJob(t, restarted)
	t.Logf("SDK error=%v; persisted before=%s/%s; after restart published=%v; raw admissions=%d", err, before.SaveState, before.SaveError, after.Published, moves)
	if before.SaveState != "pending" || !after.Published || moves != 1 {
		t.Fatal(fmt.Sprintf("C3: transient journal deadline froze original committed ref: %+v", after))
	}
}

func TestC3TransientErrorsKeepSameTupleAndOnlyCommittedIsSaved(t *testing.T) {
	for _, code := range []string{"jobs_busy", "jobs_unavailable", "canceled"} {
		t.Run(code, func(t *testing.T) {
			s, f := stagedDelegate(t)
			if err := s.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			original := readJob(t, s)
			f.err = &api.Error{Code: code}
			_ = s.SaveCompleted(context.Background())
			j := readJob(t, s)
			if j.SaveState != "pending" || j.Published || j.Uncertain || j.SaveParams == nil || *j.SaveParams != *original.SaveParams {
				t.Fatal("transient became terminal or changed tuple", j)
			}
			f.err = nil
			f.state = "committed"
			f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
			if err := s.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			j = readJob(t, s)
			if !j.Published || j.SaveError != "" || *j.SaveParams != *original.SaveParams {
				t.Fatal("lost receipt convergence", j)
			}
			n := f.calls
			_ = s.SaveCompleted(context.Background())
			if f.calls != n {
				t.Fatal("committed replay")
			}
		})
	}
}

func TestC3UpgradeRecoversOnlyCompleteOriginalTransientTuples(t *testing.T) {
	for _, mode := range []string{"jobs_unavailable", "jobs_busy", "canceled", "no-params", "no-owner", "no-start", "no-grant", "no-job", "changed-grant", "changed-job", "changed-stage", "changed-name", "changed-size", "unsafe-stage", "APP_DENIED", "DELEGATION_DENIED", "disabled", "operation-not-found", "spool_unsafe", "journal_corrupt", "download_failed", "access_denied", "published"} {
		t.Run(mode, func(t *testing.T) {
			s, f := stagedDelegate(t)
			if err := s.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			original := readJob(t, s)
			original.SaveJobID = "original-legacy-ref"
			original.SaveParams.JobID = original.SaveJobID
			original.SaveState = "save_failed"
			original.SaveError = "jobs_unavailable"
			original.Uncertain = false
			recoverable := mode == "jobs_unavailable" || mode == "jobs_busy" || mode == "canceled"
			switch mode {
			case "jobs_unavailable", "jobs_busy", "canceled", "APP_DENIED", "DELEGATION_DENIED", "disabled", "operation-not-found", "spool_unsafe", "journal_corrupt":
				original.SaveError = mode
			case "no-params":
				original.SaveParams = nil
			case "no-owner":
				original.Owner = ""
			case "no-start":
				original.SaveStarted = false
			case "no-grant":
				original.DelegationID = ""
			case "no-job":
				original.SaveJobID = ""
			case "changed-grant":
				original.SaveParams.DelegationID = "new-grant"
			case "changed-job":
				original.SaveParams.JobID = "new-job"
			case "changed-stage":
				original.SaveParams.Staging = "different/file.bin"
			case "changed-name":
				original.SaveParams.Name = "other.bin"
			case "changed-size":
				original.SaveParams.Size++
			case "unsafe-stage":
				original.Stage = filepath.Dir(s.StagingDir)
			case "download_failed", "access_denied":
				original.SaveState = mode
			case "published":
				original.Published = true
			}
			_, err := s.withState(context.Background(), func(st *State) (any, error) { st.Jobs["mine"] = original; return nil, nil })
			if err != nil {
				t.Fatal(err)
			}
			// Restart the App using only the persisted tuple, never the current grant.
			f.calls = 0
			f.input = delegate.MoveParams{}
			f.state = "queued"
			f.err = &api.Error{Code: "jobs_busy"}
			restarted := New(s.Data)
			restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, f
			_ = restarted.SaveCompleted(context.Background())
			before := readJob(t, restarted)
			if recoverable {
				if before.SaveState != "pending" || before.Published || f.calls != 1 || f.input != *original.SaveParams {
					t.Fatal("legacy transient did not recover with original tuple", before, f.calls, f.input)
				}
				f.err = nil
				f.state = "committed"
				f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
				if err := restarted.SaveCompleted(context.Background()); err != nil {
					t.Fatal(err)
				}
				after := readJob(t, restarted)
				if !after.Published || after.SaveError != "" || after.SaveJobID != original.SaveJobID || *after.SaveParams != *original.SaveParams {
					t.Fatal(after)
				}
			} else if f.calls != 0 || before.SaveState != original.SaveState || before.Published != original.Published || before.SaveError != original.SaveError {
				t.Fatal("revived refusal or fabricated tuple", before, f.calls)
			}
		})
	}
}

func TestC3LegacyFailedSDKRefConvergesWithOneRawAdmission(t *testing.T) {
	s, _ := stagedDelegate(t)
	moves := 0
	state := "queued"
	sdk := recoverySDK(s, filepath.Join(t.TempDir(), "mirror"), appExecutor(func(_ context.Context, op string, _ map[string]string, p, out any) error {
		if op == "files.moveFromStaging" {
			moves++
		}
		result := delegate.OperationResult{JobID: "gopeed-mine", State: state}
		if state == "committed" {
			result.Receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
		}
		return encodeResult(out, result)
	}))
	s.Delegate = sdk
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	original := readJob(t, s)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs["mine"]
		j.SaveState = "save_failed"
		j.SaveError = "jobs_unavailable"
		st.Jobs["mine"] = j
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	state = "committed"
	restarted := New(s.Data)
	restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, sdk
	for range 3 {
		if err := restarted.SaveCompleted(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	after := readJob(t, restarted)
	if !after.Published || after.SaveError != "" || moves != 1 || *after.SaveParams != *original.SaveParams {
		t.Fatal("legacy recovery readmitted or lost receipt", after, moves)
	}
}
