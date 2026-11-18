package media

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"dsmapp/internal/binaries"
)

func jobRelease(t *testing.T, f *fixture, tool string) Release {
	t.Helper()
	body := elfBody(62)
	r := Release{Tool: tool, Version: "2026.08.19", Arch: "amd64", Asset: "yt-dlp_linux"}
	if tool == "ffmpeg" {
		body = tarXZ(t, map[string][]byte{"x/bin/ffmpeg": body, "x/bin/ffprobe": body}, "")
		r.Version = "2026-10-10-18-21"
		r.Asset = "ffmpeg-N-1-gabcdef1-linux64-gpl.tar.xz"
	}
	a := f.asset("fixture", r.Version, r.Asset, body)
	r.URL = a.URL
	r.SHA256 = digest(body)
	return r
}

func TestToolJobsRunIndependentlyAndMergeTheirReceipts(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	yt, ff := jobRelease(t, f, "yt-dlp"), jobRelease(t, f, "ffmpeg")
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	tools.Check = func(_ context.Context, tool, _ string, _ Release) error {
		if tool == "yt-dlp" {
			close(started)
			<-release
		}
		return nil
	}
	tools.Submit(ToolRequest{ID: "yt", Tool: "yt-dlp", Op: "install", Release: &yt})
	go func() { tools.ProcessTool(context.Background(), "yt-dlp"); close(done) }()
	defer func() {
		if release != nil {
			close(release)
			<-done
		}
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("yt-dlp did not start")
	}
	if job := tools.State().Tools["yt-dlp"].Job; job.Status != "running" {
		t.Fatal(job)
	}
	// Admit and finish FFmpeg while the other tool is still running.
	if err := tools.Submit(ToolRequest{ID: "ff", Tool: "ffmpeg", Op: "install", Release: &ff}); err != nil {
		t.Fatal(err)
	}
	tools.ProcessTool(context.Background(), "ffmpeg")
	st := tools.State()
	if st.Tools["ffmpeg"].Active == nil || st.Tools["ffmpeg"].Job.Status != "succeeded" || st.Tools["yt-dlp"].Job.Status != "running" {
		t.Fatalf("%+v", st)
	}
	// Releasing yt-dlp must retain FFmpeg's new active selection and receipt.
	close(release)
	<-done
	release = nil
	st = tools.State()
	if st.Tools["ffmpeg"].Active == nil || st.Tools["yt-dlp"].Active == nil {
		t.Fatal(st)
	}
}

func TestConcurrentToolAdmissionAndWorkersDeduplicate(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	r := jobRelease(t, f, "yt-dlp")
	var checked atomic.Int32
	tools.Check = func(context.Context, string, string, Release) error { checked.Add(1); return nil }
	const n = 12
	ids := make(chan string, n)
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req, err := tools.Enqueue(context.Background(), ToolRequest{ID: fmt.Sprint(i), Tool: "yt-dlp", Op: "install", Release: &r})
			ids <- req.ID
			errs <- err
		}(i)
	}
	wg.Wait()
	close(ids)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	first := ""
	for id := range ids {
		if first == "" {
			first = id
		}
		if id != first {
			t.Fatalf("multiple admissions: %s %s", first, id)
		}
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); tools.ProcessTool(context.Background(), "yt-dlp") }()
	}
	wg.Wait()
	if checked.Load() != 1 || tools.Pending() {
		t.Fatal(checked.Load(), tools.State())
	}
	conflict := r
	conflict.Version = "2026.09.01"
	tools.Submit(ToolRequest{ID: "next", Tool: "yt-dlp", Op: "install", Release: &r})
	if _, err := tools.Enqueue(context.Background(), ToolRequest{ID: "conflict", Tool: "yt-dlp", Op: "install", Release: &conflict}); !errors.Is(err, ErrToolPending) {
		t.Fatal(err)
	}
}

func TestToolFailureStaysOnItsToolAndCanBeRetried(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	yt, ff := jobRelease(t, f, "yt-dlp"), jobRelease(t, f, "ffmpeg")
	tools.Check = func(_ context.Context, tool, _ string, _ Release) error {
		if tool == "ffmpeg" {
			return errors.New("private candidate path")
		}
		return nil
	}
	tools.Submit(ToolRequest{ID: "yt", Tool: "yt-dlp", Op: "install", Release: &yt})
	tools.Submit(ToolRequest{ID: "ff", Tool: "ffmpeg", Op: "install", Release: &ff})
	tools.ProcessPending(context.Background())
	st := tools.State()
	if st.Tools["ffmpeg"].Job.Error != "tool_version_failed" || st.Tools["ffmpeg"].Active != nil || st.Tools["yt-dlp"].Job.Status != "succeeded" || tools.Pending() {
		t.Fatalf("%+v", st)
	}
	tools.Check = func(context.Context, string, string, Release) error { return nil }
	if err := tools.Submit(ToolRequest{ID: "retry", Tool: "ffmpeg", Op: "install", Release: &ff}); err != nil {
		t.Fatal(err)
	}
	tools.ProcessPending(context.Background())
	if st = tools.State(); st.Tools["ffmpeg"].Active == nil || st.Tools["ffmpeg"].Job.Status != "succeeded" || st.Tools["yt-dlp"].Active == nil {
		t.Fatalf("%+v", st)
	}
}

func TestToolJobsResumeAfterDaemonStopAndPreserveLegacyReceipts(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	r := jobRelease(t, f, "yt-dlp")
	// An old package's request remains executable after upgrade.
	binaries.Atomic(filepath.Join(tools.dir(), "request.json"), ToolRequest{ID: "legacy", Tool: "yt-dlp", Op: "install", Release: &r})
	ctx, cancel := context.WithCancel(context.Background())
	tools.Check = func(context.Context, string, string, Release) error { cancel(); return ctx.Err() }
	tools.ProcessPending(ctx)
	if job := tools.State().Tools["yt-dlp"].Job; job.Status != "running" {
		t.Fatal(job)
	}
	tools.Check = func(context.Context, string, string, Release) error { return nil }
	tools.ProcessPending(context.Background())
	if job := tools.State().Tools["yt-dlp"].Job; job.Status != "succeeded" || job.Request != "legacy" {
		t.Fatal(job)
	}
	// Legacy completed selections cannot be resurrected when a sibling finishes.
	st := tools.readState()
	st.Tools["yt-dlp"].Job = nil
	binaries.Atomic(filepath.Join(tools.dir(), "state.json"), st)
	ff := jobRelease(t, f, "ffmpeg")
	tools.Submit(ToolRequest{ID: "ff", Tool: "ffmpeg", Op: "install", Release: &ff})
	tools.ProcessPending(context.Background())
	if tools.Pending() || tools.State().Tools["yt-dlp"].Job.Request != "legacy" {
		t.Fatal(tools.State())
	}
}

func TestToolStorageAndPermissionFailuresKeepTheirCause(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{syscall.ENOSPC, "tool_storage_full"}, {syscall.EDQUOT, "tool_storage_full"}, {syscall.EACCES, "tool_permission_denied"}} {
		t.Run(tc.code, func(t *testing.T) {
			f := newFixture(t)
			tools := f.tools(t, "amd64")
			r := jobRelease(t, f, "yt-dlp")
			tools.Check = func(context.Context, string, string, Release) error { return tc.err }
			tools.Submit(ToolRequest{ID: "failure", Tool: "yt-dlp", Op: "install", Release: &r})
			tools.ProcessPending(context.Background())
			if got := tools.State().Tools["yt-dlp"].Job.Error; got != tc.code {
				t.Fatalf("%s != %s", got, tc.code)
			}
		})
	}
}
