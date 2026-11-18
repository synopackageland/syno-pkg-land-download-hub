package gopeed

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

func recoverySDK(s *Service, store string, client delegate.ExecutorClient) *delegate.Service {
	return &delegate.Service{Client: client, Store: background.NewRequestStore(store), StagingDir: s.StagingDir, Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}}
}

// Real SDK/client/journal; fixture executor receipts do not certify NAS writes.
func TestC1AbsentSocketRecoversSameRefExactlyOnce(t *testing.T) {
	s, _ := stagedDelegate(t)
	dir, e := os.MkdirTemp("/tmp", "admission-recovery-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "executor.sock")
	sdk := recoverySDK(s, filepath.Join(dir, "mirror"), &delegate.Client{Socket: socket, Timeout: time.Second})
	s.Delegate = sdk
	if e = s.SaveCompleted(context.Background()); e == nil {
		t.Fatal("expected unavailable socket")
	}
	before := readJob(t, s)
	if before.Uncertain || before.SaveState != "pending" {
		t.Fatalf("retry must remain pending: %#v", before)
	}
	ln, e := net.Listen("unix", socket)
	if e != nil {
		t.Fatal(e)
	}
	defer ln.Close()
	var mu sync.Mutex
	moves := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, e := ln.Accept()
			if e != nil {
				return
			}
			line, _ := bufio.NewReader(c).ReadBytes('\n')
			var r struct {
				Op     string          `json:"op"`
				Params json.RawMessage `json:"params"`
			}
			_ = json.Unmarshal(line, &r)
			mu.Lock()
			if r.Op == "files.moveFromStaging" {
				moves++
			}
			mu.Unlock()
			_, _ = fmt.Fprintln(c, `{"ok":true,"result":{"job_id":"gopeed-mine","state":"committed","receipt":{"path":"/home/Downloads/file.bin","size":8}}}`)
			c.Close()
		}
	}()
	restarted := New(s.Data)
	restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, recoverySDK(s, filepath.Join(dir, "mirror"), &delegate.Client{Socket: socket, Timeout: time.Second})
	if e = restarted.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := readJob(t, restarted)
	if !after.Published || before.SaveJobID != after.SaveJobID || before.DelegationID != after.DelegationID {
		t.Fatal(after)
	}
	for i := 0; i < 3; i++ {
		_ = restarted.SaveCompleted(context.Background())
	}
	ln.Close()
	<-done
	mu.Lock()
	defer mu.Unlock()
	if moves != 1 {
		t.Fatal("admission count", moves)
	}
}

func TestC1CrashBeforeTransportRestartCommitsOriginalParams(t *testing.T) {
	s, _ := stagedDelegate(t)
	store := filepath.Join(t.TempDir(), "mirror")
	moves := 0
	crash := true
	exec := appExecutor(func(_ context.Context, op string, _ map[string]string, p, out any) error {
		if op == "files.moveFromStaging" {
			if crash {
				panic("injected exit before transport")
			}
			moves++
			if p.(delegate.MoveParams).JobID != "gopeed-mine" {
				t.Fatal(p)
			}
			return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "queued"})
		}
		if moves == 0 {
			return &api.Error{Code: "operation-not-found"}
		}
		return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "committed", Receipt: &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}})
	})
	sdk := recoverySDK(s, store, exec)
	s.Delegate = sdk
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected injected crash")
			}
		}()
		_ = s.SaveCompleted(context.Background())
	}()
	before := readJob(t, s)
	if !before.SaveStarted || before.SaveParams == nil {
		t.Fatal(before)
	}
	crash = false
	// Wait for the SDK-owned backoff; never change mirrors or params.
	time.Sleep(2100 * time.Millisecond)
	restarted := New(s.Data)
	restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, recoverySDK(s, store, exec)
	if e := restarted.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	after := readJob(t, restarted)
	if !after.Published || after.SaveJobID != before.SaveJobID || after.SaveParams == nil || *after.SaveParams != *before.SaveParams || moves != 1 {
		t.Fatal(after, moves)
	}
	_ = restarted.SaveCompleted(context.Background())
	if moves != 1 {
		t.Fatal("replayed committed", moves)
	}
}

