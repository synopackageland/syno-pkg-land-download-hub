package gopeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// BackgroundSave is the SDK facade, not a second executor or authority store.
type BackgroundSave interface {
	MoveFromStaging(context.Context, delegate.MoveParams) (delegate.OperationAdmission, error)
	OperationStatus(context.Context, map[string]string, delegate.OperationRef) (delegate.OperationResult, error)
	CancelOperation(context.Context, map[string]string, delegate.OperationRef) (delegate.OperationResult, error)
	DiscardStaging(context.Context, delegate.OperationRef) error
}

func operationID(id string) string {
	if len(id) <= 121 {
		return "gopeed-" + id
	}
	sum := sha256.Sum256([]byte(id))
	return "gopeed-" + hex.EncodeToString(sum[:])
}
func (s *Service) saveBudget() time.Duration {
	if s.saveTimeout > 0 {
		return s.saveTimeout
	}
	return api.DefaultRequestTimeout
}

// SaveCompleted gives each job its own bounded budget. Engine locking ends
// before SDK validation/admission/status, so another user's list stays responsive.
func (s *Service) SaveCompleted(ctx context.Context) error {
	lookup, done := context.WithTimeout(ctx, s.saveBudget())
	result := s.ReconcileSubmissions(lookup)
	done()
	jobs := map[string]Job{}
	deletions := map[string]Job{}
	medias := map[string]Job{}
	pauses := map[string]Job{}
	_, err := s.withState(ctx, func(st *State) (any, error) {
		for id, j := range st.Jobs {
			if j.Preparation != nil {
				continue
			}
			if j.DeleteRequested {
				deletions[id] = j
				continue
			}
			if j.SavePausePending || j.ManualPaused {
				if j.SavePausePending {
					pauses[id] = j
				}
				continue
			}
			if j.Media != nil {
				medias[id] = j
				continue
			}
			j = s.recoverLegacySave(j)
			st.Jobs[id] = j
			if j.DelegationID != "" && !j.Published && !terminalSave(j) {
				jobs[id] = j
			}
		}
		return nil, nil
	})
	if err != nil {
		return err
	}
	// Before this pass's deletions: a task removed now is checked on the next pass.
	sweep, done := context.WithTimeout(ctx, s.saveBudget())
	result = errors.Join(result, s.deleteRemovedTasks(sweep))
	done()
	for id, j := range deletions {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		jobCtx, cancel := context.WithTimeout(ctx, s.saveBudget())
		err = s.advanceDeletion(jobCtx, id, j)
		cancel()
		result = errors.Join(result, err)
	}
	for id, j := range pauses {
		work, cancel := context.WithTimeout(ctx, s.saveBudget())
		err = s.advanceSavePause(work, id, j)
		cancel()
		result = errors.Join(result, err)
	}
	for id, j := range medias {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		jobCtx, cancel := context.WithTimeout(ctx, s.saveBudget())
		err = s.saveMedia(jobCtx, id, j)
		cancel()
		result = errors.Join(result, err)
	}
	for id, j := range jobs {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		jobCtx, cancel := context.WithTimeout(ctx, s.saveBudget())
		_, err = s.saveDelegated(jobCtx, id, j)
		cancel()
		result = errors.Join(result, err)
	}
	return result
}

func (s *Service) recordSaveError(ctx context.Context, id string, job Job, err error) (any, error) {
	// CANCEL_REQUESTED is durable intent. Transient reads may not erase it or
	// route the next pass back through admission.
	if job.SaveCancelRequested && retrySave(saveCode(err)) {
		return nil, err
	}
	job.SaveError = saveCode(err)
	job.Uncertain = false
	if retrySave(job.SaveError) {
		job.SaveState = "pending"
	} else {
		job.SaveState = "save_failed"
	}
	if accessDenied(job.SaveError) {
		markAccessDenied(&job)
	}
	persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.saveBudget())
	defer cancel()
	if e := s.recordSave(persist, id, job); e != nil {
		return nil, e
	}
	return nil, err
}

