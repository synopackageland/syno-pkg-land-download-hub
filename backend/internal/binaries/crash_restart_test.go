package binaries

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMain lets this test binary act as a fake engine: Supervise starts it
// with -c <cfg> -A 127.0.0.1 -P <port>.
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_GOPEED_ENGINE") != "" {
		fakeEngine()
		return
	}
	os.Exit(m.Run())
}

// fakeEngine serves the info and task list endpoints, after
// FAKE_GOPEED_DELAY_MS milliseconds when set. While fake-starts has
// not passed fake-crashes it exits with status 2 shortly after becoming
// healthy, as the official engine does on a panic; while it has not passed
// fake-hangs it stops itself instead, as SIGSTOP did on the device.
func fakeEngine() {
	cfg, port := os.Args[2], os.Args[6]
	dir := filepath.Dir(cfg)
	raw, err := os.ReadFile(cfg)
	var config struct{ ApiToken string }
	if err != nil || json.Unmarshal(raw, &config) != nil {
		os.Exit(4)
	}
	history, err := os.OpenFile(filepath.Join(dir, "fake-token-hashes"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		os.Exit(4)
	}
	fmt.Fprintf(history, "%x\n", sha256.Sum256([]byte(config.ApiToken)))
	history.Close()
	starts := readCount(filepath.Join(dir, "fake-starts")) + 1
	os.WriteFile(filepath.Join(dir, "fake-starts"), []byte(strconv.Itoa(starts)), 0600)
	os.WriteFile(filepath.Join(dir, "fake-pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	delay, _ := strconv.Atoi(os.Getenv("FAKE_GOPEED_DELAY_MS"))
	time.Sleep(time.Duration(delay) * time.Millisecond)
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(3)
	}
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Token") != config.ApiToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/v1/info" {
			fmt.Fprint(w, `{"code":0,"data":{"version":"1.9.3"}}`)
			return
		}
		fmt.Fprint(w, `{"code":0,"data":[]}`)
	}))
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	if starts <= readCount(filepath.Join(dir, "fake-hangs")) {
		time.Sleep(300 * time.Millisecond)
		syscall.Kill(os.Getpid(), syscall.SIGSTOP)
	}
	if starts <= readCount(filepath.Join(dir, "fake-crashes")) {
		select {
		case <-stop:
			os.Exit(0)
		case <-time.After(300 * time.Millisecond):
			os.Exit(2)
		}
	}
	<-stop
	os.Exit(0)
}

func readCount(p string) int {
	b, _ := os.ReadFile(p)
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

type fakeSupervision struct {
	data string
	done chan error
	stop context.CancelFunc
}

func superviseFake(t *testing.T, crashes, hangs int, saved *Runtime) fakeSupervision {
	t.Helper()
	return superviseFakeWith(t, crashes, hangs, saved, HealthPolicy{Interval: 50 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 3})
}

func superviseFakeWith(t *testing.T, crashes, hangs int, saved *Runtime, health HealthPolicy) fakeSupervision {
	t.Helper()
	return superviseFakeRecord(t, crashes, hangs, saved, health, true)
}

// superviseFakeRecord saves the record as given unless named, which makes it
// name the fake engine.
func superviseFakeRecord(t *testing.T, crashes, hangs int, saved *Runtime, health HealthPolicy, named bool) fakeSupervision {
	t.Helper()
	t.Setenv("FAKE_GOPEED_ENGINE", "1")
	before, beforeHealth := restartPolicy, healthPolicy
	restartPolicy = RestartPolicy{Limit: 3, Window: time.Minute, Backoff: func(int) time.Duration { return 10 * time.Millisecond }}
	healthPolicy = health
	t.Cleanup(func() { restartPolicy, healthPolicy = before, beforeHealth })
	data, _ := filepath.EvalSymlinks(t.TempDir())
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	h, err := Hash(exe)
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
	if err := Atomic(filepath.Join(data, "engine.json"), map[string]any{"apiToken": strings.Repeat("a", 64)}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(data, "fake-crashes"), []byte(strconv.Itoa(crashes)), 0600)
	os.WriteFile(filepath.Join(data, "fake-hangs"), []byte(strconv.Itoa(hangs)), 0600)
	selection := Selection{Path: exe, Version: "1.9.3", SHA256: h}
	if saved != nil {
		if named {
			saved.Active = selection
		}
		if err = Atomic(filepath.Join(data, "runtime.json"), saved); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() {
		done <- Supervise(ctx, data, selection, filepath.Join(data, "engine.json"), fmt.Sprintf("http://127.0.0.1:%d", port))
	}()
	return fakeSupervision{data: data, done: done, stop: cancel}
}

// waitRuntime polls runtime.json until ok accepts it.
func (f fakeSupervision) waitRuntime(t *testing.T, ok func(Runtime) bool) Runtime {
	t.Helper()
	var st Runtime
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); time.Sleep(20 * time.Millisecond) {
		st, _ = ReadRuntime(f.data)
		if ok(st) {
			return st
		}
	}
	t.Fatalf("runtime never matched: %+v", st)
	return st
}

func TestSupervisorRestartsTheEngineAfterACrash(t *testing.T) {
	f := superviseFake(t, 1, 0, nil)
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready && readCount(filepath.Join(f.data, "fake-starts")) == 2 })
	st := f.waitRuntime(t, func(st Runtime) bool { return st.Ready })
	f.stop()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
	if st.Error != "" {
		t.Fatalf("recovered engine still reports %q", st.Error)
	}
	history, err := os.ReadFile(filepath.Join(f.data, "fake-token-hashes"))
	if err != nil {
		t.Fatal(err)
	}
	hashes := strings.Fields(string(history))
	if len(hashes) != 2 || hashes[0] == hashes[1] {
		t.Fatal("engine restart reused its API token")
	}
}

