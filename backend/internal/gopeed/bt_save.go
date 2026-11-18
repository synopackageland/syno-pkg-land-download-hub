package gopeed

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"dsmapp/internal/binaries"
	"dsmapp/internal/multisave"

	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// multiFile reports jobs published file by file through multisave.
func (j Job) multiFile() bool {
	return j.Hash != "" || len(j.Files) > 0
}

// batch is the job's multisave identity; Stage is relative to the SDK root.
func (s *Service) batch(id string, job Job) (multisave.Batch, error) {
	rel, err := filepath.Rel(s.StagingDir, job.Stage)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) {
		return multisave.Batch{}, refused("unsafe_file")
	}
	return multisave.Batch{Task: id, DelegationID: job.DelegationID, Destination: job.Destination, Subfolder: job.Subfolder, Stage: filepath.ToSlash(rel)}, nil
}

func summarizeFiles(job *Job) {
	sum := multisave.Summarize(job.Files)
	job.SaveState, job.Published = sum.State, sum.Published
	job.SaveError, job.SaveReason = sum.Code, sum.Reason
	job.Uncertain = sum.State == "uncertain"
}

// saveFiles publishes every file of a completed multi-file task. Each pass
// admits or queries only unfinished files; saved files are never sent again.
func (s *Service) saveFiles(ctx context.Context, id string, job Job) (any, error) {
	job, err := s.prepareFiles(ctx, id, job)
	if err != nil {
		if saveCode(err) == "download_pending" {
			return map[string]bool{"published": false}, nil
		}
		return nil, err
	}
	b, err := s.batch(id, job)
	if err != nil {
		return nil, err
	}
	job.Files = multisave.Advance(ctx, s.Delegate, b, job.Files)
	summarizeFiles(&job)
	if err = s.recordSave(context.WithoutCancel(ctx), id, job); err != nil {
		return nil, refused("commit_uncertain")
	}
	if job.Published {
		// Only unchosen partial pieces remain; the torrent stays for history.
		_ = os.RemoveAll(job.Stage)
	}
	return map[string]any{"published": job.Published, "state": job.SaveState}, nil
}

type btTask struct {
	Status    string `json:"status"`
	Uploading bool   `json:"uploading"`
	Meta      struct {
		Opts struct {
			SelectFiles []int `json:"selectFiles"`
		} `json:"opts"`
		Res btResource `json:"res"`
	} `json:"meta"`
}

// prepareFiles plans the publication once the engine has finished the task,
// including seeding, and every file has its exact size.
func (s *Service) prepareFiles(ctx context.Context, id string, expected Job) (Job, error) {
	var job Job
	unlock, err := (binaries.Manager{Data: s.Data}).Lock(ctx)
	if err != nil {
		return job, refused("engine_busy")
	}
	defer unlock()
	if (binaries.Manager{Data: s.Data}).Pending() {
		return job, refused("engine_busy")
	}
	_, err = s.withState(ctx, func(st *State) (any, error) {
		var ok bool
		job, ok = st.Jobs[id]
		if !ok || job.Owner != expected.Owner || job.DelegationID != expected.DelegationID {
			return nil, refused("task_denied")
		}
		if job.DeleteRequested {
			return nil, refused("task_deleting")
		}
		return nil, nil
	})
	if err != nil || job.SaveStarted {
		return job, err
	}
	rep, err := s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
	if err != nil {
		return job, err
	}
	var task btTask
	if rep.Code != 0 || json.Unmarshal(rep.Data, &task) != nil || task.Status == "" {
		return job, refused("engine_unavailable")
	}
	if task.Status == "error" {
		_, err = s.recordIncompleteDownload(ctx, id, job)
		return job, err
	}
	if task.Status != "done" || task.Uploading {
		if task.Status != "done" && job.Select != nil && !slices.Equal(task.Meta.Opts.SelectFiles, job.Select) {
			s.applySelection(ctx, id, job)
		}
		return job, refused("download_pending")
	}
	if !strings.EqualFold(task.Meta.Res.Hash, job.Hash) || len(task.Meta.Res.Files) != job.FileCount {
		return job, refused("unsafe_file")
	}
	items, err := btPlan(task.Meta.Res, job.Select)
	if err != nil {
		return job, refused("unsafe_file")
	}
	// The engine may report done before its storage renames a finished
	// ".part" file, so a missing or short file is waited for, not failed.
	for _, it := range items {
		file := filepath.Join(job.Stage, filepath.FromSlash(it.Source))
		if it.Size == 0 {
			restoreEmptyFile(file)
		}
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() != it.Size {
			return job, refused("download_pending")
		}
	}
	job.Files = items
	job.SaveStarted = true
	job.SaveState = "pending"
	job.Uncertain = false
	// Persist every file's tuple before IPC; operation IDs derive from it.
	return job, s.recordSave(ctx, id, job)
}

