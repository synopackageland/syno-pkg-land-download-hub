package gopeed

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// GOPEED_TEST_BINARY explicitly selects a locally downloaded official host
// executable. This checks the actual pinned upstream API, not a fake daemon.
// DSM authentication and File Station remain fixtures, never NAS evidence.
func TestOfficialEngineRefusesDownloadWithoutDelegate(t *testing.T) { runOfficialEngine(t, false) }
func TestOfficialEngineNormalDownloadAndDelegatedCompletionAfterLogout(t *testing.T) {
	runOfficialEngine(t, true)
}
func officialEngine(t *testing.T) *Service {
	t.Helper()
	binary := os.Getenv("GOPEED_TEST_BINARY")
	if binary == "" {
		t.Skip("set GOPEED_TEST_BINARY for official executable contract")
	}
	binary, _ = filepath.Abs(binary)
	data := t.TempDir()
	data, _ = filepath.EvalSymlinks(data)
	token := strings.Repeat("a", 64)
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(token), 0600)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	b, _ := json.Marshal(map[string]any{"address": "127.0.0.1", "port": port, "apiToken": token, "storageDir": filepath.Join(data, "storage"), "whiteDownloadDirs": []string{filepath.Join(data, "staging", "*"), filepath.Join(data, "executor-staging", "*")}})
	cfg := filepath.Join(data, "engine.json")
	os.WriteFile(cfg, b, 0600)
	process := exec.Command(binary, "-c", cfg)
	logfile, e := os.Create(filepath.Join(data, "engine.log"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { logfile.Close() })
	process.Stdout = logfile
	process.Stderr = logfile
	if e = process.Start(); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		process.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			process.Process.Kill()
			<-done
		}
	})
	// DSM exposes package var through a symlink while the daemon receives its
	// canonical volume path. The engine's whitelist matches literal paths.
	alias := filepath.Join(t.TempDir(), "package-var")
	if e := os.Symlink(data, alias); e != nil {
		t.Fatal(e)
	}
	s := New(alias)
	s.StagingDir = filepath.Join(data, "executor-staging")
	s.Engine = "http://127.0.0.1:" + strconv.Itoa(port)
	s.Owner = func(u string) (string, error) { return "uid:" + u, nil }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ready := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); {
		if rep, e := s.engine(ctx, "GET", "/api/v1/info", nil, nil); e == nil && rep.Code == 0 {
			ready = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ready {
		t.Fatal("official API did not become ready")
	}
	return s
}

func runOfficialEngine(t *testing.T, delegated bool) {
	s := officialEngine(t)
	data := s.Data
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	payload := strings.Repeat("official-engine-contract\n", 1000)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="fixture.bin"`)
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		fmt.Fprint(w, payload)
	}))
	defer source.Close()
	// Test-only URL policy accepts exactly this loopback fixture; production has
	// no browser/API switch to override the URL admission policy.
	s.checkURL = func(u string) bool { return u == source.URL+"/fixture.bin" }
	if delegated {
		currentUser, _ := user.Current()
		destinationSDK := &delegate.Service{Scope: "install-selected", Lifetime: "until-revoked", Destinations: []string{"save"}, LookupUID: func(string) string { return currentUser.Uid }}
		destinationSDK.Client = appExecutor(func(_ context.Context, op string, auth map[string]string, p, out any) error {
			if op == "status" {
				disabled, allowed := false, true
				return encodeResult(out, delegate.ExecutorStatus{ExecutorDigest: "executor", PolicyDigest: "policy", Version: "fixture", Ready: true, Disabled: &disabled, Allowed: &allowed, Selection: &delegate.ScopeSelection{Scope: "home", Root: "/home", Generation: 1}})
			}
			if op == "files.checkPermission" {
				return encodeResult(out, map[string]bool{"allowed": true})
			}
			if op != "delegate.list" || auth["dsm_cookie"] != "current-session" {
				t.Fatalf("unexpected destination lookup %s", op)
			}
			return encodeResult(out, map[string]any{"delegations": []delegate.Grant{{DelegationID: "grant1", Subject: delegate.Subject{Username: "normal1", UID: json.Number(currentUser.Uid)}, Lifetime: "until-revoked", Destination: "save", Path: "/home", Scope: "home", ScopeGeneration: 1}}})
		})
		s.DestinationDelegate = destinationSDK
		_, e := s.withState(ctx, func(st *State) (any, error) {
			st.Folders["uid:normal1"] = "/home"
			st.Grants["uid:normal1"] = "grant1"
			return nil, nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	task := map[string]any{"req": map[string]any{"url": source.URL + "/fixture.bin"}, "opts": map[string]any{"path": "/home", "name": "fixture.bin"}}
	out := resolveAndCreate(t, s, "normal1", task)
	if !delegated {
		if out["success"] != false {
			t.Fatal("ungranted download admitted", out)
		}
		return
	}
	if out["success"] != true {
		t.Fatal(out)
	}
	rep := out["data"].(map[string]any)
	if rep["code"] != float64(0) {
		t.Fatal(out)
	}
	id := rep["data"].(string)
	complete := false
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		r, e := s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
		var task struct {
			Status string `json:"status"`
		}
		if e == nil && json.Unmarshal(r.Data, &task) == nil && task.Status == "done" {
			complete = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !complete {
		raw, _ := os.ReadFile(filepath.Join(data, "engine.log"))
		t.Fatalf("not complete: %s", raw)
	}
	out, _ = call(t, s, "normal2", "gopeed.publish", map[string]string{"id": id})
	if out["success"] != false {
		t.Fatal("foreign task published")
	}
	if delegated {
		moves := 0
		state := "queued"
		sdk := &delegate.Service{Store: background.NewRequestStore(filepath.Join(data, "background")), StagingDir: s.StagingDir, Operations: []string{"files.moveFromStaging"}, Lifetime: "until-revoked"}
		sdk.Client = appExecutor(func(_ context.Context, op string, auth map[string]string, p, out any) error {
			if len(auth) != 0 {
				t.Fatal("offline credential leak")
			}
			switch op {
			case "files.moveFromStaging":
				moves++
				in := p.(delegate.MoveParams)
				b, e := os.ReadFile(filepath.Join(s.StagingDir, in.Staging))
				if e != nil || string(b) != payload {
					t.Fatal("official staged bytes mismatch", e)
				}
				return encodeResult(out, delegate.OperationResult{JobID: operationID(id), State: "queued"})
			case "files.operationStatus":
				r := delegate.OperationResult{JobID: operationID(id), State: state}
				if state == "committed" {
					r.Receipt = &delegate.Receipt{Path: "/home/fixture.bin", Size: int64(len(payload))}
				}
				return encodeResult(out, r)
			default:
				t.Fatalf("unexpected operation %s", op)
				return nil
			}
		})
		s.Delegate = sdk
		if e := s.SaveCompleted(ctx); e != nil {
			t.Fatal(e)
		}
		state = "committed"
		if e := s.SaveCompleted(ctx); e != nil {
			t.Fatal(e)
		}
		if moves != 1 {
			t.Fatal("replay or session fallback")
		}
	}

	response, e := http.Get(s.Engine + "/api/v1/tasks")
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("anonymous engine access", response.StatusCode)
	}
}
