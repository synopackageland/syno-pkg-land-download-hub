package gopeed

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/filestation"
)

type fakeFiles struct {
	deny      bool
	uploaded  string
	uncertain bool
}

func (f *fakeFiles) Verify(context.Context, *http.Request, string) error {
	if f.deny {
		return refused("denied")
	}
	return nil
}
func (f *fakeFiles) Stat(context.Context, *http.Request, string) (filestation.Entry, error) {
	if f.deny {
		return filestation.Entry{}, refused("denied")
	}
	return filestation.Entry{IsDir: true}, nil
}
func (f *fakeFiles) CheckPermission(context.Context, *http.Request, string) error {
	if f.deny {
		return refused("denied")
	}
	return nil
}
func (f *fakeFiles) UploadStream(_ context.Context, r *http.Request, parent, name string, src io.Reader, size int64) error {
	if f.deny {
		return refused("denied")
	}
	if r.Header.Get("X-SYNO-TOKEN") != "current-token" {
		return refused("session_required")
	}
	b, _ := io.ReadAll(src)
	f.uploaded = string(b)
	if f.uncertain {
		return &api.Error{Code: "commit-uncertain"}
	}
	return nil
}

type actorAuth struct{}

func (actorAuth) Authenticate(r *http.Request) (api.Identity, error) {
	u := r.Header.Get("actor")
	if u == "" {
		return api.Identity{}, api.ErrNoSession
	}
	return api.Identity{User: u, Admin: u != "normal1" && u != "normal2"}, nil
}
func fixture(t *testing.T) (*Service, *fakeFiles, *int) {
	t.Helper()
	calls := new(int)
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.Header.Get("X-Api-Token") != strings.Repeat("a", 64) || r.Header.Get("Cookie") != "" || r.Header.Get("X-SYNO-TOKEN") != "" {
			t.Error("credential boundary")
		}
		switch r.URL.Path {
		case "/api/v1/tasks":
			fmt.Fprint(w, `{"code":0,"data":[{"id":"mine","meta":{"opts":{"path":"private"}}},{"id":"other"}]}`)
		default:
			if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/tasks/") {
				id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/"), "/stats")
				var st State
				b, _ := os.ReadFile(filepath.Join(data, "jobs.json"))
				_ = json.Unmarshal(b, &st)
				job := st.Jobs[id]
				if info, err := os.Stat(filepath.Join(job.Stage, job.Filename)); err == nil {
					result := any(completionTask(info.Size()))
					if strings.HasSuffix(r.URL.Path, "/stats") {
						result = completionStats(info.Size())
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": result})
					return
				}
			}
			fmt.Fprint(w, `{"code":0,"data":{"status":"done"}}`)
		}
	}))
	t.Cleanup(engine.Close)
	files := &fakeFiles{}
	s := New(data)
	s.Engine = engine.URL
	s.Owner = func(u string) (string, error) { return "uid:" + u, nil }
	s.fetch = &http.Client{Transport: refuseTransport{}}
	return s, files, calls
}
func call(t *testing.T, s *Service, user, method string, p any) (map[string]any, int) {
	t.Helper()
	server, e := testServer(s, actorAuth{})
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"method": method, "params": p})
	r := httptest.NewRequest("POST", "http://fixture/api.cgi", bytes.NewReader(body))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SYNO-TOKEN", "current-token")
	r.Header.Set("Cookie", "id=current-session")
	r.Header.Set("actor", user)
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Body.String())
	}
	recordRegistryResult(t, method, out)
	return out, w.Code
}
func seed(t *testing.T, s *Service) {
	t.Helper()
	_, e := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["mine"] = Job{Owner: "uid:alice", Destination: "/home", Stage: filepath.Join(s.Data, "staging", "private"), Filename: "file.bin"}
		st.Jobs["other"] = Job{Owner: "uid:bob"}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
}
func TestAnonymousAndUnknownEndpointsNeverReachEngine(t *testing.T) {
	s, _, calls := fixture(t)
	for _, p := range []string{"/api/v1/proxy", "/api/v1/tasks/batch", "/fs/tasks/mine/file", "/api/v1/config", "/api/v1/tasks/pause", "/api/v1/extensions"} {
		out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "POST", Path: p})
		if out["success"] != false {
			t.Fatal(p, out)
		}
	}
	out, _ := call(t, s, "", "gopeed.request", Input{Method: "GET", Path: "/api/v1/info"})
	if out["success"] != false || *calls != 0 {
		t.Fatal(out, *calls)
	}
}
func TestTasksOwnerFilterAndForeignBatchRefusal(t *testing.T) {
	s, _, calls := fixture(t)
	seed(t, s)
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "other") || strings.Contains(string(b), "private") || !strings.Contains(string(b), "/home") {
		t.Fatal(string(b))
	}
	before := *calls
	for _, p := range []string{"/api/v1/tasks/other", "/api/v1/tasks/other/continue", "/api/v1/tasks/pause"} {
		out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: p})
		if out["success"] != false {
			t.Fatal(out)
		}
	}
	if *calls != before {
		t.Fatal("foreign reached engine")
	}
}
func TestCurrentPermissionGatesFolderAndPublication(t *testing.T) {
	s, f, _ := fixture(t)
	seed(t, s)
	p := filepath.Join(s.Data, "staging", "private")
	os.MkdirAll(p, 0700)
	os.WriteFile(filepath.Join(p, "file.bin"), []byte("payload"), 0600)
	f.deny = true
	out, _ := call(t, s, "alice", "gopeed.folder", map[string]string{"folder": "/home"})
	if out["success"] != false {
		t.Fatal(out)
	}
	out, _ = call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false || f.uploaded != "" {
		t.Fatal(out)
	}
	f.deny = false
	out, _ = call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false || f.uploaded != "" {
		t.Fatal("legacy task bypassed delegate grant", out, f.uploaded)
	}
	out, _ = call(t, s, "bob", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false {
		t.Fatal("foreign publication")
	}
}
func TestSymlinkAndUncertainPublishFailClosed(t *testing.T) {
	s, f, _ := fixture(t)
	seed(t, s)
	p := filepath.Join(s.Data, "staging", "private")
	os.MkdirAll(p, 0700)
	outside := filepath.Join(s.Data, "outside")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(p, "file.bin"))
	out, _ := call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false || f.uploaded != "" {
		t.Fatal(out)
	}
	os.Remove(filepath.Join(p, "file.bin"))
	os.WriteFile(filepath.Join(p, "file.bin"), []byte("payload"), 0600)
	f.uncertain = true
	out, _ = call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false {
		t.Fatal(out)
	}
	f.uploaded = ""
	out, _ = call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false || f.uploaded != "" {
		t.Fatal("uncertain retry", out)
	}
}

