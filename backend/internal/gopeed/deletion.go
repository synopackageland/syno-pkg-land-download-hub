package gopeed

import (
	"context"
	"dsmapp/internal/binaries"
	"encoding/json"
	"errors"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"net/url"
)

// Removal is a bounded, owner-scoped completion notice, never write authority.
type Removal struct {
	ID       string `json:"id"`
	Owner    string `json:"owner"`
	Filename string `json:"filename"`
	FileKept bool   `json:"fileKept"`
}

func deletionReply(pending, kept bool) Reply {
	data, _ := json.Marshal(map[string]bool{"deleting": pending, "fileKept": kept})
	return Reply{Code: 0, Data: data}
}
func (s *Service) deleteTask(ctx context.Context, owner, id string) (any, error) {
	var job Job
	_, err := s.withState(ctx, func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		if !ok || current.Owner != owner {
			return nil, refused("task_denied")
		}
		if !current.DeleteRequested {
			current.DeleteRequested = true
			current.DeletePhase = "cancel"
			if !current.SaveStarted || current.DelegationID == "" || current.Published {
				current.DeletePhase = "engine"
				current.DeleteKept = current.Published
			}
			st.Jobs[id] = current
		}
		job = current
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if job.Preparation != nil {
		return deletionReply(true, false), nil
	}
	// Persist intent before IPC, and never hold jobs/engine locks while canceling.
	_ = s.advanceDeletion(ctx, id, job)
	var pending, kept bool
	_, err = s.withState(context.WithoutCancel(ctx), func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		pending = ok
		kept = current.DeleteKept
		if !ok {
			for _, r := range st.Removals {
				if r.ID == id && r.Owner == owner {
					kept = r.FileKept
				}
			}
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return deletionReply(pending, kept), nil
}
func (s *Service) persistDeletion(ctx context.Context, id string, job Job) error {
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.saveBudget())
	defer cancel()
	_, err := s.withState(persist, func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		if !ok {
			return nil, nil
		}
		if !current.DeleteRequested || current.Owner != job.Owner || current.SaveJobID != job.SaveJobID || current.DelegationID != job.DelegationID {
			return nil, refused("task_denied")
		}
		if current.DeletePhase != "engine" {
			// A concurrent timeout cannot undo a successful cancellation acknowledgement.
			if job.DeletePhase != "cancel" || current.DeletePhase != "poll" {
				current.DeletePhase = job.DeletePhase
			}
			current.DeleteKept = job.DeleteKept
			current.DeleteError = job.DeleteError
			if job.Files != nil {
				current.Files = job.Files
			}
			st.Jobs[id] = current
		}
		return nil, nil
	})
	return err
}
func (s *Service) advanceDeletion(ctx context.Context, id string, job Job) error {
	if job.Media != nil {
		return s.advanceMediaDeletion(ctx, id, job)
	}
	if job.DeletePhase != "engine" {
		if s.Delegate == nil {
			job.DeleteError = "save_unavailable"
			return s.persistDeletion(ctx, id, job)
		}
		if job.multiFile() {
			job, err := s.cancelFiles(ctx, id, job)
			if e := s.persistDeletion(ctx, id, job); e != nil || err != nil {
				return errors.Join(e, err)
			}
			if job.DeletePhase != "engine" {
				return nil
			}
			return s.finishDeletion(ctx, id, job)
		}
		ref := delegate.OperationRef{DelegationID: job.DelegationID, JobID: job.SaveJobID}
		var out delegate.OperationResult
		var err error
		if job.DeletePhase == "poll" {
			out, err = s.Delegate.OperationStatus(ctx, nil, ref)
		} else {
			out, err = s.Delegate.CancelOperation(ctx, nil, ref)
		}
		if err != nil {
			job.DeleteError = saveCode(err)
			if e := s.persistDeletion(ctx, id, job); e != nil {
				return e
			}
			return err
		}
		if out.JobID != job.SaveJobID {
			job.DeleteError = "EXECUTOR_PROTOCOL"
			return s.persistDeletion(ctx, id, job)
		}
		job.DeleteError = ""
		switch out.State {
		case "canceled", "interrupted":
			job.DeletePhase = "engine"
		case "committed":
			if !committedSave(job, out) {
				job.DeleteError = "EXECUTOR_PROTOCOL"
				break
			}
			job.DeletePhase = "engine"
			job.DeleteKept = true
		case "uncertain":
			job.DeletePhase = "poll"
		case "copying":
			if out.Code == "CANCEL_REQUESTED" || job.DeletePhase == "poll" {
				job.DeletePhase = "poll"
			} else {
				job.DeleteError = "EXECUTOR_PROTOCOL"
			}
		case "queued": // No terminal evidence yet; retry cancellation of this reference.
		default:
			job.DeleteError = "EXECUTOR_PROTOCOL"
		}
		if job.DeletePhase == "engine" && !job.DeleteKept {
			if code := s.discardStaging(ctx, ref); code != "" {
				job.DeletePhase, job.DeleteError = "poll", code
			}
		}
		if err = s.persistDeletion(ctx, id, job); err != nil {
			return err
		}
		if job.DeletePhase != "engine" {
			return nil
		}
	}
	return s.finishDeletion(ctx, id, job)
}

// discardStaging asks the SDK to remove the staging file a stopped move kept.
// It answers the code that keeps the task deleting: the move may still copy
// (operation-active), its result is unknown (uncertain), or the SDK could not
// answer. A file another pending job uses (STAGING_IN_USE) or a move the SDK
// never recorded leaves nothing for this task to discard.
func (s *Service) discardStaging(ctx context.Context, ref delegate.OperationRef) string {
	err := s.Delegate.DiscardStaging(ctx, ref)
	if err == nil {
		return ""
	}
	switch code := saveCode(err); code {
	case "STAGING_IN_USE", "operation-not-found":
		return ""
	default:
		return code
	}
}
func (s *Service) finishDeletion(ctx context.Context, id string, job Job) error {
	unlock, err := (binaries.Manager{Data: s.Data}).Lock(ctx)
	if err != nil {
		return refused("engine_busy")
	}
	defer unlock()
	if (binaries.Manager{Data: s.Data}).Pending() {
		return refused("engine_busy")
	}
	_, err = s.withState(ctx, func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		if !ok {
			return nil, nil
		}
		if current.Owner != job.Owner || !current.DeleteRequested || current.DeletePhase != "engine" {
			return nil, refused("task_denied")
		}
		rep, e := s.engine(ctx, "DELETE", "/api/v1/tasks/"+id, url.Values{"force": []string{"false"}}, nil)
		if e != nil || rep.Code != 0 {
			// The DELETE reply can be lost. Confirm absence through a successful list,
			// never infer it from a transport error or an arbitrary engine error code.
			list, readErr := s.engine(ctx, "GET", "/api/v1/tasks", nil, nil)
			var tasks []struct {
				ID string `json:"id"`
			}
			if readErr != nil || list.Code != 0 || json.Unmarshal(list.Data, &tasks) != nil {
				return nil, refused("engine_unavailable")
			}
			for _, task := range tasks {
				if task.ID == id {
					return nil, refused("engine_unavailable")
				}
			}
		}
		st.Removals = append(st.Removals, Removal{ID: id, Owner: current.Owner, Filename: current.Filename, FileKept: current.DeleteKept})
		if len(st.Removals) > 128 {
			st.Removals = st.Removals[len(st.Removals)-128:]
		}
		delete(st.Jobs, id)
		s.dropBTData(st, current)
		s.dropUnsavedStage(st, current)
		s.removeEmptyStage(st, current.Stage)
		return nil, nil
	})
	return err
}

