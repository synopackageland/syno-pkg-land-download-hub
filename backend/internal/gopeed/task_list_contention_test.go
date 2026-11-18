package gopeed

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"dsmapp/internal/binaries"
)

func boundedListCall(t *testing.T, s *Service, user string, in Input) map[string]any {
	t.Helper()
	server, err := NewServer(actorAuth{}, s.Routes()...)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"method": "gopeed.request", "params": in})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	r := httptest.NewRequest("POST", "http://fixture/api.cgi", bytes.NewReader(body)).WithContext(ctx)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("actor", user)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Body.String())
	}
	return out
}
func assertOwnedList(t *testing.T, out map[string]any, id string) {
	t.Helper()
	if out["success"] != true {
		t.Fatalf("list blocked: %v", out)
	}
	rows := out["data"].(map[string]any)["data"].([]any)
	want := 0
	if id != "" {
		want = 1
	}
	if len(rows) != want {
		t.Fatalf("owner isolation failed: %v", rows)
	}
	if want == 1 && rows[0].(map[string]any)["id"] != id {
		t.Fatalf("foreign task leaked: %v", rows)
	}
}
func TestListsDoNotWaitForEngineOrJournalWriters(t *testing.T) {
	for _, kind := range []string{"engine-lock", "jobs-lock", "pending", "all"} {
		t.Run(kind, func(t *testing.T) {
			s, _, calls := fixture(t)
			seed(t, s)
			if kind == "engine-lock" || kind == "all" {
				unlock, err := (binaries.Manager{Data: s.Data}).Lock(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				defer unlock()
			}
			if kind == "jobs-lock" || kind == "all" {
				f, err := os.OpenFile(filepath.Join(s.Data, "jobs.lock"), os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
					t.Fatal(err)
				}
				defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
			}
			if kind == "pending" || kind == "all" {
				if err := os.WriteFile(filepath.Join(s.Data, "engine-request.json"), []byte(`{"id":"pending-update"}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			for user, id := range map[string]string{"alice": "mine", "bob": "other", "normal1": ""} {
				assertOwnedList(t, boundedListCall(t, s, user, Input{Method: "GET", Path: "/api/v1/tasks"}), id)
			}
			// A read exception must never make pause/create bypass the mutation fence.
			// The SDK server reports its own timeout once this short deadline passes,
			// so the fence is proven by the engine never seeing the mutation.
			if kind != "jobs-lock" {
				before := *calls
				out := boundedListCall(t, s, "alice", Input{Method: "PUT", Path: "/api/v1/tasks/mine/pause"})
				if code := out["error"].(map[string]any)["code"]; out["success"] != false || (code != "engine_busy" && code != "request_timeout") || *calls != before {
					t.Fatalf("mutation fence lost: %v", out)
				}
			}
		})
	}
}
func TestListDoesNotBlockBehindSlowCompletionValidation(t *testing.T) {
	s, f := stagedDelegate(t)
	f.state = "copying"
	base := s.HTTP.Transport
	entered, release := make(chan struct{}), make(chan struct{})
	s.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/api/v1/tasks/mine/stats" {
			close(entered)
			select {
			case <-release:
			case <-r.Context().Done():
				return nil, r.Context().Err()
			}
		}
		return base.RoundTrip(r)
	})
	done := make(chan error, 1)
	go func() { done <- s.SaveCompleted(context.Background()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("save did not enter completion validation")
	}
	out := boundedListCall(t, s, "alice", Input{Method: "GET", Path: "/api/v1/tasks"})
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertOwnedList(t, out, "mine")
	if j := readJob(t, s); j.SaveState != "copying" || f.calls != 1 {
		t.Fatalf("read disrupted save: %#v moves=%d", j, f.calls)
	}
}

func TestListFiltersLatestDurableOwnerSnapshotAfterEngineRead(t *testing.T) {
	s, _, _ := fixture(t)
	seed(t, s)
	base := s.HTTP.Transport
	entered, release := make(chan struct{}), make(chan struct{})
	s.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		resp, err := base.RoundTrip(r)
		if r.URL.Path == "/api/v1/tasks" {
			close(entered)
			<-release
		}
		return resp, err
	})
	done := make(chan map[string]any, 1)
	go func() { done <- boundedListCall(t, s, "alice", Input{Method: "GET", Path: "/api/v1/tasks"}) }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := s.withState(ctx, func(st *State) (any, error) { delete(st.Jobs, "mine"); return nil, nil })
	close(release)
	out := <-done
	if err != nil {
		t.Fatal("list held journal while waiting for engine", err)
	}
	assertOwnedList(t, out, "")
}
func TestReadOnlyListRecognizesDurableIntentWithoutCommittingOrReposting(t *testing.T) {
	s, _, _ := fixture(t)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Resolved["rid"] = Job{SubmissionRID: "rid", Owner: "uid:alice", Stage: "private", Filename: "file.bin", DelegationID: "grant"}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Exact stage/name matching uses the same reconciliation contract as C2.
	base := s.HTTP.Transport
	s.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != "GET" {
			t.Error("read sent mutation", r.Method)
		}
		resp, err := base.RoundTrip(r)
		if err == nil {
			resp.Body.Close()
			resp.Body = io.NopCloser(strings.NewReader(`{"code":0,"data":[{"id":"mine","meta":{"opts":{"path":"private","name":"file.bin"}}},{"id":"foreign","meta":{"opts":{"path":"other","name":"file.bin"}}}]}`))
		}
		return resp, err
	})
	before, err := os.ReadFile(filepath.Join(s.Data, "jobs.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertOwnedList(t, boundedListCall(t, s, "alice", Input{Method: "GET", Path: "/api/v1/tasks"}), "mine")
	assertOwnedList(t, boundedListCall(t, s, "normal2", Input{Method: "GET", Path: "/api/v1/tasks"}), "")
	after, _ := os.ReadFile(filepath.Join(s.Data, "jobs.json"))
	if !bytes.Equal(before, after) {
		t.Fatal("list wrote owner state")
	}
	if err = s.ReconcileSubmissions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if readJob(t, s).Owner != "uid:alice" {
		t.Fatal("writer reconciliation no longer persists original owner")
	}
}
func TestReadOnlyListRejectsCorruptStateAndForbiddenQuery(t *testing.T) {
	s, _, _ := fixture(t)
	seed(t, s)
	out := boundedListCall(t, s, "alice", Input{Method: "GET", Path: "/api/v1/tasks", Query: map[string]json.RawMessage{"owner": json.RawMessage(`"bob"`)}})
	if out["success"] != false || out["error"].(map[string]any)["code"] != "endpoint_denied" {
		t.Fatal("query bypassed authorization", out)
	}
	if err := os.WriteFile(filepath.Join(s.Data, "jobs.json"), []byte(`{"jobs":null,"resolved":{},"folders":{}}`), 0600); err != nil {
		t.Fatal(err)
	}
	out = boundedListCall(t, s, "alice", Input{Method: "GET", Path: "/api/v1/tasks"})
	if out["success"] != false || out["error"].(map[string]any)["code"] != "state_invalid" {
		t.Fatal("corrupt state leaked tasks", out)
	}
}
