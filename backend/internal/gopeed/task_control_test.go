package gopeed

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type pauseSave struct {
	*fakeSave
	params      []delegate.MoveParams
	refs        []delegate.OperationRef
	cancelState string
}

func (f *pauseSave) MoveFromStaging(_ context.Context, p delegate.MoveParams) (delegate.OperationAdmission, error) {
	f.params = append(f.params, p)
	return delegate.OperationAdmission{JobID: p.JobID, State: "queued"}, nil
}
func (f *pauseSave) CancelOperation(_ context.Context, auth map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	if auth != nil {
		panic("retained session")
	}
	f.refs = append(f.refs, ref)
	return delegate.OperationResult{JobID: ref.JobID, State: f.cancelState}, nil
}
func TestSavePauseResumeWaitsForTerminalAndRetainsStaging(t *testing.T) {
	s, base := stagedDelegate(t)
	f := &pauseSave{fakeSave: base, cancelState: "copying"}
	s.Delegate = f
	ctx := context.Background()
	if err := s.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	original := readJob(t, s)
	if len(f.params) != 1 {
		t.Fatal(f.params)
	}
	if _, err := s.controlTask(ctx, "uid:alice", "mine", "pause"); err != nil {
		t.Fatal(err)
	}
	if j := readJob(t, s); !j.ManualPaused || !j.SavePausePending {
		t.Fatal(j)
	}
	if _, err := s.controlTask(ctx, "uid:alice", "mine", "continue"); err != nil {
		t.Fatal(err)
	}
	restarted := New(s.Data)
	restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, f
	if err := restarted.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.params) != 1 {
		t.Fatal("resubmitted a copying operation", f.params)
	}
	for _, ref := range f.refs {
		if ref.JobID != original.SaveJobID || ref.DelegationID != original.DelegationID {
			t.Fatal(ref)
		}
	}
	f.cancelState = "canceled"
	if err := restarted.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if len(f.params) != 2 || f.params[1].JobID == original.SaveJobID || f.params[1].DelegationID != original.DelegationID {
		t.Fatal(f.params)
	}
	if b, err := os.ReadFile(filepath.Join(original.Stage, original.Filename)); err != nil || string(b) != "download" {
		t.Fatal("lost staging", err)
	}
	if len(f.discards) != 0 {
		t.Fatal("pause discarded staging")
	}
}
func TestPausedBeforeSaveNeverAdmits(t *testing.T) {
	s, f := stagedDelegate(t)
	ctx := context.Background()
	if _, err := s.controlTask(ctx, "uid:alice", "mine", "pause"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatal("paused save admitted")
	}
	if _, err := s.controlTask(ctx, "uid:alice", "mine", "continue"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCompleted(ctx); err != nil || f.calls != 1 {
		t.Fatal(err, f.calls)
	}
}
func TestPreparationClearNeverStartsSource(t *testing.T) {
	s, f := newEngineFixture(t)
	ctx := context.Background()
	results := createMany(t, s, "alice", item(1, "https://example.com/a"))
	id := admissionID(results[0])
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/" + id})
	if out["success"] != true {
		t.Fatal(out)
	}
	r := &PreparationRunner{Service: s}
	defer r.Close()
	if err := r.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	if len(state(t, s).Jobs) != 0 || len(f.resolves) != 0 || len(f.creates) != 0 {
		t.Fatal("cleared request ran")
	}
}

// A preparation whose accepted child was deleted must not stay runnable.
func TestPreparationWithMissingChildFails(t *testing.T) {
	s, _ := newEngineFixture(t)
	ctx := context.Background()
	results := createMany(t, s, "alice", item(1, "https://example.com/a"))
	id := admissionID(results[0])
	s.withState(ctx, func(st *State) (any, error) {
		j := st.Jobs[id]
		p := *j.Preparation
		p.Children = []string{"task-gone"}
		j.Preparation = &p
		st.Jobs[id] = j
		return nil, nil
	})
	r := &PreparationRunner{Service: s}
	defer r.Close()
	r.prepare(ctx, id, state(t, s).Jobs[id])
	j := state(t, s).Jobs[id]
	if j.Preparation == nil || j.Preparation.Status != "error" || j.Preparation.Error != "task_denied" {
		t.Fatal(j.Preparation)
	}
}

func TestBTSavePauseKeepsCommittedFilesAndRetriesOnlyCanceledFile(t *testing.T) {
	s, f, m := createdBT(t)
	ctx := context.Background()
	f.finish(false)
	// One file commits while its sibling is still copying.
	m.state["Album - cover.jpg"] = "copying"
	if err := s.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	before := state(t, s).Jobs["bt-task"]
	if _, err := s.controlTask(ctx, "uid:admin", "bt-task", "pause"); err != nil {
		t.Fatal(err)
	}
	paused := state(t, s).Jobs["bt-task"]
	if !paused.ManualPaused || len(m.cancels) != 1 || paused.Files[0].State != "committed" || len(m.discards) != 0 {
		t.Fatal(paused, m.cancels)
	}
	if err := s.SaveCompleted(ctx); err != nil || len(m.moves) != 2 {
		t.Fatal("paused task progressed", err, m.moves)
	}
	if _, err := s.controlTask(ctx, "uid:admin", "bt-task", "continue"); err != nil {
		t.Fatal(err)
	}
	delete(m.state, "Album - cover.jpg")
	if err := s.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	after := state(t, s).Jobs["bt-task"]
	if !after.Published || len(m.moves) != 3 || after.Files[0].Attempt != before.Files[0].Attempt || after.Files[1].Attempt != before.Files[1].Attempt+1 {
		t.Fatal(after, m.moves)
	}
}