// deleteRemovedTasks deletes again each engine task the App already removed
// (a Removal with no job) that the engine still lists. The engine can
// acknowledge a DELETE and keep the task: a download cleared while it starts
// is stored again, paused, when its resource resolves. Nobody owns it then,
// so no list shows it, yet it holds every core update. Only removed ids are
// deleted, with force=false as the first deletion was.
func (s *Service) deleteRemovedTasks(ctx context.Context) error {
	removed := map[string]bool{}
	_, err := s.withState(ctx, func(st *State) (any, error) {
		for _, r := range st.Removals {
			if _, live := st.Jobs[r.ID]; !live && engineTaskID.MatchString(r.ID) {
				removed[r.ID] = true
			}
		}
		return nil, nil
	})
	if err != nil || len(removed) == 0 {
		return err
	}
	unlock, err := (binaries.Manager{Data: s.Data}).Lock(ctx)
	if err != nil {
		return refused("engine_busy")
	}
	defer unlock()
	if (binaries.Manager{Data: s.Data}).Pending() {
		return refused("engine_busy")
	}
	list, err := s.engine(ctx, "GET", "/api/v1/tasks", nil, nil)
	var tasks []struct {
		ID string `json:"id"`
	}
	if err != nil || list.Code != 0 || json.Unmarshal(list.Data, &tasks) != nil {
		return refused("engine_unavailable")
	}
	var result error
	for _, task := range tasks {
		if !removed[task.ID] {
			continue
		}
		if rep, e := s.engine(ctx, "DELETE", "/api/v1/tasks/"+task.ID, url.Values{"force": []string{"false"}}, nil); e != nil || rep.Code != 0 {
			result = errors.Join(result, refused("engine_unavailable"))
		}
	}
	return result
}
