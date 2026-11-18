package multisave

import (
	"context"
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// Mover is the SDK delegate facade; gopeed.BackgroundSave satisfies it.
type Mover interface {
	MoveFromStaging(context.Context, delegate.MoveParams) (delegate.OperationAdmission, error)
	OperationStatus(context.Context, map[string]string, delegate.OperationRef) (delegate.OperationResult, error)
	CancelOperation(context.Context, map[string]string, delegate.OperationRef) (delegate.OperationResult, error)
}

// Batch identifies one task's publication. Stage is the task's stage folder
// relative to the SDK staging root; Destination is the grant's folder, used
// only to verify receipts.
type Batch struct {
	Subfolder    string
	Task         string
	DelegationID string
	Destination  string
	Stage        string
}

// Summary is the task-level state derived from its items.
type Summary struct {
	// State is committed when every file is saved, save_failed or
	// access_denied when no file can progress without Retry, otherwise the
	// least advanced in-progress state.
	State     string
	Published bool
	Total     int
	Committed int
	Failed    int
	Code      string
	Reason    string
}

// Code reports a stable SDK/API error code, as the single-file save does.
func Code(err error) string {
	var a *api.Error
	if errors.As(err, &a) {
		return a.Code
	}
	return "save_unavailable"
}

// Retryable reports codes that leave a file pending for the next pass.
func Retryable(code string) bool {
	switch code {
	case "EXECUTOR_UNAVAILABLE", "EXECUTOR_TIMEOUT", "EXECUTOR_PROTOCOL", "canceled", "EXECUTOR_PAUSED", "EXECUTOR_STARTING", "queue-full", "STORAGE_ERROR", "ADMISSION_PENDING", "jobs_busy", "jobs_unavailable":
		return true
	}
	return false
}

// AccessDenied reports codes that mean the owner lost the destination.
// APP_DENIED keeps the grant (SDK 0.9.6) and fails only that file.
func AccessDenied(code string) bool {
	return code == "DELEGATION_DENIED"
}

func terminal(state string) bool {
	return state == "committed" || state == "save_failed" || state == "access_denied" || state == "canceled"
}

// Params is the exact, repeatable SDK request of item i.
func Params(b Batch, i int, it Item) delegate.MoveParams {
	name := it.Name
	if b.Subfolder != "" {
		name = b.Subfolder + "/" + name
	}
	return delegate.MoveParams{DelegationID: b.DelegationID, JobID: OperationID(b.Task, i, it.Attempt), Staging: path.Join(b.Stage, it.Source), Name: name, Size: it.Size, Conflict: it.Conflict}
}

func failed(it Item, code string) Item {
	it.Code = code
	switch {
	case AccessDenied(code):
		it.State = "access_denied"
	case Retryable(code):
		it.State = "pending"
	default:
		it.State = "save_failed"
	}
	return it
}

// Advance admits and queries every unfinished file once, in order. Repeating
// it is safe: the SDK deduplicates identical parameters by operation ID, and
// committed or failed files are never sent again. It stops early when ctx
// ends and returns the items with every observed result.
func Advance(ctx context.Context, m Mover, b Batch, items []Item) []Item {
	out := append([]Item(nil), items...)
	for i, it := range out {
		if terminal(it.State) {
			continue
		}
		if ctx.Err() != nil {
			break
		}
		p := Params(b, i, it)
		if _, err := m.MoveFromStaging(ctx, p); err != nil {
			out[i] = failed(it, Code(err))
			continue
		}
		res, err := m.OperationStatus(ctx, nil, delegate.OperationRef{DelegationID: p.DelegationID, JobID: p.JobID})
		if err != nil {
			out[i] = failed(it, Code(err))
			continue
		}
		out[i] = observe(it, p, b.Destination, res)
	}
	return out
}

func observe(it Item, p delegate.MoveParams, dest string, res delegate.OperationResult) Item {
	it.State, it.Code, it.Reason = res.State, res.Code, res.Reason
	uncertain := false
	switch res.State {
	case "committed":
		uncertain = res.JobID != p.JobID || !CommittedReceipt(dest, p, res)
		if !uncertain {
			it.Code, it.Reason = "", ""
			it.SavedName = path.Base(res.Receipt.Path)
		}
	case "queued", "copying", "uncertain":
		uncertain = res.Receipt != nil
	case "canceled", "interrupted":
		// Only a terminal protected result without receipt proves no write.
		uncertain = res.Receipt != nil
		if !uncertain && res.Code != "EXECUTOR_PAUSED" {
			if it.Code == "" {
				it.Code = "save_interrupted"
			}
			it.State = "save_failed"
			if AccessDenied(it.Code) {
				it.State = "access_denied"
			}
		} else if !uncertain {
			// The paused operation is final and wrote nothing; resending its ID
			// would replay it, so the next pass admits a fresh attempt.
			it.State = ""
			it.Attempt++
		}
	default:
		uncertain = true
	}
	if uncertain {
		it.State, it.Code = "uncertain", "commit_uncertain"
	}
	return it
}

// Summarize derives the task state; an empty list is never published.
func Summarize(items []Item) Summary {
	s := Summary{Total: len(items)}
	progressing := ""
	for _, it := range items {
		switch it.State {
		case "committed":
			s.Committed++
		case "save_failed", "access_denied", "canceled":
			s.Failed++
			if s.Code == "" || it.State == "access_denied" {
				s.Code, s.Reason = it.Code, it.Reason
				s.State = it.State
				if it.State == "canceled" {
					s.State = "save_failed"
				}
			}
		default:
			if progressing == "" || it.State == "uncertain" {
				progressing = it.State
				if progressing == "" {
					progressing = "pending"
				}
			}
		}
	}
	switch {
	case s.Total > 0 && s.Committed == s.Total:
		s.State, s.Published = "committed", true
	case progressing != "":
		s.State = progressing
		if s.Failed == 0 {
			s.Code, s.Reason = "", ""
		}
	}
	return s
}

// Retry starts a fresh attempt for every file that failed terminally. Saved
// files are kept; uncertain files keep their reference until resolved.
func Retry(items []Item) ([]Item, bool) {
	out := append([]Item(nil), items...)
	changed := false
	for i, it := range out {
		if it.State == "save_failed" || it.State == "access_denied" || it.State == "canceled" {
			out[i] = Item{Source: it.Source, Rel: it.Rel, Name: it.Name, Size: it.Size, Attempt: it.Attempt + 1, Conflict: "rename"}
			changed = true
		}
	}
	return out, changed
}

// Cancel stops every admitted, unfinished file. done reports that no file can
// still be written; kept that at least one file was saved. An executor error is
// returned after recording the items already resolved.
func Cancel(ctx context.Context, m Mover, b Batch, items []Item) ([]Item, bool, bool, error) {
	out := append([]Item(nil), items...)
	done, kept := true, false
	for i, it := range out {
		switch it.State {
		case "committed":
			kept = true
			continue
		case "", "save_failed", "access_denied", "canceled":
			// Never admitted, or terminally refused without a write.
			continue
		}
		ref := delegate.OperationRef{DelegationID: b.DelegationID, JobID: OperationID(b.Task, i, it.Attempt)}
		var res delegate.OperationResult
		var err error
		if it.State == "uncertain" {
			res, err = m.OperationStatus(ctx, nil, ref)
		} else {
			res, err = m.CancelOperation(ctx, nil, ref)
		}
		if err != nil {
			return out, false, kept, err
		}
		if res.JobID != ref.JobID {
			return out, false, kept, &api.Error{Code: "EXECUTOR_PROTOCOL"}
		}
		switch res.State {
		case "canceled", "interrupted":
			out[i].State = "canceled"
		case "committed":
			if !CommittedReceipt(b.Destination, Params(b, i, it), res) {
				return out, false, kept, &api.Error{Code: "EXECUTOR_PROTOCOL"}
			}
			out[i].State = "committed"
			out[i].SavedName = path.Base(res.Receipt.Path)
			kept = true
		default:
			out[i].State = res.State
			done = false
		}
	}
	return out, done, kept, nil
}

// CommittedReceipt checks the protected SDK result against the pinned request.
// Rename allocation and deduplication belong entirely to the SDK executor.
func CommittedReceipt(destination string, p delegate.MoveParams, res delegate.OperationResult) bool {
	if res.JobID != p.JobID || res.Receipt == nil || res.Receipt.Size != p.Size {
		return false
	}
	requested := path.Join(destination, p.Name)
	if res.Receipt.Path == requested {
		return true
	}
	if p.Conflict != "rename" || path.Dir(res.Receipt.Path) != path.Dir(requested) {
		return false
	}
	leaf := path.Base(requested)
	ext := path.Ext(leaf)
	if ext == leaf {
		ext = ""
	}
	stem := strings.TrimSuffix(leaf, ext)
	// Match the SDK's bounded DSM-style alternatives without allocating a name.
	for n := 1; n <= 99; n++ {
		if path.Base(res.Receipt.Path) == stem+" ("+strconv.Itoa(n)+")"+ext {
			return true
		}
	}
	return false
}
