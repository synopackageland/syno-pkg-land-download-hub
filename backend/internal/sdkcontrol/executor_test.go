package sdkcontrol

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// useSocketBase points executor requests at a short temporary directory:
// unix socket paths are limited to about 100 bytes.
func useSocketBase(t *testing.T) string {
	t.Helper()
	base, err := os.MkdirTemp("/tmp", "exec")
	if err != nil {
		t.Fatal(err)
	}
	previous := executorSocketBase
	executorSocketBase = base
	t.Cleanup(func() { executorSocketBase = previous; os.RemoveAll(base) })
	return base
}

type adminAuth struct{}

func (adminAuth) Authenticate(*http.Request) (api.Identity, error) {
	return api.Identity{User: "admin", Admin: true}, nil
}

// fakeExecutor answers each v1 request with replies[op] and records the ops
// and the auth each one carried.
type fakeExecutor struct {
	mu   sync.Mutex
	ops  []string
	auth map[string]map[string]string
}

func (f *fakeExecutor) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ops...)
}

func (f *fakeExecutor) authOf(op string) map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.auth[op]
}

func startFakeExecutor(t *testing.T, cfg Config, replies map[string]string) *fakeExecutor {
	t.Helper()
	base := useSocketBase(t)
	dir := filepath.Join(base, cfg.PackageID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("unix", filepath.Join(dir, "executor.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	f := &fakeExecutor{auth: map[string]map[string]string{}}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var req struct {
				Op   string
				Auth map[string]string
			}
			line, _ := bufio.NewReader(conn).ReadBytes('\n')
			json.Unmarshal(line, &req)
			f.mu.Lock()
			f.ops = append(f.ops, req.Op)
			f.auth[req.Op] = req.Auth
			f.mu.Unlock()
			conn.Write([]byte(replies[req.Op] + "\n"))
			conn.Close()
		}
	}()
	return f
}

// serve calls method as an administrator with a current DSM session from
// remoteAddr and answers the error code, empty on success, and the envelope.
func serve(t *testing.T, cfg Config, method, params, remoteAddr string) (string, map[string]any) {
	t.Helper()
	server, err := api.NewServerWithPermissions(adminAuth{}, []byte(`{"schema":1,"default":"administrators","methods":{}}`), cfg.Routes()...)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://nas/api", strings.NewReader(`{"method":"`+method+`","params":`+params+`}`))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Cookie", "id=session")
	r.Header.Set("X-SYNO-TOKEN", "token")
	r.RemoteAddr = remoteAddr
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s: %s", method, w.Body)
	}
	e, _ := out["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code, out
}

func rootRun(t *testing.T, cfg Config) (string, map[string]any) {
	t.Helper()
	return serve(t, cfg, "root.run", `{"name":"whoami"}`, "192.0.2.7:51234")
}

const readyStatus = `{"ok":true,"result":{"executor_digest":"e1","policy_digest":"p1","ready":true,"disabled":false,"version":"1"}}`

func pinnedConfig(t *testing.T) Config {
	return Config{PackageID: "SynoPkgGopeed", DataDir: t.TempDir(), ExecutorDigest: "e1", PolicyDigest: "p1"}
}

// executor.status is the SDK's: an executor refusal keeps its code instead
// of becoming "internal".
func TestExecutorStatusKeepsTheExecutorsRefusalCode(t *testing.T) {
	cfg := pinnedConfig(t)
	startFakeExecutor(t, cfg, map[string]string{"status": `{"ok":false,"code":"EXECUTOR_PAUSED","message":"paused"}`})
	if code, out := serve(t, cfg, "executor.status", `{}`, "192.0.2.7:51234"); code != "EXECUTOR_PAUSED" {
		t.Fatalf("status: %q %v", code, out)
	}
}

// SDK 0.9.4 checks IP-limited App Privileges against the browser's address:
// root.run forwards the DSM session and that address, nothing else.
func TestRootRunForwardsTheBrowserAddress(t *testing.T) {
	cfg := pinnedConfig(t)
	f := startFakeExecutor(t, cfg, map[string]string{"status": readyStatus, "root.run": `{"ok":true,"result":{}}`})
	if code, out := rootRun(t, cfg); code != "" {
		t.Fatalf("%s %v", code, out)
	}
	auth := f.authOf("root.run")
	if len(auth) != 3 || auth["dsm_cookie"] != "session" || auth["dsm_synotoken"] != "token" || auth["remote_addr"] != "192.0.2.7" {
		t.Fatalf("auth: %v", auth)
	}
}

func TestExecutorRootRunRunsWhenTheExecutorHoldsThisBuildsPins(t *testing.T) {
	cfg := pinnedConfig(t)
	startFakeExecutor(t, cfg, map[string]string{"status": readyStatus, "root.run": `{"ok":true,"result":{"output":"0\n"}}`})
	code, out := rootRun(t, cfg)
	if data, _ := out["data"].(map[string]any); code != "" || data["output"] != "0\n" {
		t.Fatalf("%s %v", code, out)
	}
}

// Like delegate: an executor pinned to another build's policy or executor
// refuses the operation, it does not run it.
func TestExecutorRootRunRefusesAnExecutorPinnedToAnotherBuild(t *testing.T) {
	for name, status := range map[string]string{
		"policy":   strings.Replace(readyStatus, `"p1"`, `"p2"`, 1),
		"executor": strings.Replace(readyStatus, `"e1"`, `"e2"`, 1),
	} {
		cfg := pinnedConfig(t)
		f := startFakeExecutor(t, cfg, map[string]string{"status": status, "root.run": `{"ok":true,"result":{"output":"0\n"}}`})
		if code, out := rootRun(t, cfg); code != "POLICY_DENIED" {
			t.Errorf("%s digest mismatch: %q %v", name, code, out)
		}
		for _, op := range f.received() {
			if op == "root.run" {
				t.Errorf("%s digest mismatch: root.run reached the executor", name)
			}
		}
	}
}

// Refusals keep the protocol v1 code (spec/protocol/v1/errors.json), never "internal".
func TestExecutorRootRunReportsTheExecutorsRefusalCode(t *testing.T) {
	cfg := pinnedConfig(t)
	useSocketBase(t)
	if code, _ := rootRun(t, cfg); code != "EXECUTOR_UNAVAILABLE" {
		t.Errorf("paused or awaiting approval (no socket): %q", code)
	}
	for _, refusal := range []string{"POLICY_DENIED", "INVALID_REQUEST", "EXECUTOR_PAUSED", "ROOT_FAILED", "ADMIN_REQUIRED"} {
		cfg := pinnedConfig(t)
		startFakeExecutor(t, cfg, map[string]string{"status": readyStatus, "root.run": `{"ok":false,"code":"` + refusal + `","message":"refused"}`})
		if code, out := rootRun(t, cfg); code != refusal {
			t.Errorf("%s: %q %v", refusal, code, out)
		}
	}
}

// The executor client waits as long as the SDK says (spec/protocol/v1,
// Timeouts): delegate.ExecutorTimeout for status and control, root.run longer.
func TestTheExecutorClientWaitsTheSDKsTimeout(t *testing.T) {
	if got := (Config{PackageID: "SynoPkgGopeed"}).executor().Timeout; got != 0 {
		t.Fatalf("executor client sets its own wait %v instead of the SDK's", got)
	}
}
