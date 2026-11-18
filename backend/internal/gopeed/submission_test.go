package gopeed

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type c234Transport struct {
	base     http.RoundTripper
	accepted atomic.Int32
}

type submissionFixture struct {
	mu              sync.Mutex
	posts           int
	tasks           []map[string]any
	listUnavailable bool
	mode            string
	stage           string
}

func newSubmissionFixture(t *testing.T, mode string) (*Service, *submissionFixture) {
	s, _, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	f := &submissionFixture{mode: mode}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.URL.Path == "/api/v1/resolve" {
			var in struct{ Opts struct{ Path string } }
			json.NewDecoder(r.Body).Decode(&in)
			f.stage = in.Opts.Path
			fmt.Fprint(w, `{"code":0,"data":{"id":"rid-1","res":{"files":[{"name":"file.bin","path":"","size":8}]}}}`)
			return
		}
		if r.Method == "POST" {
			f.posts++
			b, _ := os.ReadFile(filepath.Join(s.Data, "jobs.json"))
			var st State
			json.Unmarshal(b, &st)
			j := st.Resolved["rid-1"]
			if j.Owner != "uid:admin" || j.Stage != f.stage || j.DelegationID != "grant-admin" || j.SubmissionRID != "rid-1" {
				t.Errorf("no durable owner before engine side effect: %s", b)
			}
			f.tasks = []map[string]any{{"id": "accepted", "status": "pause", "meta": map[string]any{"opts": map[string]any{"path": f.stage, "name": "file.bin"}}}}
			switch f.mode {
			case "lost":
				fmt.Fprint(w, `{"code":`)
				return
			case "engine-error":
				fmt.Fprint(w, `{"code":500,"data":null}`)
				return
			case "timeout":
				time.Sleep(80 * time.Millisecond)
			case "commit-failure":
				os.Chmod(s.Data, 0500)
			}
			fmt.Fprint(w, `{"code":0,"data":"accepted"}`)
			return
		}
		if r.Method == "PUT" {
			fmt.Fprint(w, `{"code":0}`)
			return
		}
		if f.listUnavailable {
			http.Error(w, "unavailable", 503)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": f.tasks})
	}))
	t.Cleanup(server.Close)
	s.Engine = server.URL
	s.checkURL = func(string) bool { return true }
	return s, f
}
func resolveSubmission(t *testing.T, s *Service) {
	out, _ := call(t, s, "admin", "test.resolve", json.RawMessage(`{"req":{"url":"https://example.com/file.bin"},"opts":{"path":"/home","name":"file.bin"}}`))
	if out["success"] != true {
		t.Fatal(out)
	}
}
func createSubmission(t *testing.T, s *Service, user string) map[string]any {
	out, _ := call(t, s, user, "test.create", json.RawMessage(`{"rid":"rid-1"}`))
	return out
}
func submissionOwner(t *testing.T, s *Service) string {
	var owner string
	_, e := s.withState(context.Background(), func(st *State) (any, error) { owner = st.Jobs["accepted"].Owner; return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	return owner
}
func TestC2LostReplyTimeoutAndAcceptedErrorNeverRepost(t *testing.T) {
	for _, mode := range []string{"lost", "timeout", "engine-error", "commit-failure"} {
		t.Run(mode, func(t *testing.T) {
			s, f := newSubmissionFixture(t, mode)
			resolveSubmission(t, s)
			if mode == "timeout" {
				s.HTTP.Timeout = 30 * time.Millisecond
			}
			_ = createSubmission(t, s, "admin")
			os.Chmod(s.Data, 0700)
			s.HTTP.Timeout = time.Second
			restarted := New(s.Data)
			restarted.Owner, restarted.DestinationDelegate = s.Owner, s.DestinationDelegate
			restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, &fakeSave{}
			_ = restarted.SaveCompleted(context.Background())
			if owner := submissionOwner(t, restarted); owner != "uid:admin" {
				t.Errorf("lost accepted owner after restart: %q", owner)
			}
			for range 2 {
				_ = createSubmission(t, s, "admin")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.posts != 1 {
				t.Errorf("engine POST count=%d", f.posts)
			}
		})
	}
}
func TestC2PendingReadFailureLaterConvergesWithoutPost(t *testing.T) {
	s, f := newSubmissionFixture(t, "lost")
	resolveSubmission(t, s)
	f.mu.Lock()
	f.listUnavailable = true
	f.mu.Unlock()
	_ = createSubmission(t, s, "admin")
	for range 2 {
		_ = s.SaveCompleted(context.Background())
		_ = createSubmission(t, s, "admin")
	}
	f.mu.Lock()
	f.listUnavailable = false
	f.mu.Unlock()
	_ = s.SaveCompleted(context.Background())
	if submissionOwner(t, s) != "uid:admin" {
		t.Fatal("late accepted task was not reconciled")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatal("blind retry", f.posts)
	}
}
func TestC2CrashBeforeTransportDoesNotSendAndAfterTransportAdopts(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprint(sent), func(t *testing.T) {
			s, f := newSubmissionFixture(t, "ok")
			resolveSubmission(t, s)
			base := s.HTTP.Transport
			crashed := false
			s.HTTP.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.Method == "POST" {
					if sent {
						resp, e := base.RoundTrip(r)
						if e != nil {
							t.Fatal(e)
						}
						io.Copy(io.Discard, resp.Body)
						resp.Body.Close()
					}
					crashed = true
					panic("injected producer crash")
				}
				return base.RoundTrip(r)
			})
			_ = createSubmission(t, s, "admin")
			if !crashed {
				t.Fatal("crash was not injected")
			}
			s.HTTP.Transport = base
			restarted := New(s.Data)
			restarted.Owner, restarted.DestinationDelegate = s.Owner, s.DestinationDelegate
			restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, &fakeSave{}
			for range 2 {
				_ = restarted.SaveCompleted(context.Background())
				_ = createSubmission(t, restarted, "admin")
			}
			if sent && submissionOwner(t, restarted) != "uid:admin" {
				t.Error("crash lost owner")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			want := 0
			if sent {
				want = 1
			}
			if f.posts != want {
				t.Error("replayed after crash", f.posts)
			}
		})
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestC2IntentPersistenceFailurePreventsEnginePost(t *testing.T) {
	s, f := newSubmissionFixture(t, "ok")
	resolveSubmission(t, s)
	if err := os.Chmod(s.Data, 0500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.Data, 0700)
	out := createSubmission(t, s, "admin")
	if out["success"] != false {
		t.Fatal(out)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 0 {
		t.Fatal("POST preceded durable intent", f.posts)
	}
}

func TestC2ReconciliationDoesNotGuessOrChangeOwners(t *testing.T) {
	for _, mode := range []string{"duplicate", "wrong-stage", "wrong-name", "unsent", "foreign"} {
		t.Run(mode, func(t *testing.T) {
			s, f := newSubmissionFixture(t, "lost")
			resolveSubmission(t, s)
			f.mu.Lock()
			f.listUnavailable = true
			f.mu.Unlock()
			if mode != "unsent" {
				_ = createSubmission(t, s, "admin")
			}
			f.mu.Lock()
			f.listUnavailable = false
			if mode == "unsent" {
				f.tasks = []map[string]any{{"id": "accepted", "meta": map[string]any{"opts": map[string]any{"path": f.stage, "name": "file.bin"}}}}
			}
			if mode == "duplicate" {
				f.tasks = append(f.tasks, f.tasks[0])
			}
			if mode == "wrong-stage" {
				f.tasks[0]["meta"].(map[string]any)["opts"].(map[string]any)["path"] = f.stage + "-other"
			}
			if mode == "wrong-name" {
				f.tasks[0]["meta"].(map[string]any)["opts"].(map[string]any)["name"] = "other.bin"
			}
			f.mu.Unlock()
			if mode == "foreign" {
				out := createSubmission(t, s, "normal2")
				if out["success"] != false || out["error"].(map[string]any)["code"] != "task_denied" {
					t.Fatal(out)
				}
			} else {
				_ = s.ReconcileSubmissions(context.Background())
			}
			if submissionOwner(t, s) != "" {
				t.Fatal("guessed owner from an unverified match")
			}
		})
	}
}

func TestC2ConcurrentSameRIDAndDeletedReceiptNeverRepost(t *testing.T) {
	s, f := newSubmissionFixture(t, "ok")
	resolveSubmission(t, s)
	out := make(chan map[string]any, 2)
	for range 2 {
		go func() { out <- createSubmission(t, s, "admin") }()
	}
	for range 2 {
		if r := <-out; r["success"] != true || r["data"].(map[string]any)["data"] != "accepted" {
			t.Fatal(r)
		}
	}
	f.mu.Lock()
	posts := f.posts
	f.mu.Unlock()
	if posts != 1 {
		t.Fatal("concurrent duplicate", posts)
	}
	result, _ := call(t, s, "normal2", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	if result["success"] != true || len(result["data"].(map[string]any)["data"].([]any)) != 0 {
		t.Fatal("foreign owner leaked", result)
	}
	_ = createSubmission(t, s, "normal2")
	deleted, _ := call(t, s, "admin", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/accepted"})
	if deleted["success"] != true {
		t.Fatal(deleted)
	}
	if r := createSubmission(t, s, "admin"); r["success"] != false {
		t.Fatal("deleted RID revived", r)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.posts != 1 {
		t.Fatal("deleted RID was reposted", f.posts)
	}
}

func TestC2EmptyEngineAndExistingSavedReceiptAreNotReplayed(t *testing.T) {
	job := Job{SubmissionRID: "rid", Owner: "uid:admin", Stage: "/private/stage", Destination: "/home", Filename: "file.bin", DelegationID: "grant"}
	st := State{Resolved: map[string]Job{"rid": job}, Jobs: map[string]Job{}}
	for _, empty := range []string{"null", "[]"} {
		if err := reconcileTaskList(&st, json.RawMessage(empty)); err != nil || len(st.Jobs) != 0 || len(st.Resolved) != 1 {
			t.Fatal("no-admission intent was lost", err, st)
		}
	}
	saved := job
	saved.Published = true
	saved.SaveState = "committed"
	saved.SaveJobID = "original-save"
	st.Jobs["accepted"] = saved
	if err := reconcileTaskList(&st, json.RawMessage(`[{"id":"accepted","meta":{"opts":{"path":"/private/stage","name":"file.bin"}}}]`)); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(st.Jobs["accepted"], saved) || len(st.Resolved) != 0 {
		t.Fatal("reconciliation overwrote committed receipt", st)
	}
}

func (f *c234Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := f.base.RoundTrip(r)
	if err == nil && r.Method == "POST" && r.URL.Path == "/api/v1/tasks" {
		// Real official engine accepts; only its response is lost at the client.
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		f.accepted.Add(1)
		return nil, io.ErrUnexpectedEOF
	}
	return resp, err
}

func TestC2AcceptedOfficialTaskMustHaveDurableOwner(t *testing.T) {
	s := officialEngine(t)
	s.DestinationDelegate = ownDestination{}
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "41984")
		if r.Method != "HEAD" {
			w.Write(make([]byte, 41984))
		}
	}))
	defer source.Close()
	s.checkURL = func(u string) bool { return u == source.URL+"/fixture.bin" }
	fault := &c234Transport{base: s.HTTP.Transport}
	s.HTTP.Transport = fault
	out := resolveAndCreate(t, s, "admin", map[string]any{"req": map[string]any{"url": source.URL + "/fixture.bin"}, "opts": map[string]any{"path": "/home", "name": "fixture.bin"}})
	s.HTTP.Transport = fault.base
	if fault.accepted.Load() != 1 {
		t.Fatalf("fault did not follow one real create: %v", out)
	}
	rep, err := s.engine(context.Background(), "GET", "/api/v1/tasks", nil, nil)
	var tasks []struct {
		ID string `json:"id"`
	}
	if err != nil || json.Unmarshal(rep.Data, &tasks) != nil || len(tasks) != 1 {
		t.Fatalf("engine did not retain exactly one task: %s %v", rep.Data, err)
	}
	var owned bool
	_, err = s.withState(context.Background(), func(st *State) (any, error) {
		owned = st.Jobs[tasks[0].ID].Owner == "uid:admin"
		t.Logf("app response=%v; engine tasks=%d; durable jobs=%d; durable resolves=%d", out, len(tasks), len(st.Jobs), len(st.Resolved))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !owned {
		t.Fatal("C2: engine accepted one task, but its owner/job was not persisted")
	}
}
