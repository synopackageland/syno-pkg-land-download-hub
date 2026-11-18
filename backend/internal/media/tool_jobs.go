package media

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
)

// ToolJob is the durable, per-tool receipt shown on the tool's card.
// queued/running remain pending until the selection and terminal receipt are
// atomically committed together. A daemon restart safely repeats unfinished work.
type ToolJob struct {
	Request string   `json:"request"`
	Release *Release `json:"release,omitempty"`
	Op      string   `json:"op,omitempty"`
	Status  string   `json:"status"`
	Error   string   `json:"error,omitempty"`
	ResetAt string   `json:"resetAt,omitempty"`
}

var ErrToolPending = errors.New("tool update pending")

type toolFailure struct {
	code string
	err  error
}

func (e *toolFailure) Error() string          { return e.code }
func (e *toolFailure) Unwrap() error          { return e.err }
func jobFailure(code string, err error) error { return &toolFailure{code, err} }

func (t Tools) request(tool string) (ToolRequest, bool) {
	var req ToolRequest
	b, err := os.ReadFile(filepath.Join(t.dir(), "requests", tool+".json"))
	if os.IsNotExist(err) {
		// Retain an unfinished request from the single-job store across upgrades.
		b, err = os.ReadFile(filepath.Join(t.dir(), "request.json"))
	}
	ok := err == nil && len(b) <= 16<<10 && json.Unmarshal(b, &req) == nil && req.ID != "" && req.Tool == tool
	return req, ok
}

func (t Tools) State() ToolsState {
	st := t.readState()
	for tool, cur := range st.Tools {
		if req, ok := t.request(tool); ok && (cur.Job == nil || req.ID != cur.Job.Request) {
			status := "queued"
			if req.Status == "running" {
				status = "running"
			}
			cur.Job = &ToolJob{Request: req.ID, Op: req.Op, Status: status, Release: req.Release}
		}
	}
	return st
}

func (t Tools) Pending() bool {
	for _, cur := range t.State().Tools {
		if cur.Job != nil && (cur.Job.Status == "queued" || cur.Job.Status == "running") {
			return true
		}
	}
	return false
}

// Enqueue serializes only admission and state writes, never downloads. Racing
// identical presses receive the original request ID; another tool remains free.
func (t Tools) Enqueue(ctx context.Context, req ToolRequest) (ToolRequest, error) {
	if !t.Supported(req.Tool) || req.ID == "" || len(req.ID) > 64 || (req.Op != "install" && req.Op != "update" && req.Op != "rollback") || (req.Op == "rollback") == (req.Release != nil) || (req.Op == "update" && req.Tool != "yt-dlp") || req.Release != nil && req.Release.Tool != req.Tool {
		return ToolRequest{}, errors.New("invalid request")
	}
	unlock, err := (binaries.Manager{Data: t.dir()}).Lock(ctx)
	if err != nil {
		return ToolRequest{}, err
	}
	defer unlock()
	cur := t.State().Tools[req.Tool]
	if cur.Job != nil && (cur.Job.Status == "queued" || cur.Job.Status == "running") {
		old, ok := t.request(req.Tool)
		if ok && old.Op == req.Op && sameRelease(old.Release, req.Release) {
			return old, nil
		}
		return ToolRequest{}, ErrToolPending
	}
	req.Status = "queued"
	return req, binaries.Atomic(filepath.Join(t.dir(), "requests", req.Tool+".json"), req)
}
func sameRelease(a, b *Release) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Submit preserves the internal caller's indication that its new ID was not used.
func (t Tools) Submit(req ToolRequest) error {
	got, err := t.Enqueue(context.Background(), req)
	if err == nil && got.ID != req.ID {
		return ErrToolPending
	}
	return err
}

