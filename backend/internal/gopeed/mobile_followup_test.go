package gopeed

import (
	"context"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"path/filepath"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

func TestSingleURLSuggestedFilenameIsSanitizedBeforeCreate(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, map[string]any{"id": "rid-1", "res": map[string]any{"files": []any{map[string]any{"name": "手機/驗收?.csv", "path": "", "size": 8}}}}
	}
	out := btResolve(t, s, "admin", "https://example.com/report")
	if out["success"] != true {
		t.Fatal(out)
	}
	if got := state(t, s).Resolved["rid-1"].Filename; got != "手機_驗收_.csv" {
		t.Fatal(got)
	}
	if out = btCreate(t, s, "admin", "rid-1"); out["success"] != true {
		t.Fatal(out)
	}
	opts, _ := f.resolves[len(f.resolves)-1]["opts"].(map[string]any)
	if opts["name"] != "手機_驗收_.csv" {
		t.Fatal("engine must receive the safe leaf", f.resolves)
	}
}

func TestSubfolderCommittedReceiptRecoversExistingUncertainFiles(t *testing.T) {
	s, f, m := createdBT(t)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs["bt-task"]
		j.Subfolder = "Downloads/Books"
		st.Jobs["bt-task"] = j
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.finish(false)
	if err = s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	job := state(t, s).Jobs["bt-task"]
	if !job.Published {
		t.Fatalf("valid protected receipts rejected: %+v", job)
	}
	original := append([]delegate.MoveParams(nil), m.moves...)
	// Simulate the persisted state from the old receipt comparison.
	_, err = s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs["bt-task"]
		j.Published = false
		j.Uncertain = true
		j.SaveState = "uncertain"
		for i := range j.Files {
			j.Files[i].State = "uncertain"
			j.Files[i].Code = "commit_uncertain"
		}
		st.Jobs["bt-task"] = j
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := New(s.Data)
	r.Engine, r.StagingDir, r.Delegate = s.Engine, s.StagingDir, m
	if err = r.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !state(t, r).Jobs["bt-task"].Published {
		t.Fatal("existing uncertain task did not recover")
	}
	if len(m.moves) != 2*len(original) {
		t.Fatal("unexpected calls", m.moves)
	}
	for i, p := range m.moves[len(original):] {
		if p != original[i] {
			t.Fatal("changed operation during recovery")
		}
	}
}

func TestSingleFileRenameKeepsOriginalTupleAndRevealsActualName(t *testing.T) {
	s, f := stagedDelegate(t)
	f.state = "committed"
	f.receipt = &delegate.Receipt{Path: "/home/Downloads/file (1).bin", Size: 8}
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	job := readJob(t, s)
	if !job.Published || f.input.Conflict != "rename" || revealName(job) != "file (1).bin" {
		t.Fatal(job, f.input, revealName(job))
	}
	calls := f.calls
	if err := s.SaveCompleted(context.Background()); err != nil || f.calls != calls {
		t.Fatal("replayed committed rename", err)
	}
}

// The real SDK owns journal pinning, admission deduplication, lost replies and
// cleanup. The executor is a local fixture, not evidence of a NAS writer.
func TestCollisionRetryThroughRealSDKRecoversLostReplyOnce(t *testing.T) {
	s, _ := stagedDelegate(t)
	sends := map[string]int{}
	sdk := recoverySDK(s, filepath.Join(t.TempDir(), "mirror"), appExecutor(func(_ context.Context, op string, _ map[string]string, p, out any) error {
		var id string
		switch op {
		case "files.moveFromStaging":
			params := p.(delegate.MoveParams)
			id = params.JobID
			sends[id]++
			if id == "gopeed-mine.a1" {
				if params.Conflict != "rename" {
					t.Fatal(params)
				}
				return &api.Error{Code: "EXECUTOR_TIMEOUT"}
			}
		case "files.operationStatus":
			id = p.(delegate.OperationRef).JobID
		default:
			t.Fatal(op)
		}
		result := delegate.OperationResult{JobID: id, State: "interrupted", Code: "destination-exists"}
		if id == "gopeed-mine.a1" {
			result.State, result.Code = "committed", ""
			result.Receipt = &delegate.Receipt{Path: "/home/Downloads/file (1).bin", Size: 8}
		}
		return encodeResult(out, result)
	}))
	s.Delegate = sdk
	// A pre-upgrade request used the default fail policy. Preserve its tuple.
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs["mine"]
		j.SaveStarted = true
		j.SaveJobID = "gopeed-mine"
		j.SaveSize = 8
		j.SaveState = "pending"
		j.SaveParams = &delegate.MoveParams{DelegationID: j.DelegationID, JobID: j.SaveJobID, Staging: "private/file.bin", Name: "file.bin", Size: 8}
		st.Jobs["mine"] = j
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if j := readJob(t, s); j.SaveState != "save_failed" || j.SaveError != "destination-exists" {
		t.Fatal(j)
	}
	for range 3 {
		out, _ := call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
		if out["success"] != true {
			t.Fatal(out)
		}
	}
	j := readJob(t, s)
	if !j.Published || j.Uncertain || j.SaveAttempt != 1 || j.SavedName != "file (1).bin" || sends["gopeed-mine"] != 1 || sends["gopeed-mine.a1"] != 1 {
		t.Fatal(j, sends)
	}
}

func TestSanitizedSingleURLDoesNotAdmitChangedMultiFileResult(t *testing.T) {
	s, f := newEngineFixture(t)
	calls := 0
	f.resolve = func(string) (int, any) {
		calls++
		files := []any{map[string]any{"name": "a/b.csv", "path": "", "size": 8}}
		if calls == 2 {
			files = append(files, map[string]any{"name": "second.csv", "path": "", "size": 8})
		}
		return 0, map[string]any{"id": "rid-1", "res": map[string]any{"files": files}}
	}
	out := btResolve(t, s, "admin", "https://example.com/report")
	if errorCode(out) != "single_file_required" || len(state(t, s).Resolved) != 0 || len(f.creates) != 0 {
		t.Fatal(out)
	}
}