func TestC1PerJobBudgetAndListResponsiveDuringAdmission(t *testing.T) {
	s, _ := stagedDelegate(t)
	s.saveTimeout = 120 * time.Millisecond
	first := readJob(t, s)
	_, e := s.withState(context.Background(), func(st *State) (any, error) {
		j := first
		j.Stage = filepath.Join(s.StagingDir, "second")
		st.Jobs["second"] = j
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = os.MkdirAll(filepath.Join(s.StagingDir, "second"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(s.StagingDir, "second/file.bin"), []byte("download"), 0600); e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	calls := 0
	deadlines := []time.Time{}
	admitted := map[string]bool{}
	sdk := recoverySDK(s, filepath.Join(t.TempDir(), "mirror"), appExecutor(func(ctx context.Context, op string, _ map[string]string, p, out any) error {
		if op == "files.operationStatus" {
			id := p.(delegate.OperationRef).JobID
			if !admitted[id] {
				return &api.Error{Code: "operation-not-found"}
			}
			return encodeResult(out, delegate.OperationResult{JobID: id, State: "committed", Receipt: &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}})
		}
		calls++
		in := p.(delegate.MoveParams)
		d, _ := ctx.Deadline()
		deadlines = append(deadlines, d)
		if calls == 1 {
			close(entered)
			<-release
			<-ctx.Done()
			return &api.Error{Code: "canceled"}
		}
		admitted[in.JobID] = true
		return encodeResult(out, delegate.OperationResult{JobID: in.JobID, State: "queued"})
	}))
	s.Delegate = sdk
	done := make(chan error, 1)
	go func() { done <- s.SaveCompleted(context.Background()) }()
	<-entered
	// A real authenticated list route must not wait on the SDK admission lock.
	srv, e := NewServer(actorAuth{}, s.Routes()...)
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req := httptest.NewRequest("POST", "http://fixture/api.cgi", bytesReader([]byte(`{"method":"gopeed.request","params":{"method":"GET","path":"/api/v1/tasks"}}`))).WithContext(ctx)
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SYNO-TOKEN", "current-token")
	req.Header.Set("Cookie", "id=current-session")
	req.Header.Set("actor", "alice")
	response := httptest.NewRecorder()
	srv.ServeHTTP(response, req)
	close(release)
	var reply map[string]any
	_ = json.Unmarshal(response.Body.Bytes(), &reply)
	if reply["success"] != true {
		t.Errorf("list blocked by admission: %v", reply)
	}
	_ = <-done
	if calls != 2 || len(admitted) != 1 || deadlines[0].Equal(deadlines[1]) {
		t.Fatalf("other job lost its budget: calls=%d deadlines=%v admitted=%v", calls, deadlines, admitted)
	}
	time.Sleep(2100 * time.Millisecond)
	if e = s.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	_, _ = s.withState(context.Background(), func(st *State) (any, error) {
		for _, j := range st.Jobs {
			if !j.Published || j.Uncertain {
				t.Fatal(j)
			}
		}
		return nil, nil
	})
	if len(admitted) != 2 || calls != 3 {
		t.Fatal("missing/duplicate admission", calls, admitted)
	}
}

func TestC1RetryAndTerminalCodeMatrix(t *testing.T) {
	for _, code := range []string{"EXECUTOR_UNAVAILABLE", "EXECUTOR_TIMEOUT", "EXECUTOR_PROTOCOL", "canceled", "EXECUTOR_PAUSED", "EXECUTOR_STARTING", "queue-full", "STORAGE_ERROR", "ADMISSION_PENDING", "APP_DENIED", "DELEGATION_DENIED", "operation-conflict", "disabled", "SCOPE_DENIED", "STAGING_CHANGED", "ADMISSION_EXHAUSTED", "operation-not-found", "WRITE_FAILED", "jobs_busy", "jobs_unavailable", "spool_unsafe", "journal_corrupt"} {
		t.Run(code, func(t *testing.T) {
			s, f := stagedDelegate(t)
			f.err = &api.Error{Code: code}
			_ = s.SaveCompleted(context.Background())
			before := readJob(t, s)
			retry := code == "EXECUTOR_UNAVAILABLE" || code == "EXECUTOR_TIMEOUT" || code == "EXECUTOR_PROTOCOL" || code == "canceled" || code == "EXECUTOR_PAUSED" || code == "EXECUTOR_STARTING" || code == "queue-full" || code == "STORAGE_ERROR" || code == "ADMISSION_PENDING" || code == "jobs_busy" || code == "jobs_unavailable"
			if before.Uncertain || before.Published || (retry && before.SaveState != "pending") || (!retry && before.SaveState != "save_failed" && before.SaveState != "access_denied") {
				t.Fatal(before)
			}
			n := f.calls
			_ = s.SaveCompleted(context.Background())
			if retry && f.calls != n+1 || !retry && f.calls != n {
				t.Fatal("incorrect retry", f.calls, n)
			}
			if b, e := os.ReadFile(filepath.Join(before.Stage, before.Filename)); e != nil || string(b) != "download" {
				t.Fatal("lost staging", e)
			}
		})
	}
}

func TestC1LegacyIntentKeepsExactOriginalTupleAndPublishedIsMonotonic(t *testing.T) {
	s, f := stagedDelegate(t)
	_, e := s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs["mine"]
		j.SaveStarted = true
		j.SaveJobID = "original-before-upgrade"
		j.SaveSize = 8
		j.SaveState = "uncertain"
		j.Uncertain = true
		st.Jobs["mine"] = j
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	f.state = "committed"
	f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
	if e = s.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	j := readJob(t, s)
	want := delegate.MoveParams{DelegationID: "grant1", JobID: "original-before-upgrade", Staging: "private/file.bin", Name: "file.bin", Size: 8}
	if j.SaveParams == nil || *j.SaveParams != want || f.input != want || !j.Published {
		t.Fatal(j, f.input)
	}
	stale := j
	stale.Published = false
	stale.SaveState = "pending"
	stale.SaveError = "EXECUTOR_TIMEOUT"
	if e = s.recordSave(context.Background(), "mine", stale); e != nil {
		t.Fatal(e)
	}
	if !readJob(t, s).Published {
		t.Fatal("stale concurrent pass lost committed receipt")
	}
	if e = s.SaveCompleted(context.Background()); e != nil || f.calls != 1 {
		t.Fatal("committed work replayed", e, f.calls)
	}
}

func TestC1KnownOperationLostIsTerminalWithoutSecondAdmission(t *testing.T) {
	s, _ := stagedDelegate(t)
	moves := 0
	lost := false
	sdk := recoverySDK(s, filepath.Join(t.TempDir(), "mirror"), appExecutor(func(_ context.Context, op string, _ map[string]string, _, out any) error {
		if op == "files.moveFromStaging" {
			moves++
			return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "queued"})
		}
		if lost {
			return &api.Error{Code: "operation-not-found"}
		}
		return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "copying"})
	}))
	s.Delegate = sdk
	if e := s.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	lost = true
	if e := s.SaveCompleted(context.Background()); saveCode(e) != "operation-not-found" {
		t.Fatal(e)
	}
	if j := readJob(t, s); j.SaveState != "save_failed" || j.Uncertain || j.Published {
		t.Fatal(j)
	}
	for i := 0; i < 3; i++ {
		_ = s.SaveCompleted(context.Background())
	}
	if moves != 1 {
		t.Fatal("accepted operation was readmitted", moves)
	}
	if b, e := os.ReadFile(filepath.Join(s.StagingDir, "private/file.bin")); e != nil || string(b) != "download" {
		t.Fatal("lost staging", e)
	}
}

func TestC1KnownUncertainSDKOperationConvergesWithoutReadmission(t *testing.T) {
	s, _ := stagedDelegate(t)
	moves := 0
	state := "uncertain"
	sdk := recoverySDK(s, filepath.Join(t.TempDir(), "mirror"), appExecutor(func(_ context.Context, op string, _ map[string]string, _, out any) error {
		if op == "files.moveFromStaging" {
			moves++
			return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "queued"})
		}
		r := delegate.OperationResult{JobID: "gopeed-mine", State: state}
		if state == "uncertain" {
			r.Code = "APP_DENIED"
		} else {
			r.Receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
		}
		return encodeResult(out, r)
	}))
	s.Delegate = sdk
	for i := 0; i < 3; i++ {
		if e := s.SaveCompleted(context.Background()); e != nil {
			t.Fatal(e)
		}
	}
	if !readJob(t, s).Uncertain || moves != 1 {
		t.Fatal("uncertain operation revived or forgotten", moves)
	}
	state = "committed"
	if e := s.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !readJob(t, s).Published || moves != 1 {
		t.Fatal("receipt convergence replayed", moves)
	}
}