// ProcessPending is also used by local fixtures. The daemon polls each tool
// separately, so a tool submitted during a slow sibling job starts next tick.
func (t Tools) ProcessPending(ctx context.Context) {
	var wg sync.WaitGroup
	for tool := range Specs {
		wg.Add(1)
		go func(tool string) { defer wg.Done(); t.ProcessTool(ctx, tool) }(tool)
	}
	wg.Wait()
}

func (t Tools) ProcessTool(ctx context.Context, tool string) {
	if _, ok := Specs[tool]; !ok {
		return
	}
	if err := os.MkdirAll(t.dir(), 0700); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(t.dir(), tool+"-work.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	// Claim from the same snapshot admission uses. Without this short lock a
	// newly submitted request could be paired with the preceding job's input.
	unlock, err := (binaries.Manager{Data: t.dir()}).Lock(ctx)
	if err != nil {
		return
	}
	req, ok := t.request(tool)
	cur := t.State().Tools[tool]
	if !ok || cur.Job == nil || cur.Job.Request != req.ID || cur.Job.Status == "succeeded" || cur.Job.Status == "failed" {
		unlock()
		return
	}
	req.Status = "running"
	err = binaries.Atomic(filepath.Join(t.dir(), "requests", tool+".json"), req)
	unlock()
	if err != nil {
		return
	}
	var workErr error
	switch {
	case req.Op == "install" && req.Release != nil && req.Release.Tool == tool:
		var got Installed
		if got, workErr = t.Install(ctx, *req.Release); workErr == nil {
			if cur.Active != nil && cur.Active.Dir != got.Dir {
				cur.Previous = cur.Active
			}
			cur.Active = &got
		}
	case req.Op == "update" && req.Release != nil && req.Release.Tool == "yt-dlp" && tool == "yt-dlp" && cur.Active != nil:
		var got Installed
		if got, workErr = t.Install(ctx, *req.Release); workErr == nil {
			cur.Previous, cur.Active = cur.Active, &got
		}
	case req.Op == "rollback" && cur.Previous != nil:
		if workErr = verify(*cur.Previous, Specs[tool]); workErr == nil {
			cur.Active, cur.Previous = cur.Previous, cur.Active
		}
	default:
		workErr = errors.New("invalid tool operation")
	}
	// Stopping the daemon leaves a resumable job, never a false success or failure.
	if ctx.Err() != nil {
		return
	}
	job := &ToolJob{Request: req.ID, Op: req.Op, Status: "succeeded", Release: req.Release}
	var rate *binaries.RateLimitError
	var failure *toolFailure
	switch {
	case errors.As(workErr, &rate):
		job.Error, job.ResetAt = "tool_source_rate_limited", rate.Reset.UTC().Format(time.RFC3339)
	case errors.Is(workErr, githubtoken.ErrInvalid):
		job.Error = "github_token_invalid"
	case errors.Is(workErr, syscall.ENOSPC), errors.Is(workErr, syscall.EDQUOT):
		job.Error = "tool_storage_full"
	case errors.Is(workErr, os.ErrPermission):
		job.Error = "tool_permission_denied"
	case errors.As(workErr, &failure):
		job.Error = failure.code
		// Preserve the more precise inner stage (checksum within download).
		for inner := errors.Unwrap(failure); inner != nil; inner = errors.Unwrap(inner) {
			var detail *toolFailure
			if errors.As(inner, &detail) {
				job.Error = detail.code
			}
		}
	case workErr != nil:
		job.Error = "tool_install_failed"
	}
	if job.Error != "" {
		job.Status = "failed"
	}
	cur.Job = job
	unlock, err = (binaries.Manager{Data: t.dir()}).Lock(ctx)
	if err != nil {
		return
	}
	// Merge only this tool into the newest state so sibling completions are kept.
	st := t.readState()
	st.Tools[tool] = cur
	st.Request, st.Tool, st.Error, st.ResetAt = req.ID, tool, job.Error, job.ResetAt
	err = binaries.Atomic(filepath.Join(t.dir(), "state.json"), st)
	unlock()
	if err == nil {
		t.prune(tool, cur)
	}
}
