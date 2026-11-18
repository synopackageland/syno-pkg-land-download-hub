package gopeed

import (
	"context"
	"encoding/json"
	"path"

	"dsmapp/internal/binaries"
	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// controlTask keeps pause intent durable before IPC. It does not wait for source
// resolution or hold jobs.lock while waiting for an executor cancellation.
func (s *Service) controlTask(ctx context.Context, owner, id, op string) (any, error) {
	var job Job
	_, err := s.withState(ctx, func(st *State) (any, error) {
		j, ok := st.Jobs[id]
		if !ok || j.Owner != owner {
			return nil, refused("task_denied")
		}
		if j.DeleteRequested {
			return nil, refused("task_deleting")
		}
		if j.Preparation != nil {
			return s.preparationRequest(st, id, j, Input{Method: "PUT"}, "/"+op)
		}
		if j.Media != nil && !j.SaveStarted && j.Media.Status != "done" {
			return s.mediaRequest(st, id, j, Input{Method: "PUT"}, "/"+op)
		}
		wasPaused := j.ManualPaused
		j.ManualPaused = op == "pause"
		if j.SaveStarted && !j.Published && (op == "pause" || wasPaused || j.SavePausePending) {
			j.SavePausePending = true
		}
		if j.Media == nil && (!j.SaveStarted || j.Published) && (!j.Published || j.Hash != "") {
			j.ControlPending = op
		}
		st.Jobs[id] = j
		job = j
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if job.Owner == "" {
		return Reply{Data: json.RawMessage(`true`)}, nil
	}
	if job.SaveStarted && !job.Published && (job.ManualPaused || job.SavePausePending) {
		err = s.advanceSavePause(ctx, id, job)
		return Reply{Data: json.RawMessage(`true`)}, err
	}
	if job.Media != nil || (job.Published && job.Hash == "") {
		return Reply{Data: json.RawMessage(`true`)}, nil
	}
	return s.advanceEngineControl(ctx, id)
}

// advanceSavePause uses cancellation, retaining private staging. Resume starts a
// fresh copy only after protected terminal cancellation; an uncertain copy keeps
// its reference. Already committed files are retained and never copied again.
func (s *Service) advanceSavePause(ctx context.Context, id string, j Job) error {
	unlock, err := s.lockSave(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.readState()
	if err != nil {
		return err
	}
	cur, ok := st.Jobs[id]
	if !ok || cur.DeleteRequested {
		return nil
	}
	j = cur
	if !j.SavePausePending {
		return nil
	}

	if s.Delegate == nil {
		return refused("save_unavailable")
	}
	if j.multiFile() {
		b, err := s.batch(id, j)
		if err != nil {
			return err
		}
		items, done, _, err := multisave.Cancel(ctx, s.Delegate, b, j.Files)
		_, persistErr := s.withState(context.WithoutCancel(ctx), func(st *State) (any, error) {
			cur, ok := st.Jobs[id]
			if !ok || cur.DeleteRequested {
				return nil, nil
			}
			cur.Files = items
			cur.SavePausePending = !done
			if done && !cur.ManualPaused {
				cur.Files, _ = multisave.Retry(cur.Files)
			}
			summarizeFiles(&cur)
			st.Jobs[id] = cur
			return nil, nil
		})
		if err != nil {
			return err
		}
		return persistErr
	}
	if j.SaveJobID == "" {
		return nil
	}
	ref := delegate.OperationRef{DelegationID: j.DelegationID, JobID: j.SaveJobID}
	out, err := s.Delegate.CancelOperation(ctx, nil, ref)
	if err != nil {
		return err
	}
	if out.JobID != ref.JobID {
		return refused("EXECUTOR_PROTOCOL")
	}
	terminal := false
	switch out.State {
	case "committed":
		if !committedSave(j, out) {
			return refused("EXECUTOR_PROTOCOL")
		}
		j.Published = true
		j.SavedName = path.Base(out.Receipt.Path)
		terminal = true
	case "canceled", "interrupted":
		if out.Receipt != nil {
			return refused("EXECUTOR_PROTOCOL")
		}
		terminal = true
	}
	_, err = s.withState(context.WithoutCancel(ctx), func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if !ok || cur.DeleteRequested || cur.SaveJobID != j.SaveJobID {
			return nil, nil
		}
		cur.SavePausePending = !terminal
		cur.Published = j.Published
		cur.SavedName = j.SavedName
		cur.SaveState, cur.SaveError, cur.SaveReason = out.State, out.Code, out.Reason
		if terminal && !cur.ManualPaused && !cur.Published {
			cur.SaveAttempt++
			cur.SaveStarted = false
			cur.SaveJobID = ""
			cur.SaveParams = nil
			cur.SaveState, cur.SaveError, cur.SaveReason = "", "", ""
			cur.SaveCancelRequested = false
		}
		st.Jobs[id] = cur
		return nil, nil
	})
	return err
}

func (s *Service) advanceEngineControl(ctx context.Context, id string) (any, error) {
	unlock, err := (binaries.Manager{Data: s.Data}).Lock(ctx)
	if err != nil {
		return nil, refused("engine_busy")
	}
	defer unlock()
	if (binaries.Manager{Data: s.Data}).Pending() {
		return nil, refused("engine_busy")
	}
	st, err := s.readState()
	if err != nil {
		return nil, err
	}
	j, ok := st.Jobs[id]
	if !ok || j.ControlPending == "" || j.DeleteRequested {
		return Reply{Data: json.RawMessage(`true`)}, nil
	}
	// A completed transfer waiting for its first save has no engine work to stop.
	rep, err := s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
	if err != nil {
		return nil, err
	}
	var task struct {
		Status    string `json:"status"`
		Uploading bool   `json:"uploading"`
	}
	if rep.Code != 0 || json.Unmarshal(rep.Data, &task) != nil {
		return nil, refused("engine_unavailable")
	}
	if task.Status != "done" || task.Uploading {
		rep, err = s.engine(ctx, "PUT", "/api/v1/tasks/"+id+"/"+j.ControlPending, nil, nil)
		if err != nil || rep.Code != 0 {
			return rep, err
		}
	}
	_, err = s.withState(ctx, func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if ok && cur.ControlPending == j.ControlPending {
			cur.ControlPending = ""
			st.Jobs[id] = cur
		}
		return nil, nil
	})
	return Reply{Data: json.RawMessage(`true`)}, err
}

// AdvanceControls retries lost engine acknowledgements on the daemon tick.
func (s *Service) AdvanceControls(ctx context.Context) error {
	st, err := s.readState()
	if err != nil {
		return err
	}
	for id, j := range st.Jobs {
		if j.ControlPending != "" {
			work, cancel := context.WithTimeout(ctx, s.saveBudget())
			_, err = s.advanceEngineControl(work, id)
			cancel()
			if err != nil {
				return err
			}
		}
	}
	return nil
}
