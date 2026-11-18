package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
)

// All unfinished jobs require the owner's SDK grant. Existing published history
// remains readable. No request can select an alternate writer or replay a save.
func (s *Service) publish(ctx context.Context, _ *http.Request, _, owner string, raw json.RawMessage) (any, error) {
	var in struct {
		ID string `json:"id"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	var job Job
	_, err := s.withState(ctx, func(st *State) (any, error) {
		j, ok := st.Jobs[in.ID]
		if !ok || j.Owner != owner {
			return nil, refused("task_denied")
		}
		job = j
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	if job.Published {
		return map[string]bool{"published": true}, nil
	}
	if job.DelegationID == "" {
		return nil, refused("background_destination_required")
	}
	// Retry restarts only failed files, also while other files still save.
	if job.multiFile() {
		if job, err = s.retryFiles(ctx, in.ID, job); err != nil {
			return nil, err
		}
	} else if job.retryable() {
		if job, err = s.retrySave(ctx, in.ID, job); err != nil {
			return nil, err
		}
	}
	return s.saveDelegated(ctx, in.ID, job)
}

// retryable reports whether the owner's publish starts a new attempt: a
// failed file of a multi-file task, or a refused single-file save. A lost
// grant (access_denied) or a refusal that revokes it keeps a single file
// refused.
func (j Job) retryable() bool {
	if j.Published || j.DeleteRequested {
		return false
	}
	if j.multiFile() {
		for _, it := range j.Files {
			if it.State == "save_failed" || it.State == "access_denied" || it.State == "canceled" {
				return true
			}
		}
		return false
	}
	return j.SaveState == "save_failed" && !j.Uncertain && !unretryable(j.SaveError)
}

// unretryable reports refusals that a retry of the same staged file cannot
// fix. SCOPE_DENIED revokes the destination grant, so a retry would only end
// in DELEGATION_DENIED; a retry keeps the too-long name. PERMISSION_DENIED and
// not-found (SDK 0.9.4) and APP_DENIED (SDK 0.9.6) keep the grant and can be
// retried once fixed.
func unretryable(code string) bool {
	return code == "SCOPE_DENIED" || code == "name-too-long"
}

// retrySave forgets a refused single-file operation; the next pass admits a
// new one for the same staged file.
func (s *Service) retrySave(ctx context.Context, id string, job Job) (Job, error) {
	_, err := s.withState(ctx, func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		if !ok || current.Owner != job.Owner {
			return nil, refused("task_denied")
		}
		if !current.retryable() {
			return nil, nil
		}
		current.SaveAttempt++
		current.SaveStarted, current.SaveJobID, current.SaveParams = false, "", nil
		current.SaveState, current.SaveError, current.SaveReason = "", "", ""
		st.Jobs[id] = current
		job = current
		return nil, nil
	})
	return job, err
}
