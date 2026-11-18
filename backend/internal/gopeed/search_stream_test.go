package gopeed

import (
	"context"
	"testing"
	"time"

	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
)

func TestAsyncSearchPublishesFastPageWithoutWaitingForSlowSource(t *testing.T) {
	s, src := asyncSearchFixture(t)
	hold := make(chan struct{})
	defer close(hold)
	s.Search.Stream = func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		if err := emit(0, search.Reply{Results: []search.Hit{{Title: "Fast", Link: "https://example.org/file"}}}); err != nil {
			return err
		}
		select {
		case <-hold:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	id := startSearchJob(t, s, src, "streamed-job")
	runner := s.SearchRunner(background.WorkerInfo{PID: 1, UID: 1000, Username: "pkg"})
	job, err := runner.Store.Claim(context.Background())
	if err != nil || job == nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); runner.Process(context.Background(), job) }()
	until := time.Now().Add(time.Second)
	var out map[string]any
	for time.Now().Before(until) {
		out, _ = call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
		if out["success"] == true && len(out["data"].(map[string]any)["items"].([]any)) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if out["success"] != true || len(out["data"].(map[string]any)["items"].([]any)) != 1 {
		t.Fatal("fast page did not arrive", out)
	}
	call(t, s, "normal1", "search.jobs.cancel", map[string]any{"id": id})
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop worker")
	}
	out, _ = call(t, s, "normal1", "search.jobs.status", map[string]any{"id": id})
	if out["data"].(map[string]any)["state"] != "canceled" {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
	if out["success"] != false {
		t.Fatal("canceled job exposed result", out)
	}
}