func TestSupervisorGivesUpOnACrashLoop(t *testing.T) {
	f := superviseFake(t, 100, 0, nil)
	select {
	case err := <-f.done:
		if err == nil {
			t.Fatal("crash loop ended without an error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("crash loop never stopped")
	}
	st, _ := ReadRuntime(f.data)
	if st.Ready || st.Error != "engine_crash_loop" {
		t.Fatalf("%+v", st)
	}
	if n := readCount(filepath.Join(f.data, "fake-starts")); n != restartPolicy.Limit {
		t.Fatalf("started %d times", n)
	}
}

func TestSupervisorStartClearsAPreviousEngineError(t *testing.T) {
	f := superviseFake(t, 0, 0, &Runtime{Error: "engine_crash_loop", Ready: true})
	st := f.waitRuntime(t, func(st Runtime) bool { return st.Ready && readCount(filepath.Join(f.data, "fake-starts")) == 1 })
	if st.Error != "" {
		t.Fatalf("stale error %q", st.Error)
	}
	f.stop()
	<-f.done
}

func TestSupervisorReportsARefusedSelectionAsACode(t *testing.T) {
	f := superviseFake(t, 0, 0, nil)
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready })
	req := Request{ID: "r1", Selection: Selection{Path: filepath.Join(f.data, "missing"), Version: "9.9.9", SHA256: strings.Repeat("0", 64)}}
	if err := Atomic(filepath.Join(f.data, "engine-request.json"), req); err != nil {
		t.Fatal(err)
	}
	st := f.waitRuntime(t, func(st Runtime) bool { return st.Request == "r1" })
	if st.Error != "selection_verification_failed" {
		t.Fatalf("runtime error %q is not a code the UI can translate", st.Error)
	}
	f.stop()
	<-f.done
}

// A stopped engine keeps its socket but never answers: the
// runtime stops claiming ready, then the supervisor restarts it.
func TestSupervisorRestartsAnUnresponsiveEngine(t *testing.T) {
	f := superviseFake(t, 0, 1, nil)
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready })
	if st := f.waitRuntime(t, func(st Runtime) bool { return !st.Ready }); st.Error != "engine_unavailable" {
		t.Fatalf("unresponsive engine reports %q", st.Error)
	}
	st := f.waitRuntime(t, func(st Runtime) bool { return st.Ready && readCount(filepath.Join(f.data, "fake-starts")) == 2 })
	if st.Error != "" {
		t.Fatalf("recovered engine still reports %q", st.Error)
	}
	f.stop()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
}

func TestSupervisorKeepsAnEngineThatAnswersAgain(t *testing.T) {
	f := superviseFakeWith(t, 0, 0, nil, HealthPolicy{Interval: 50 * time.Millisecond, Timeout: 100 * time.Millisecond, Failures: 1000})
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready })
	req := Request{ID: "r1", Selection: Selection{Path: filepath.Join(f.data, "missing"), Version: "9.9.9", SHA256: strings.Repeat("0", 64)}}
	if err := Atomic(filepath.Join(f.data, "engine-request.json"), req); err != nil {
		t.Fatal(err)
	}
	f.waitRuntime(t, func(st Runtime) bool { return st.Request == "r1" })
	pid := readCount(filepath.Join(f.data, "fake-pid"))
	syscall.Kill(pid, syscall.SIGSTOP)
	f.waitRuntime(t, func(st Runtime) bool { return !st.Ready && st.Error == "engine_unavailable" })
	syscall.Kill(pid, syscall.SIGCONT)
	st := f.waitRuntime(t, func(st Runtime) bool { return st.Ready })
	if st.Error != "selection_verification_failed" || readCount(filepath.Join(f.data, "fake-starts")) != 1 {
		t.Fatalf("resumed engine: %+v", st)
	}
	f.stop()
	<-f.done
}

func TestSupervisorCountsUnresponsiveRestartsTowardTheCrashLoop(t *testing.T) {
	f := superviseFake(t, 0, 100, nil)
	select {
	case err := <-f.done:
		if err == nil {
			t.Fatal("crash loop ended without an error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("crash loop never stopped")
	}
	st, _ := ReadRuntime(f.data)
	if st.Ready || st.Error != "engine_crash_loop" {
		t.Fatalf("%+v", st)
	}
	if n := readCount(filepath.Join(f.data, "fake-starts")); n != restartPolicy.Limit {
		t.Fatalf("started %d times", n)
	}
}
