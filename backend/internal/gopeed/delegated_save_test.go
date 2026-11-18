package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type fakeSave struct {
	calls   int
	queries int
	cancels int
	state   string
	reason  string
	code    string
	receipt *delegate.Receipt
	err     error
	input   delegate.MoveParams
	// discards records DiscardStaging calls; discardErr is their answer.
	discards   []delegate.OperationRef
	discardErr error
}

func (f *fakeSave) MoveFromStaging(_ context.Context, p delegate.MoveParams) (delegate.OperationAdmission, error) {
	f.calls++
	if f.calls > 1 && f.input != p {
		panic("changed original move tuple")
	}
	f.input = p
	return delegate.OperationAdmission{JobID: p.JobID, State: "queued"}, f.err
}
func (f *fakeSave) OperationStatus(_ context.Context, auth map[string]string, p delegate.OperationRef) (delegate.OperationResult, error) {
	if auth != nil {
		panic("offline query used credentials")
	}
	f.queries++
	return delegate.OperationResult{JobID: p.JobID, State: f.state, Receipt: f.receipt, Reason: f.reason, Code: f.code}, f.err
}

func TestOfflineAccessRefusalsStopPollingAndRetainStagingAcrossRestart(t *testing.T) {
	for _, code := range []string{"DELEGATION_DENIED"} {
		for _, phase := range []string{"admission", "status-error", "interrupted-result"} {
			t.Run(code+"/"+phase, func(t *testing.T) {
				s, f := stagedDelegate(t)
				ctx := context.Background()
				if phase != "admission" {
					if err := s.SaveCompleted(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if phase == "interrupted-result" {
					f.state, f.code = "interrupted", code
				} else {
					f.err = &api.Error{Code: code}
				}
				_ = s.SaveCompleted(ctx)
				job := readJob(t, s)
				if job.Published || job.Uncertain || job.SaveState != "access_denied" || job.SaveError != code {
					t.Fatalf("not terminal: %#v", job)
				}
				moves, queries := f.calls, f.queries
				// Restoring access cannot revive this grant or turn its operation into a replay.
				f.err, f.code, f.state = nil, "", "committed"
				f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
				restarted := New(s.Data)
				restarted.Engine, restarted.Delegate, restarted.StagingDir = s.Engine, f, s.StagingDir
				restarted.Owner = s.Owner
				for i := 0; i < 3; i++ {
					_ = restarted.SaveCompleted(ctx)
				}
				out, _ := call(t, restarted, "alice", "gopeed.publish", map[string]string{"id": "mine"})
				if out["success"] == true || out["error"].(map[string]any)["code"] != code || f.calls != moves || f.queries != queries {
					t.Fatal("terminal operation retried", out, f.calls, f.queries)
				}
				if got, err := os.ReadFile(filepath.Join(job.Stage, job.Filename)); err != nil || string(got) != "download" {
					t.Fatal("lost staging", err)
				}
				if readJob(t, restarted).DelegationID != "grant1" || readJob(t, restarted).SaveJobID != job.SaveJobID {
					t.Fatal("original reference lost")
				}
			})
		}
	}
}

// Since SDK 0.9.6 APP_DENIED during an operation (DSM Application Privileges
// refuse the owner from the grant's address) keeps the grant like
// PERMISSION_DENIED: only that save fails, and it can be retried.
func TestAppDeniedFailsOnlyThatSave(t *testing.T) {
	for _, phase := range []string{"admission", "status-error", "interrupted-result"} {
		t.Run(phase, func(t *testing.T) {
			s, f := stagedDelegate(t)
			ctx := context.Background()
			if phase != "admission" {
				if err := s.SaveCompleted(ctx); err != nil {
					t.Fatal(err)
				}
			}
			if phase == "interrupted-result" {
				f.state, f.code = "interrupted", "APP_DENIED"
			} else {
				f.err = &api.Error{Code: "APP_DENIED"}
			}
			_ = s.SaveCompleted(ctx)
			job := readJob(t, s)
			if job.Published || job.Uncertain || job.SaveState != "save_failed" || job.SaveError != "APP_DENIED" || job.DelegationID != "grant1" {
				t.Fatalf("%#v", job)
			}
			task := map[string]any{}
			s.present(task, job)
			if task["publication"].(map[string]any)["retryable"] != true {
				t.Fatal("APP_DENIED save not offered for retry", task)
			}
		})
	}
}

func TestUncertainReceiptStillQueriesOriginalOperationUntilCommitted(t *testing.T) {
	s, f := stagedDelegate(t)
	f.state, f.code = "uncertain", "APP_DENIED"
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !readJob(t, s).Uncertain {
		t.Fatal("uncertain must remain nonterminal")
	}
	f.code, f.state, f.receipt = "", "committed", &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !readJob(t, s).Published || f.calls != 2 || f.queries != 2 {
		t.Fatal("lost convergence or replayed admission")
	}
}
func (f *fakeSave) CancelOperation(_ context.Context, _ map[string]string, p delegate.OperationRef) (delegate.OperationResult, error) {
	f.cancels++
	return delegate.OperationResult{JobID: p.JobID, State: "canceled"}, f.err
}
func (f *fakeSave) DiscardStaging(_ context.Context, ref delegate.OperationRef) error {
	f.discards = append(f.discards, ref)
	return f.discardErr
}
func stagedDelegate(t *testing.T) (*Service, *fakeSave) {
	t.Helper()
	s, _, _ := fixture(t)
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	stage := filepath.Join(s.StagingDir, "private")
	if e := os.MkdirAll(stage, 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(stage, "file.bin"), []byte("download"), 0600); e != nil {
		t.Fatal(e)
	}
	f := &fakeSave{state: "queued"}
	s.Delegate = f
	_, e := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["mine"] = Job{Owner: "uid:alice", Stage: stage, Destination: "/home/Downloads", Filename: "file.bin", DelegationID: "grant1"}
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	return s, f
}
func readJob(t *testing.T, s *Service) Job {
	t.Helper()
	var job Job
	_, e := s.withState(context.Background(), func(st *State) (any, error) { job = st.Jobs["mine"]; return nil, nil })
	if e != nil {
		t.Fatal(e)
	}
	return job
}
func TestCompletionWithoutSessionAndAfterDaemonRestart(t *testing.T) {
	s, f := stagedDelegate(t)
	ctx := context.Background()
	if e := s.SaveCompleted(ctx); e != nil {
		t.Fatal(e)
	}
	if f.calls != 1 || f.input.Staging != "private/file.bin" || f.input.JobID != "gopeed-mine" || f.input.Size != 8 {
		t.Fatalf("admission %#v calls %d", f.input, f.calls)
	}
	if readJob(t, s).Published {
		t.Fatal("admission is not a receipt")
	}
	restarted := New(s.Data)
	restarted.Engine = s.Engine
	restarted.StagingDir = s.StagingDir
	restarted.Delegate = f
	f.state = "committed"
	f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
	if e := restarted.SaveCompleted(ctx); e != nil {
		t.Fatal(e)
	}
	if !readJob(t, restarted).Published || f.calls != 2 {
		t.Fatal("restart must invoke the SDK with the original tuple")
	}
}
func TestUnknownInterruptedAndReceiptMismatchNeverReplay(t *testing.T) {
	for _, state := range []string{"uncertain", "interrupted", "mismatch", "transport"} {
		t.Run(state, func(t *testing.T) {
			s, f := stagedDelegate(t)
			ctx := context.Background()
			if e := s.SaveCompleted(ctx); e != nil {
				t.Fatal(e)
			}
			f.state = state
			if state == "mismatch" {
				f.state = "committed"
				f.receipt = &delegate.Receipt{Path: "/home/Other/file.bin", Size: 8}
			}
			if state == "transport" {
				f.err = errors.New("offline")
			}
			_ = s.SaveCompleted(ctx)
			_ = s.SaveCompleted(ctx)
			j := readJob(t, s)
			wantCalls := 3
			if state == "transport" || state == "interrupted" {
				wantCalls = 2
			}
			if j.Published || f.calls != wantCalls {
				t.Fatal("replayed or falsely saved")
			}
			if state != "interrupted" && state != "transport" && !j.Uncertain {
				t.Fatalf("expected uncertainty: %#v", j)
			}
			if state == "interrupted" && j.SaveState != "save_failed" {
				t.Fatal(j)
			}
		})
	}
}
func TestOldJobsWaitForOwnersExplicitSDKGrant(t *testing.T) {
	s, _, _ := fixture(t)
	seed(t, s)
	f := &fakeSave{}
	s.Delegate = f
	if e := s.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	if f.calls != 0 || f.queries != 0 {
		t.Fatal("migration must not invent grants")
	}
}

func TestDelegateReasonSurvivesRestartAndNeverReplaysTerminalCancellation(t *testing.T) {
	s, f := stagedDelegate(t)
	f.state, f.reason = "canceled", "integration-disabled"
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.Data)
	restarted.Engine, restarted.Delegate = s.Engine, f
	job := readJob(t, restarted)
	raw, _ := json.Marshal(job)
	var stored map[string]any
	_ = json.Unmarshal(raw, &stored)
	if stored["saveReason"] != f.reason {
		t.Fatalf("lost durable reason: %s", raw)
	}
	task := map[string]any{}
	restarted.present(task, job)
	if task["publication"].(map[string]any)["reason"] != f.reason {
		t.Fatal("missing API reason", task)
	}
	f.state, f.reason = "copying", ""
	if err := restarted.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	restarted.present(task, readJob(t, restarted))
	if task["publication"].(map[string]any)["reason"] != "integration-disabled" || f.calls != 1 {
		t.Fatal("terminal cancellation lost reason or replayed", task, f.calls)
	}
}
func TestDelegatePublishDoesNotUseSessionFallback(t *testing.T) {
	s, f := stagedDelegate(t)
	f.err = errors.New("unavailable")
	out, _ := call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
	if out["success"] == true {
		t.Fatal(out)
	}
	if _, err := os.Stat(filepath.Join(s.StagingDir, "private/file.bin")); err != nil {
		t.Fatal("failed delegation must retain staging", err)
	}
}
func TestCancelBeforeDeletingTask(t *testing.T) {
	s, f := stagedDelegate(t)
	if e := s.SaveCompleted(context.Background()); e != nil {
		t.Fatal(e)
	}
	f.err = errors.New("cancel unavailable")
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/mine", Query: map[string]json.RawMessage{}})
	if out["success"] != true || f.cancels != 1 || !readJob(t, s).DeleteRequested || readJob(t, s).DelegationID == "" {
		t.Fatal("lost pending operation", out)
	}
}

// A single file refused with destination-exists can be retried once the owner
// removed the conflict; the retry is a new operation (D1). Since SDK 0.9.4 a
// write-access (ACL) refusal or a missing folder, and since SDK 0.9.6 an
// Application Privileges refusal, fails only that save and keeps the grant,
// so it is retried the same way once the owner fixed it.
func TestSingleFileSaveFailureRetriesAsANewOperation(t *testing.T) {
	for _, code := range []string{"destination-exists", "PERMISSION_DENIED", "not-found", "APP_DENIED"} {
		s, f := stagedDelegate(t)
		ctx := context.Background()
		f.err = &api.Error{Code: code}
		_ = s.SaveCompleted(ctx)
		job := readJob(t, s)
		if job.SaveState != "save_failed" || job.SaveError != code {
			t.Fatalf("%s: %#v", code, job)
		}
		task := map[string]any{}
		s.present(task, job)
		if task["publication"].(map[string]any)["retryable"] != true {
			t.Fatal(code, "failed save not offered for retry", task)
		}
		f.err, f.calls = nil, 0
		f.state, f.receipt = "committed", &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
		if out, _ := call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"}); out["success"] != true {
			t.Fatal(code, out)
		}
		if job = readJob(t, s); !job.Published || f.input.JobID != "gopeed-mine.a1" || f.input.Staging != "private/file.bin" {
			t.Fatalf("%s: %#v %#v", code, job, f.input)
		}
		s.present(task, job)
		if task["publication"].(map[string]any)["retryable"] != false {
			t.Fatal(code, "published save offered for retry")
		}
	}
}

// SCOPE_DENIED (the path now names another folder) revokes the grant, so a
// retry could only end in DELEGATION_DENIED; a retry keeps the too-long name.
// Neither is offered.
func TestSingleFileSaveRefusalsARetryCannotFixAreNotRetried(t *testing.T) {
	for _, code := range []string{"SCOPE_DENIED", "name-too-long"} {
		s, f := stagedDelegate(t)
		f.err = &api.Error{Code: code}
		_ = s.SaveCompleted(context.Background())
		job := readJob(t, s)
		task := map[string]any{}
		s.present(task, job)
		if job.SaveState != "save_failed" || task["publication"].(map[string]any)["retryable"] != false {
			t.Fatal(code, task)
		}
		f.err, f.calls = nil, 0
		call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
		if job = readJob(t, s); job.SaveAttempt != 0 || job.SaveError != code {
			t.Fatalf("%s retried: %#v", code, job)
		}
	}
}