// restoreEmptyFile recreates a zero-length file the engine created only when
// it opened the torrent, before a repeated resolve cleared the stage.
func restoreEmptyFile(file string) {
	if os.MkdirAll(filepath.Dir(file), 0700) != nil {
		return
	}
	if f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600); err == nil {
		f.Close()
	}
}

// retryFiles starts a fresh attempt for the owner's terminally failed files.
func (s *Service) retryFiles(ctx context.Context, id string, job Job) (Job, error) {
	_, err := s.withState(ctx, func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		if !ok || current.Owner != job.Owner || current.DeleteRequested {
			return nil, refused("task_denied")
		}
		items, changed := multisave.Retry(current.Files)
		if !changed {
			return nil, nil
		}
		current.Files = items
		current.SaveState, current.SaveError, current.SaveReason, current.Uncertain = "pending", "", "", false
		st.Jobs[id] = current
		job = current
		return nil, nil
	})
	return job, err
}

// cancelFiles advances a deletion of a multi-file task, keeping saved files.
func (s *Service) cancelFiles(ctx context.Context, id string, job Job) (Job, error) {
	b, err := s.batch(id, job)
	if err != nil {
		return job, err
	}
	items, done, kept, err := multisave.Cancel(ctx, s.Delegate, b, job.Files)
	job.Files = items
	job.DeleteKept = kept
	if err != nil {
		job.DeleteError = saveCode(err)
		return job, err
	}
	job.DeleteError = s.discardFiles(ctx, b, items)
	job.DeletePhase = "poll"
	if done && job.DeleteError == "" {
		job.DeletePhase = "engine"
	}
	return job, nil
}

// discardFiles asks the SDK to remove the staging file of every file whose
// move stopped without a write; it answers the first code that keeps the task
// deleting (see discardStaging).
func (s *Service) discardFiles(ctx context.Context, b multisave.Batch, items []multisave.Item) string {
	for i, it := range items {
		switch it.State {
		case "canceled", "save_failed", "access_denied":
			if code := s.discardStaging(ctx, delegate.OperationRef{DelegationID: b.DelegationID, JobID: multisave.OperationID(b.Task, i, it.Attempt)}); code != "" {
				return code
			}
		}
	}
	return ""
}

// dropBTData removes a deleted task's private stage and torrent unless another
// task or preview still refers to the same hash.
func (s *Service) dropBTData(st *State, job Job) {
	if job.Hash == "" || job.Stage != filepath.Join(s.StagingDir, "bt-"+job.Hash) {
		return
	}
	for _, j := range st.Jobs {
		if j.Hash == job.Hash {
			return
		}
	}
	_ = os.RemoveAll(job.Stage)
	for _, j := range st.Resolved {
		if j.Hash == job.Hash {
			return
		}
	}
	_ = os.Remove(s.torrentPath(job.Hash))
}

// presentFiles is the owner's per-file view; staging paths stay private.
func presentFiles(task map[string]any, job Job) {
	files := make([]map[string]any, 0, len(job.Files))
	for _, it := range job.Files {
		files = append(files, map[string]any{"name": revealItemName(it), "rel": it.Rel, "size": it.Size, "state": it.State, "error": it.Code})
	}
	task["publication"].(map[string]any)["files"] = files
	if meta, ok := task["meta"].(map[string]any); ok && job.Hash != "" {
		if req, ok := meta["req"].(map[string]any); ok && job.Kind != string(SourceMagnet) {
			req["url"] = ""
		}
	}
}

// revealName is a saved file the owner can open in File Station.
func revealName(job Job) string {
	for _, it := range job.Files {
		if it.State == "committed" {
			return revealItemName(it)
		}
	}
	if job.SavedName != "" {
		return job.SavedName
	}
	return job.Filename
}

// publishMedia runs the media job's own save pass for an owner request.
func (s *Service) publishMedia(ctx context.Context, id string, job Job) (any, error) {
	if err := s.saveMediaLocked(ctx, id, job); err != nil {
		return nil, err
	}
	st, err := s.readState()
	if err != nil {
		return nil, err
	}
	job = st.Jobs[id]
	return map[string]any{"published": job.Published, "state": job.SaveState}, nil
}

func revealItemName(it multisave.Item) string {
	if it.SavedName != "" {
		return it.SavedName
	}
	return it.Name
}
