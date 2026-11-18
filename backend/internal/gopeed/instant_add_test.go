package gopeed

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestInstantAdmissionDoesNotWaitForSource(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		time.Sleep(250 * time.Millisecond)
		return 0, map[string]any{"id": "slow-rid", "res": map[string]any{"files": []any{map[string]any{"name": "slow.bin", "size": 8}}}}
	}
	start := time.Now()
	results := createMany(t, s, "alice", item(9, "https://example.com/slow.bin"))
	elapsed := time.Since(start)
	t.Logf("admission=%s source-delay=250ms resolves=%d result=%v", elapsed, len(f.resolves), results)
	if elapsed >= 200*time.Millisecond || len(f.resolves) != 0 {
		t.Fatalf("admission waited for source: %s", elapsed)
	}
}

func TestPreparingTaskCanPauseResumeAndRejectOtherOwner(t *testing.T) {
	s, f := newEngineFixture(t)
	result := createMany(t, s, "alice", item(1, "https://example.com/a"))
	id := admissionID(result[0])
	for _, op := range []string{"pause", "continue"} {
		out, _ := call(t, s, "bob", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/" + op})
		if out["success"] != false {
			t.Fatal(out)
		}
	}
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/pause"})
	if out["success"] != true {
		t.Fatal(out)
	}
	r := &PreparationRunner{Service: s}
	defer r.Close()
	r.Tick(context.Background())
	if len(f.resolves) != 0 || !state(t, s).Jobs[id].ManualPaused {
		t.Fatal("paused preparation started")
	}
	out, _ = call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/continue"})
	if out["success"] != true || state(t, s).Jobs[id].ManualPaused {
		t.Fatal(out)
	}
}

func TestFailedPreparationCanRetryInPlace(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) { return 1, nil }
	result := createMany(t, s, "alice", item(1, "https://example.com/broken"))
	id := admissionID(result[0])
	processPreparation(t, s, id)
	if state(t, s).Jobs[id].Preparation.Error != "source_unreachable" {
		t.Fatal(state(t, s))
	}
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/continue"})
	if out["success"] != true || state(t, s).Jobs[id].Preparation.Status != "queued" {
		t.Fatal(out)
	}
}

func TestSlowResolutionDoesNotBlockAnotherAdmissionOrPause(t *testing.T) {
	s, f := newEngineFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.resolve = func(string) (int, any) { close(entered); <-release; return 1, nil }
	results := createMany(t, s, "alice", item(1, "https://example.com/slow"))
	id := admissionID(results[0])
	job := state(t, s).Jobs[id]
	done := make(chan struct{})
	go func() { defer close(done); (&PreparationRunner{Service: s}).prepare(context.Background(), id, job) }()
	<-entered
	defer func() { close(release); <-done }()
	start := time.Now()
	second := createMany(t, s, "bob", item(2, "https://example.com/another"))
	if second[0]["status"] != "created" || time.Since(start) > 200*time.Millisecond {
		t.Fatal("admission blocked", second)
	}
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/pause"})
	if out["success"] != true || !state(t, s).Jobs[id].ManualPaused {
		t.Fatal(out)
	}
}

func TestPreparationMergeDoesNotResurrectConcurrentlyReconciledIntent(t *testing.T) {
	s, _ := newEngineFixture(t)
	result := createMany(t, s, "alice", item(1, "https://example.com/a"))
	id := admissionID(result[0])
	j := state(t, s).Jobs[id]
	s.withState(context.Background(), func(st *State) (any, error) {
		st.Resolved["lost-reply"] = Job{Owner: "uid:bob", SubmissionRID: "lost-reply", Stage: "private"}
		return nil, nil
	})
	a := sourceAccess{owner: j.Owner, admission: &j, admissionID: id}
	_, err := a.resolveLocked(context.Background(), s, func(_ string, snapshot *State) (any, error) {
		// Represents the save worker reconciling an accepted task while a network
		// resolve computed against an older journal snapshot.
		s.withState(context.Background(), func(st *State) (any, error) {
			st.Jobs["accepted"] = st.Resolved["lost-reply"]
			delete(st.Resolved, "lost-reply")
			return nil, nil
		})
		snapshot.Resolved["new-preview"] = Job{Owner: j.Owner, Stage: "new-private"}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	st := state(t, s)
	if _, ok := st.Resolved["lost-reply"]; ok {
		t.Fatal("stale intent resurrected")
	}
	if st.Jobs["accepted"].Owner != "uid:bob" || st.Resolved["new-preview"].Owner != j.Owner {
		t.Fatal(st)
	}
}

func TestSlowEngineCreateDoesNotBlockAnotherAdmission(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, map[string]any{"id": "rid-file", "res": map[string]any{"files": []any{map[string]any{"name": "file.bin", "size": 8}}}}
	}
	entered, release := make(chan struct{}), make(chan struct{})
	base := s.HTTP.Transport
	s.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method == "POST" && r.URL.Path == "/api/v1/tasks" {
			close(entered)
			<-release
		}
		return base.RoundTrip(r)
	})
	result := createMany(t, s, "alice", item(1, "https://example.com/a"))
	id := admissionID(result[0])
	j := state(t, s).Jobs[id]
	worker := make(chan struct{})
	go func() { defer close(worker); (&PreparationRunner{Service: s}).prepare(context.Background(), id, j) }()
	<-entered
	defer func() { close(release); <-worker }()
	admission := make(chan map[string]any, 1)
	go func() {
		out, _ := call(t, s, "bob", "downloads.create_many", map[string]any{"path": "/home", "items": []batchItem{item(2, "https://example.com/b")}})
		admission <- out
	}()
	select {
	case out := <-admission:
		if out["success"] != true {
			t.Fatal(out)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("engine create held admission journal lock")
	}
}

func TestPreparationCreateLostRepliesNeverRepost(t *testing.T) {
	for _, mode := range []string{"lost", "engine-error", "timeout", "commit-failure"} {
		t.Run(mode, func(t *testing.T) {
			s, f := newSubmissionFixture(t, mode)
			if mode == "timeout" {
				s.HTTP.Timeout = 30 * time.Millisecond
			}
			result := createMany(t, s, "admin", item(1, "https://example.com/file.bin"))
			id := admissionID(result[0])
			processPreparation(t, s, id)
			if mode == "timeout" {
				f.mu.Lock()
				f.mu.Unlock()
				processPreparation(t, s, id)
			}
			if mode == "commit-failure" {
				os.Chmod(s.Data, 0700)
				processPreparation(t, s, id)
			}
			again := createMany(t, s, "admin", item(1, "https://example.com/file.bin"))
			f.mu.Lock()
			posts := f.posts
			f.mu.Unlock()
			if posts != 1 || admissionID(again[0]) != "accepted" {
				t.Fatal("create replayed or lost receipt", posts, again)
			}
		})
	}
}

func TestPreparationExtensionIntentsRemainDurable(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, galleryResult("https://cdn.example/1.jpg", "https://cdn.example/2.jpg")
	}
	result := createMany(t, s, "admin", item(1, "pan-share:abc"))
	id := admissionID(result[0])
	f.onCreate = func() {
		st := state(t, s)
		count := 0
		for _, j := range st.Resolved {
			if j.SubmissionRID != "" {
				count++
			}
		}
		if len(f.creates) == 0 && count != 2 {
			t.Fatal("extension not durable before send", st)
		}
	}
	processPreparation(t, s, id)
	if len(f.creates) != 2 || len(state(t, s).Jobs) != 2 {
		t.Fatal(state(t, s), f.creates)
	}
}
