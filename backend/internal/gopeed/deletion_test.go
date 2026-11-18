package gopeed

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type deleteSave struct {
	*fakeSave
	cancelState, cancelCode string
	cancelErr, statusErr    error
	refs                    []delegate.OperationRef
}

func (f *deleteSave) CancelOperation(_ context.Context, auth map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	if len(auth) != 0 {
		panic("deletion retained session")
	}
	f.cancels++
	f.refs = append(f.refs, ref)
	return delegate.OperationResult{JobID: ref.JobID, State: f.cancelState, Code: f.cancelCode, Receipt: f.receipt}, f.cancelErr
}
func (f *deleteSave) OperationStatus(ctx context.Context, auth map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	if f.statusErr != nil {
		f.queries++
		return delegate.OperationResult{}, f.statusErr
	}
	return f.fakeSave.OperationStatus(ctx, auth, ref)
}
func deletionFixture(t *testing.T) (*Service, *deleteSave, *int) {
	t.Helper()
	s, f := stagedDelegate(t)
	f.state = "copying"
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	saver := &deleteSave{fakeSave: f, cancelState: "copying", cancelCode: "CANCEL_REQUESTED"}
	s.Delegate = saver
	target, _ := url.Parse(s.Engine)
	proxy := httputil.NewSingleHostReverseProxy(target)
	deletes := new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			if r.URL.Query().Get("force") != "false" {
				t.Error("deletion must retain saved files and private staging")
			}
			*deletes++
			w.Write([]byte(`{"code":0}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s.Engine = server.URL
	return s, saver, deletes
}
func deleteOwned(t *testing.T, s *Service) map[string]any {
	t.Helper()
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/mine"})
	return out
}
func existsJob(t *testing.T, s *Service) bool {
	t.Helper()
	exists := false
	_, err := s.withState(context.Background(), func(st *State) (any, error) { _, exists = st.Jobs["mine"]; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	return exists
}
func assertCanceling(t *testing.T, s *Service) {
	t.Helper()
	job := readJob(t, s)
	task := map[string]any{}
	s.present(task, job)
	if task["publication"].(map[string]any)["state"] != "canceling" || job.SaveState == "save_failed" {
		t.Fatalf("deletion intent not durable/presented: %#v", job)
	}
}
func restartDeletion(s *Service) *Service {
	r := New(s.Data)
	r.Engine, r.Delegate, r.StagingDir, r.Owner = s.Engine, s.Delegate, s.StagingDir, s.Owner
	return r
}
func TestD1AsyncOwnerDeleteConverges(t *testing.T) {
	s, f, deletes := deletionFixture(t)
	job := readJob(t, s)
	moves := f.calls
	if out := deleteOwned(t, s); out["success"] != true {
		t.Fatalf("async intent refused: %#v", out)
	}
	assertCanceling(t, s)
	if *deletes != 0 {
		t.Fatal("removed engine before writer stopped")
	}
	f.state, f.code = "canceled", "OPERATION_CANCELED"
	r := restartDeletion(s)
	if err := r.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if existsJob(t, r) || *deletes != 1 || f.calls != moves {
		t.Fatal("delete failed or admitted again", *deletes, f.calls)
	}
	if f.refs[0] != (delegate.OperationRef{DelegationID: job.DelegationID, JobID: job.SaveJobID}) {
		t.Fatal("reference changed")
	}
	if _, err := os.Stat(filepath.Join(job.Stage, job.Filename)); err != nil {
		t.Fatal("force=false staging lost", err)
	}
	// The SDK, not the engine, removes the staging file the move kept.
	if len(f.discards) != 1 || f.discards[0] != f.refs[0] {
		t.Fatal("staging not discarded through the SDK", f.discards)
	}
}
func TestD1QueuedDeleteAndCommittedRace(t *testing.T) {
	for _, state := range []string{"canceled", "interrupted", "committed"} {
		t.Run(state, func(t *testing.T) {
			s, f, deletes := deletionFixture(t)
			f.cancelState, f.cancelCode = state, ""
			moves := f.calls
			if state == "committed" {
				f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
			}
			out := deleteOwned(t, s)
			if out["success"] != true || existsJob(t, s) || *deletes != 1 || f.calls != moves {
				t.Fatal("terminal deletion failed", out)
			}
			if discarded := len(f.discards) == 1 && f.discards[0] == f.refs[0]; discarded == (state == "committed") {
				t.Fatal("a stopped move's staging is discarded, a committed one's is not", f.discards)
			}
			if state == "committed" {
				notices, _ := call(t, s, "alice", "gopeed.request", Input{Method: "GET", Path: "/api/v1/removals"})
				if notices["success"] != true {
					t.Fatal(notices)
				}
				b, _ := json.Marshal(notices)
				if !containsKept(b) {
					t.Fatal("no durable saved-file notification", string(b))
				}
			}
		})
	}
}
func containsKept(b []byte) bool {
	var x map[string]any
	_ = json.Unmarshal(b, &x)
	data, _ := x["data"].(map[string]any)
	r, _ := data["data"].([]any)
	for _, item := range r {
		if item.(map[string]any)["fileKept"] == true {
			return true
		}
	}
	return false
}

func TestD1RealSDKCancelMirrorSurvivesOfflineRestartWithoutAdmission(t *testing.T) {
	s, f, deletes := deletionFixture(t)
	store := background.NewRequestStore(filepath.Join(t.TempDir(), "journal"))
	unavailable, canceled, moves := true, false, 0
	sdk := &delegate.Service{Store: store, StagingDir: s.StagingDir, Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}}
	sdk.Client = appExecutor(func(_ context.Context, op string, _ map[string]string, _, out any) error {
		if unavailable {
			return &api.Error{Code: "EXECUTOR_UNAVAILABLE"}
		}
		switch op {
		case "files.moveFromStaging":
			moves++
			t.Error("canceled producer re-admitted")
			return nil
		case "files.cancelOperation":
			canceled = true
		case "files.operationStatus":
			if !canceled {
				return &api.Error{Code: "operation-not-found"}
			}
		default:
			t.Fatal(op)
		}
		return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "canceled", Code: "OPERATION_CANCELED"})
	})
	// Leave the SDK mirror reserved without delivering admission.
	_, _ = sdk.MoveFromStaging(context.Background(), f.input)
	s.Delegate = sdk
	if out := deleteOwned(t, s); out["success"] != true {
		t.Fatal(out)
	}
	binding, err := store.MoveBinding(context.Background(), "gopeed-mine")
	if err != nil || !binding.CancelRequested {
		t.Fatal("lost SDK cancel mirror", binding, err)
	}
	unavailable = false
	r := restartDeletion(s)
	if err = sdk.SweepMoves(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err = r.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if existsJob(t, r) || *deletes != 1 || moves != 0 {
		t.Fatal("offline cancellation did not finish safely")
	}
}

func TestD1StaleSaveCannotOverwriteDeletionOrAdmitNewMove(t *testing.T) {
	s, f, _ := deletionFixture(t)
	old := readJob(t, s)
	moves := f.calls
	if out := deleteOwned(t, s); out["success"] != true {
		t.Fatal(out)
	}
	old.SaveState, old.SaveError = "save_failed", "OPERATION_CANCELED"
	if err := s.recordSave(context.Background(), "mine", old); saveCode(err) != "task_deleting" {
		t.Fatal("stale pass overwrote deletion", err)
	}
	if _, err := s.prepareDelegated(context.Background(), "mine", old); saveCode(err) != "task_deleting" {
		t.Fatal("stale producer not fenced", err)
	}
	assertCanceling(t, s)
	if f.calls != moves {
		t.Fatal("readmitted deletion")
	}
	for _, actor := range []string{"normal1", "normal2", "admin"} {
		out, _ := call(t, s, actor, "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/mine"})
		if out["success"] == true {
			t.Fatal("foreign delete admitted", actor)
		}
	}
	if f.cancels != 1 {
		t.Fatal("foreign cancel leaked")
	}
}

type blockedDeleteSave struct {
	*deleteSave
	entered, release chan struct{}
}

func (f *blockedDeleteSave) CancelOperation(ctx context.Context, auth map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	close(f.entered)
	select {
	case <-f.release:
	case <-ctx.Done():
		return delegate.OperationResult{}, ctx.Err()
	}
	return f.deleteSave.CancelOperation(ctx, auth, ref)
}
func TestD1CancelDoesNotHoldEngineOrJobsLocks(t *testing.T) {
	s, f, _ := deletionFixture(t)
	blocked := &blockedDeleteSave{f, make(chan struct{}), make(chan struct{})}
	s.Delegate = blocked
	done := make(chan struct{})
	go func() { defer close(done); deleteOwned(t, s) }()
	<-blocked.entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := s.withState(ctx, func(st *State) (any, error) {
		if !st.Jobs["mine"].DeleteRequested {
			t.Error("intent not persisted before IPC")
		}
		return nil, nil
	})
	if err != nil {
		t.Error("cancel held journal lock", err)
	}
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	close(blocked.release)
	<-done
	if out["success"] != true {
		t.Fatal("cancel blocked lists", out)
	}
}

func TestD1EngineDeleteReplyLossReconcilesOnlyProvenAbsence(t *testing.T) {
	for _, absent := range []bool{false, true} {
		t.Run(fmt.Sprint(absent), func(t *testing.T) {
			s, f, _ := deletionFixture(t)
			f.cancelState, f.cancelCode = "canceled", "OPERATION_CANCELED"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					w.Write([]byte(`{"code":500}`))
					return
				}
				if absent {
					w.Write([]byte(`{"code":0,"data":[]}`))
				} else {
					w.Write([]byte(`{"code":0,"data":[{"id":"mine"}]}`))
				}
			}))
			defer server.Close()
			s.Engine = server.URL
			if out := deleteOwned(t, s); out["success"] != true {
				t.Fatal("intent discarded", out)
			}
			if existsJob(t, s) == absent {
				t.Fatal("inferred deletion without proof")
			}
			if !absent {
				assertCanceling(t, s)
				absent = true
				r := restartDeletion(s)
				if err := r.SaveCompleted(context.Background()); err != nil {
					t.Fatal(err)
				}
				if existsJob(t, r) {
					t.Fatal("engine cleanup not resumed")
				}
			}
		})
	}
}
func TestD1CancelRetryPersistsAcrossRestart(t *testing.T) {
	for _, code := range []string{"EXECUTOR_UNAVAILABLE", "EXECUTOR_TIMEOUT", "EXECUTOR_PROTOCOL", "canceled", "STORAGE_ERROR", "jobs_busy", "jobs_unavailable"} {
		t.Run(code, func(t *testing.T) {
			s, f, deletes := deletionFixture(t)
			moves := f.calls
			f.cancelErr = &api.Error{Code: code}
			if out := deleteOwned(t, s); out["success"] != true {
				t.Fatal("intent lost on temporary error", out)
			}
			assertCanceling(t, s)
			r := restartDeletion(s)
			_ = r.SaveCompleted(context.Background())
			assertCanceling(t, r)
			f.cancelErr = nil
			f.cancelState, f.cancelCode = "canceled", "OPERATION_CANCELED"
			if err := r.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			if existsJob(t, r) || *deletes != 1 || f.calls != moves {
				t.Fatal("retry failed/replayed")
			}
			for _, ref := range f.refs {
				if ref != f.refs[0] {
					t.Fatal("changed ref")
				}
			}
		})
	}
}
func TestD1UncertainOnlyQueriesUntilCommitted(t *testing.T) {
	s, f, deletes := deletionFixture(t)
	f.cancelState, f.cancelCode = "uncertain", "uncertain"
	moves := f.calls
	if out := deleteOwned(t, s); out["success"] != true {
		t.Fatal(out)
	}
	assertCanceling(t, s)
	f.state, f.code = "uncertain", "uncertain"
	f.statusErr = &api.Error{Code: "EXECUTOR_TIMEOUT"}
	r := restartDeletion(s)
	_ = r.SaveCompleted(context.Background())
	assertCanceling(t, r)
	f.statusErr = nil
	_ = r.SaveCompleted(context.Background())
	if f.cancels != 1 || *deletes != 0 || f.calls != moves {
		t.Fatal("uncertain cancel/readmitted")
	}
	f.state, f.code, f.receipt = "committed", "", &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
	if err := r.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if existsJob(t, r) || *deletes != 1 || f.cancels != 1 {
		t.Fatal("committed race did not finish")
	}
}

// DiscardStaging keeps the file while the move may still copy
// (operation-active), while its result is unknown (uncertain) or when the SDK
// cannot answer; the task stays deleting and the next pass asks again.
func TestD1DeletionWaitsUntilTheSDKDiscardsTheStaging(t *testing.T) {
	for _, code := range []string{"operation-active", "uncertain", "EXECUTOR_TIMEOUT"} {
		t.Run(code, func(t *testing.T) {
			s, f, deletes := deletionFixture(t)
			f.cancelState, f.cancelCode = "canceled", ""
			f.discardErr = &api.Error{Code: code}
			if out := deleteOwned(t, s); out["success"] != true {
				t.Fatal(out)
			}
			assertCanceling(t, s)
			if *deletes != 0 || readJob(t, s).DeleteError != code {
				t.Fatal("engine task removed before the staging was discarded", readJob(t, s))
			}
			f.discardErr, f.state, f.code = nil, "canceled", ""
			r := restartDeletion(s)
			if err := r.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			if existsJob(t, r) || *deletes != 1 || f.cancels != 1 || len(f.discards) != 2 {
				t.Fatal("deletion did not finish", *deletes, f.cancels, f.discards)
			}
		})
	}
}

// Another pending job of the App still uses the file (STAGING_IN_USE), or the
// SDK never recorded the move: the task is deleted and the file left alone.
func TestD1DeletionLeavesStagingTheSDKDoesNotOwnForThisTask(t *testing.T) {
	for _, code := range []string{"STAGING_IN_USE", "operation-not-found"} {
		t.Run(code, func(t *testing.T) {
			s, f, deletes := deletionFixture(t)
			f.cancelState, f.cancelCode = "canceled", ""
			f.discardErr = &api.Error{Code: code}
			if out := deleteOwned(t, s); out["success"] != true || existsJob(t, s) || *deletes != 1 {
				t.Fatal("deletion blocked", out)
			}
		})
	}
}

// a download deleted before its save started leaves no staging.
// The engine keeps files on delete (force=false) and a stalled fetcher may
// never let it remove them, so the App removes the task's own stage once the
// engine no longer lists the task.
func TestDeletingAnUnsavedDownloadRemovesItsStaging(t *testing.T) {
	s, _ := stagedDelegate(t)
	stage := readJob(t, s).Stage
	target, _ := url.Parse(s.Engine)
	proxy := httputil.NewSingleHostReverseProxy(target)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			if r.URL.Query().Get("force") != "false" {
				t.Error("deletion must not ask the engine to remove files")
			}
			w.Write([]byte(`{"code":0}`))
			return
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	s.Engine = server.URL
	if out := deleteOwned(t, s); out["success"] != true || existsJob(t, s) {
		t.Fatal("deletion did not finish", out)
	}
	if _, err := os.Lstat(stage); !os.IsNotExist(err) {
		t.Fatal("staging kept", err)
	}
}

// A file whose save started belongs to the SDK's move; deletion leaves it to
// the SDK's discard.
func TestDeletingAStartedSaveLeavesItsStagingToTheSDK(t *testing.T) {
	s, f, _ := deletionFixture(t)
	f.cancelState, f.cancelCode = "canceled", ""
	f.discardErr = &api.Error{Code: "STAGING_IN_USE"}
	stage := readJob(t, s).Stage
	if out := deleteOwned(t, s); out["success"] != true || existsJob(t, s) {
		t.Fatal("deletion blocked", out)
	}
	if _, err := os.Lstat(filepath.Join(stage, "file.bin")); err != nil {
		t.Fatal("removed a file the SDK still uses", err)
	}
}

// The engine can acknowledge a DELETE and keep the task: a download cleared
// while it starts is stored again, paused, once its resource resolves. Nobody
// owns it then, so no list shows it, yet it holds every core update
// (downloads_active). The background pass deletes again each task the App
// already removed while the engine still lists it, and nothing else.
func TestATaskTheEngineKeptAfterItsRemovalIsDeletedAgain(t *testing.T) {
	s, _ := stagedDelegate(t)
	target, _ := url.Parse(s.Engine)
	proxy := httputil.NewSingleHostReverseProxy(target)
	gone := false
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "DELETE":
			if r.URL.Query().Get("force") != "false" {
				t.Error("deletion must not ask the engine to remove files")
			}
			deleted = append(deleted, strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/"))
			w.Write([]byte(`{"code":0}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/tasks" && gone:
			w.Write([]byte(`{"code":0,"data":[{"id":"other","status":"pause"}]}`))
		case r.Method == "GET" && r.URL.Path == "/api/v1/tasks":
			w.Write([]byte(`{"code":0,"data":[{"id":"mine","status":"pause","meta":{"opts":{"path":"private"}}},{"id":"other","status":"pause"}]}`))
		default:
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	s.Engine = server.URL
	if out := deleteOwned(t, s); out["success"] != true || existsJob(t, s) {
		t.Fatal("deletion did not finish", out)
	}
	if fmt.Sprint(deleted) != "[mine]" {
		t.Fatal("first delete", deleted)
	}
	// The engine still lists it: the next pass deletes it again, and only it.
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(deleted) != "[mine mine]" {
		t.Fatal("kept task not deleted again, or another task touched", deleted)
	}
	// Once the engine no longer lists it, nothing more is deleted.
	gone = true
	if err := restartDeletion(s).SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(deleted) != "[mine mine]" {
		t.Fatal("deleted after the engine dropped it", deleted)
	}
}