func (s *Service) saveDelegated(ctx context.Context, id string, job Job) (any, error) {
	unlock, err := s.lockSave(ctx, id)
	if err != nil {
		return nil, err
	}
	defer unlock()
	st, err := s.readState()
	if err != nil {
		return nil, err
	}
	current, ok := st.Jobs[id]
	if !ok || current.Owner != job.Owner {
		return nil, refused("task_denied")
	}
	return s.saveDelegatedLocked(ctx, id, current)
}
func (s *Service) saveDelegatedLocked(ctx context.Context, id string, job Job) (any, error) {
	job = s.recoverLegacySave(job)
	if job.ManualPaused || job.SavePausePending {
		return nil, refused("task_paused")
	}
	if job.Published {
		return map[string]bool{"published": true}, nil
	}
	if terminalSave(job) {
		return nil, refused(job.SaveError)
	}
	if s.Delegate == nil {
		return nil, refused("save_unavailable")
	}
	if job.Media != nil {
		return s.publishMedia(ctx, id, job)
	}
	if job.multiFile() {
		return s.saveFiles(ctx, id, job)
	}
	var err error
	job, err = s.prepareDelegated(ctx, id, job)
	if err != nil {
		if saveCode(err) == "download_pending" {
			return map[string]bool{"published": false}, nil
		}
		return nil, err
	}
	if job.Published {
		return map[string]bool{"published": true}, nil
	}
	// Identical params on every pass: only the SDK may recover admission.
	// Upgrade an observed cancellation without changing its reference.
	if job.SaveState == "copying" && job.SaveError == "CANCEL_REQUESTED" {
		job.SaveCancelRequested = true
	}
	if !job.SaveCancelRequested {
		_, err = s.Delegate.MoveFromStaging(ctx, *job.SaveParams)
		if err != nil {
			return s.recordSaveError(ctx, id, job, err)
		}
	}
	out, err := s.Delegate.OperationStatus(ctx, nil, delegate.OperationRef{DelegationID: job.DelegationID, JobID: job.SaveJobID})
	if err != nil {
		return s.recordSaveError(ctx, id, job, err)
	}
	if out.JobID != job.SaveJobID {
		return nil, refused("commit_uncertain")
	}
	job.SaveState = out.State
	job.Uncertain = out.State == "uncertain"
	job.SaveError = out.Code
	job.SaveReason = out.Reason
	if out.State == "copying" && out.Code == "CANCEL_REQUESTED" {
		job.SaveCancelRequested = true
	}
	switch out.State {
	case "committed":
		if !committedSave(job, out) {
			job.SaveState = "uncertain"
			job.Uncertain = true
			job.SaveError = "commit_uncertain"
		} else {
			job.Published = true
			job.SavedName = path.Base(out.Receipt.Path)
			job.Uncertain = false
			job.SaveError = ""
		}
	case "queued", "copying", "canceled", "interrupted", "uncertain":
		if out.Receipt != nil {
			job.SaveState = "uncertain"
			job.Uncertain = true
			job.SaveError = "commit_uncertain"
		}
	default:
		job.SaveState = "uncertain"
		job.Uncertain = true
		job.SaveError = "commit_uncertain"
	}
	// Only a terminal protected result proves that publishing stopped. An
	// uncertain result must keep querying the original reference for B3 recovery.
	if !job.Uncertain && out.State == "interrupted" && out.Code != "EXECUTOR_PAUSED" {
		if job.SaveError == "" {
			job.SaveError = "save_interrupted"
		}
		job.SaveState = "save_failed"
		if accessDenied(job.SaveError) {
			markAccessDenied(&job)
		}
	}
	if err = s.recordSave(ctx, id, job); err != nil {
		return nil, refused("commit_uncertain")
	}
	return map[string]any{"published": job.Published, "state": job.SaveState}, nil
}

