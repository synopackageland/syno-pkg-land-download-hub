package binaries

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// MaxJournalBytes bounds jobs.json, including multi-file tasks' per-file
// publication records.
const MaxJournalBytes = 8 << 20

// The official headless engine restores unfinished tasks as paused. The Flutter
// autoStartTasks preference does not run in the server. Resume package-owned
// tasks once per daemon start, preserving explicit pauses and terminal states.
func resumeInterrupted(ctx context.Context, data, endpoint string) error {
	b, e := os.ReadFile(filepath.Join(data, "jobs.json"))
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	var journal struct {
		Jobs map[string]struct {
			ManualPaused    bool `json:"manualPaused"`
			DeleteRequested bool `json:"deleteRequested"`
		}
	}
	if len(b) > MaxJournalBytes || json.Unmarshal(b, &journal) != nil || journal.Jobs == nil {
		return errors.New("invalid task recovery journal")
	}
	var tasks []struct{ ID, Status string }
	if e = engineCall(ctx, data, endpoint, "GET", "/api/v1/tasks", nil, &tasks); e != nil {
		return e
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	for _, task := range tasks {
		job, owned := journal.Jobs[task.ID]
		if !owned || job.DeleteRequested || job.ManualPaused || !valid.MatchString(task.ID) {
			continue
		}
		if task.Status != "pause" && task.Status != "ready" && task.Status != "wait" {
			continue
		}
		if e = engineCall(ctx, data, endpoint, "PUT", "/api/v1/tasks/"+url.PathEscape(task.ID)+"/continue", nil, nil); e != nil {
			return e
		}
	}
	return nil
}

func engineCall(ctx context.Context, data, endpoint, method, path string, body io.Reader, out any) error {
	token, e := os.ReadFile(filepath.Join(data, "engine.token"))
	if e != nil || len(token) != 64 {
		return errors.New("engine token unavailable")
	}
	client := &http.Client{Timeout: 110 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, e := http.NewRequestWithContext(ctx, method, endpoint+path, body)
	if e != nil {
		return e
	}
	req.Header.Set("X-Api-Token", string(token))
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return errors.New("engine request unavailable")
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, (2<<20)+1))
	var reply struct {
		Code int
		Data json.RawMessage
	}
	if e != nil || len(b) > 2<<20 || resp.StatusCode != 200 || json.Unmarshal(b, &reply) != nil || reply.Code != 0 {
		return errors.New("engine request failed")
	}
	if out != nil && json.Unmarshal(reply.Data, out) != nil {
		return errors.New("invalid engine reply")
	}
	return nil
}
