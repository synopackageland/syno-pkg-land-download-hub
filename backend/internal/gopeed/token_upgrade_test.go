package gopeed

import (
	"bytes"
	"context"
	"dsmapp/internal/binaries"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A legacy engine and its persisted jobs survive an upgrade to the rotating
// supervisor. This uses the official host binary, never DSM or a NAS.
func TestOfficialEngineTokenUpgradeKeepsDownloadsAndGateway(t *testing.T) {
	binary := os.Getenv("GOPEED_TEST_BINARY")
	if binary == "" {
		t.Skip("set GOPEED_TEST_BINARY for official executable contract")
	}
	binary, err := filepath.Abs(binary)
	if err != nil {
		t.Fatal(err)
	}
	data, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(data, "staging")
	if err := os.Mkdir(stage, 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	cfg := filepath.Join(data, "engine.json")
	legacyToken := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(data, "engine.token"), []byte(legacyToken), 0600); err != nil {
		t.Fatal(err)
	}
	if err := binaries.Atomic(cfg, map[string]any{"address": "127.0.0.1", "port": port, "apiToken": legacyToken, "storageDir": filepath.Join(data, "storage"), "whiteDownloadDirs": []string{stage}, "downloadConfig": map[string]any{"downloadDir": stage, "extra": map[string]any{"autoStartTasks": false}}}); err != nil {
		t.Fatal(err)
	}
	logfile, err := os.CreateTemp(data, ".test-engine-log-*")
	if err != nil {
		t.Fatal(err)
	}
	defer logfile.Close()
	process := exec.Command(binary, "-c", cfg)
	process.Stdout, process.Stderr = logfile, logfile
	if err := process.Start(); err != nil {
		t.Fatal(err)
	}
	stopLegacy := func() {
		if process == nil {
			return
		}
		process.Process.Signal(os.Interrupt)
		done := make(chan error, 1)
		go func() { done <- process.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			process.Process.Kill()
			<-done
		}
		process = nil
	}
	defer stopLegacy()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s := New(data)
	s.Engine = "http://127.0.0.1:" + strconv.Itoa(port)
	wait := func(description string, ok func() bool) {
		t.Helper()
		for ctx.Err() == nil {
			if ok() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatal(description)
	}
	wait("legacy engine not ready", func() bool { r, e := s.engine(ctx, "GET", "/api/v1/info", nil, nil); return e == nil && r.Code == 0 })
	payload := bytes.Repeat([]byte("token-upgrade-download\n"), 8192)
	complete := make(chan struct{})
	started := make(chan struct{}, 1)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-complete:
			http.ServeContent(w, r, "fixture.bin", time.Unix(1, 0), bytes.NewReader(payload))
			return
		case <-r.Context().Done():
			return
		}
	}))
	defer source.Close()
	defer func() {
		select {
		case <-complete:
		default:
			close(complete)
		}
	}()
	create := func(name string) string {
		t.Helper()
		r, err := s.engine(ctx, "POST", "/api/v1/tasks", nil, map[string]any{"req": map[string]any{"url": source.URL + "/fixture.bin"}, "opts": map[string]any{"path": stage, "name": name}})
		var id string
		if err != nil || r.Code != 0 || json.Unmarshal(r.Data, &id) != nil || id == "" {
			t.Fatalf("could not create legacy task: err=%v code=%d", err, r.Code)
		}
		return id
	}
	interrupted := create("continued.bin")
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("legacy download never started")
	}
	manual := create("manual.bin")
	if r, err := s.engine(ctx, "PUT", "/api/v1/tasks/"+manual+"/pause", nil, nil); err != nil || r.Code != 0 {
		t.Fatal("could not pause legacy task")
	}
	if err := binaries.Atomic(filepath.Join(data, "jobs.json"), map[string]any{"jobs": map[string]any{interrupted: map[string]any{}, manual: map[string]any{"manualPaused": true}}}); err != nil {
		t.Fatal(err)
	}
	stopLegacy()
	close(complete)
	// Capture the supervisor and child's actual logs without exposing credentials
	// in test output, including on assertion failure.
	stdout, stderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = logfile, logfile
	defer func() { os.Stdout, os.Stderr = stdout, stderr }()
	hash, err := binaries.Hash(binary)
	if err != nil {
		t.Fatal(err)
	}
	runCtx, stop := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- binaries.Supervise(runCtx, data, binaries.Selection{Path: binary, Version: "1.9.3", SHA256: hash}, cfg, s.Engine)
	}()
	stopped := false
	stopSupervisor := func() {
		if stopped {
			return
		}
		stopped = true
		stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}
	defer stopSupervisor()
	wait("upgraded supervisor not ready", func() bool { st, e := binaries.ReadRuntime(data); return e == nil && st.Ready })
	token, err := os.ReadFile(filepath.Join(data, "engine.token"))
	if err != nil || string(token) == legacyToken {
		t.Fatal("upgrade reused legacy token")
	}
	if r, err := s.engine(ctx, "GET", "/api/v1/info", nil, nil); err != nil || r.Code != 0 {
		t.Fatal("existing gateway did not pick up rotated token")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.Engine+"/api/v1/info", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Api-Token", legacyToken)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		t.Fatal("legacy-token probe failed")
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatal("old token still authorized")
	}
	status := func(id string) string {
		r, e := s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
		var task struct{ Status string }
		if e != nil || r.Code != 0 || json.Unmarshal(r.Data, &task) != nil {
			return ""
		}
		return task.Status
	}
	wait("original download did not complete after token rotation", func() bool { return status(interrupted) == "done" })
	if status(manual) != "pause" {
		t.Fatal("manual pause was lost during upgrade")
	}
	stopSupervisor()
	if got, err := os.ReadFile(filepath.Join(stage, "continued.bin")); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("continued download bytes changed: err=%v size=%d want=%d", err, len(got), len(payload))
	}
	logs, err := os.ReadFile(logfile.Name())
	if err != nil || bytes.Contains(logs, token) || bytes.Contains(logs, []byte(legacyToken)) {
		t.Fatal("engine logs contain credentials or could not be checked")
	}
	if err := filepath.WalkDir(filepath.Join(data, "storage", "logs"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err == nil && (bytes.Contains(raw, token) || bytes.Contains(raw, []byte(legacyToken))) {
			t.Error("upstream log contains credentials")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
}