func (s *Service) prepareDelegated(ctx context.Context, id string, expected Job) (Job, error) {
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
		job = s.recoverLegacySave(job)
		st.Jobs[id] = job
		return nil, nil
	})
	if err != nil {
		return job, err
	}
	if job.Published {
		return job, nil
	}
	if terminalSave(job) {
		return job, refused(job.SaveError)
	}
	if !job.SaveStarted {
		if job.SaveError != "" {
			return job, refused(job.SaveError)
		}
		rep, err := s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
		if err != nil {
			return job, err
		}
		var task engineCompletion
		if rep.Code != 0 || json.Unmarshal(rep.Data, &task) != nil || task.Status == "" {
			return job, refused("engine_unavailable")
		}
		if task.Status == "error" {
			_, err := s.recordIncompleteDownload(ctx, id, job)
			return job, err
		}
		if task.Status != "done" {
			return job, refused("download_pending")
		}
		if job.Uncertain {
			return job, refused("commit_uncertain")
		}
		if err := s.relocateLegacyStage(id, &job); err != nil {
			return job, err
		}
		rel, err := filepath.Rel(s.StagingDir, filepath.Join(job.Stage, job.Filename))
		if err != nil || rel == ".." || strings.HasPrefix(rel, "../") || filepath.IsAbs(rel) || !validName(job.Filename) {
			return job, refused("unsafe_file")
		}
		info, err := os.Lstat(filepath.Join(job.Stage, job.Filename))
		if err != nil || !info.Mode().IsRegular() || info.Size() > delegate.MaxMoveBytes {
			return job, refused("unsafe_file")
		}
		if err = s.verifyCompletion(ctx, id, task, info.Size()); err != nil {
			if saveCode(err) == "download_incomplete" {
				_, err := s.recordIncompleteDownload(ctx, id, job)
				return job, err
			}
			return job, err
		}
		job.SaveStarted = true
		job.SaveJobID = operationID(id)
		if job.SaveAttempt > 0 {
			job.SaveJobID += ".a" + strconv.Itoa(job.SaveAttempt)
		}
		job.SaveSize = info.Size()
		job.SaveState = "pending"
		job.Uncertain = false
		job.SaveParams = &delegate.MoveParams{DelegationID: job.DelegationID, JobID: job.SaveJobID, Staging: filepath.ToSlash(rel), Name: job.destinationName(job.Filename), Size: job.SaveSize, Conflict: "rename"}
		// Persist the exact tuple before IPC. SDK owns recovery and deduplication.
		if err = s.recordSave(ctx, id, job); err != nil {
			return job, err
		}
	}
	// Upgrade pre-C1 intents without changing the original tuple or reference.
	if job.SaveParams == nil {
		rel, err := filepath.Rel(s.StagingDir, filepath.Join(job.Stage, job.Filename))
		if err != nil {
			return job, refused("unsafe_file")
		}
		job.SaveParams = &delegate.MoveParams{DelegationID: job.DelegationID, JobID: job.SaveJobID, Staging: filepath.ToSlash(rel), Name: job.destinationName(job.Filename), Size: job.SaveSize}
		if err = s.recordSave(ctx, id, job); err != nil {
			return job, err
		}
	}
	return job, nil
}

func (s *Service) recordSave(ctx context.Context, id string, job Job) error {
	_, err := s.withState(ctx, func(st *State) (any, error) {
		current, ok := st.Jobs[id]
		if !ok || current.Owner != job.Owner || current.DelegationID != job.DelegationID {
			return nil, refused("task_denied")
		}
		// A concurrent pass must not regress a durable receipt or refusal.
		if current.DeleteRequested {
			return nil, refused("task_deleting")
		}
		if current.SavePausePending || current.ManualPaused || current.SaveAttempt != job.SaveAttempt {
			return nil, refused("task_paused")
		}
		if current.Published || terminalSave(current) {
			return nil, nil
		}
		current.SaveCancelRequested = current.SaveCancelRequested || job.SaveCancelRequested
		current.SaveParams = job.SaveParams
		current.SaveStarted = job.SaveStarted
		current.SaveJobID = job.SaveJobID
		current.SaveSize = job.SaveSize
		current.SaveState = job.SaveState
		current.Published = job.Published
		current.Uncertain = job.Uncertain
		current.SaveError = job.SaveError
		current.SaveReason = job.SaveReason
		current.SavedName = job.SavedName
		current.Stage = job.Stage
		current.Files = job.Files
		st.Jobs[id] = current
		return nil, nil
	})
	return err
}

func committedSave(job Job, out delegate.OperationResult) bool {
	p := job.SaveParams
	if p == nil {
		p = &delegate.MoveParams{JobID: job.SaveJobID, Name: job.destinationName(job.Filename), Size: job.SaveSize}
	}
	return multisave.CommittedReceipt(job.Destination, *p, out)
}