func TestExplicitPauseIntentSurvivesDaemonRestart(t *testing.T) {
	s, _, _ := fixture(t)
	seed(t, s)
	for _, op := range []string{"pause", "continue"} {
		out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/mine/" + op})
		if out["success"] != true {
			t.Fatal(out)
		}
		_, e := s.withState(context.Background(), func(st *State) (any, error) {
			if st.Jobs["mine"].ManualPaused != (op == "pause") {
				t.Fatal("pause intent not persisted", op)
			}
			return nil, nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
}

func TestPublicationFailureReasonPersistsWithoutCredentials(t *testing.T) {
	s, f := stagedDelegate(t)
	f.err = refused("denied")
	out, _ := call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] != false {
		t.Fatal(out)
	}
	b, e := os.ReadFile(filepath.Join(s.Data, "jobs.json"))
	if e != nil {
		t.Fatal(e)
	}
	var st map[string]any
	json.Unmarshal(b, &st)
	job := st["jobs"].(map[string]any)["mine"].(map[string]any)
	if job["saveError"] != "denied" {
		t.Fatal("missing durable error", job)
	}
	if strings.Contains(string(b), "current-session") || strings.Contains(string(b), "current-token") {
		t.Fatal("persisted credentials")
	}
}

// refuseTransport keeps tests off the network: Content-Type probes fail fast.
type refuseTransport struct{}

func (refuseTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

// The contract gate uses the CLI validator on actual authenticated responses.
func recordRegistryResult(t *testing.T, method string, out map[string]any) {
	t.Helper()
	if file := os.Getenv("REGISTRY_RESULTS"); file != "" && out["success"] == true {
		f, err := os.OpenFile(file, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewEncoder(f).Encode(map[string]any{"method": method, "data": out["data"]})
		f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
}
