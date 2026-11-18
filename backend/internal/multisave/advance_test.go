package multisave

import (
	"context"
	"errors"
	"path"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type fakeMover struct {
	moves    []delegate.MoveParams
	statuses map[string]delegate.OperationResult
	moveErr  map[string]error
	canceled []string
}

func (f *fakeMover) MoveFromStaging(_ context.Context, p delegate.MoveParams) (delegate.OperationAdmission, error) {
	f.moves = append(f.moves, p)
	if err := f.moveErr[p.Name]; err != nil {
		return delegate.OperationAdmission{}, err
	}
	if _, ok := f.statuses[p.JobID]; !ok {
		f.statuses[p.JobID] = delegate.OperationResult{JobID: p.JobID, State: "committed", Receipt: &delegate.Receipt{Path: path.Join("/vol/dest", p.Name), Size: p.Size}}
	}
	return delegate.OperationAdmission{JobID: p.JobID, State: "queued"}, nil
}
func (f *fakeMover) OperationStatus(_ context.Context, _ map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	out, ok := f.statuses[ref.JobID]
	if !ok {
		return delegate.OperationResult{}, &api.Error{Code: "EXECUTOR_UNAVAILABLE"}
	}
	return out, nil
}
func (f *fakeMover) CancelOperation(_ context.Context, _ map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	f.canceled = append(f.canceled, ref.JobID)
	out := f.statuses[ref.JobID]
	if out.State != "committed" {
		out = delegate.OperationResult{JobID: ref.JobID, State: "canceled"}
		f.statuses[ref.JobID] = out
	}
	return out, nil
}

var batch = Batch{Task: "task1", DelegationID: "grant1", Destination: "/vol/dest", Stage: "stage1"}

func plan(t *testing.T) []Item {
	items, err := Plan("Album", "Album", []Source{{Path: "a.flac", Size: 3}, {Path: "b.flac", Size: 4}})
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func TestAdvancePublishesEveryFileWithDeterministicParams(t *testing.T) {
	m := &fakeMover{statuses: map[string]delegate.OperationResult{}}
	items := Advance(context.Background(), m, batch, plan(t))
	if s := Summarize(items); s.State != "committed" || !s.Published || s.Committed != 2 || s.Total != 2 {
		t.Fatalf("%+v %+v", s, items)
	}
	if len(m.moves) != 2 || m.moves[0].Staging != "stage1/Album/a.flac" || m.moves[0].Name != "Album - a.flac" || m.moves[0].DelegationID != "grant1" || m.moves[0].JobID != OperationID("task1", 0, 0) || m.moves[0].Size != 3 {
		t.Fatalf("%+v", m.moves)
	}
	// A later pass never repeats a committed file.
	Advance(context.Background(), m, batch, items)
	if len(m.moves) != 2 {
		t.Fatalf("repeated moves %+v", m.moves)
	}
}

func TestAdvanceKeepsTransientErrorsPendingAndTerminalErrorsRetryable(t *testing.T) {
	m := &fakeMover{statuses: map[string]delegate.OperationResult{}, moveErr: map[string]error{
		"Album - a.flac": &api.Error{Code: "EXECUTOR_UNAVAILABLE"},
		"Album - b.flac": &api.Error{Code: "EXISTS"},
	}}
	items := Advance(context.Background(), m, batch, plan(t))
	if items[0].State != "pending" || items[0].Code != "EXECUTOR_UNAVAILABLE" || items[1].State != "save_failed" || items[1].Code != "EXISTS" {
		t.Fatalf("%+v", items)
	}
	if s := Summarize(items); s.State != "pending" || s.Published {
		t.Fatalf("%+v", s)
	}
	m.moveErr = nil
	items = Advance(context.Background(), m, batch, items)
	if items[0].State != "committed" || items[1].State != "save_failed" {
		t.Fatalf("terminal item advanced without retry: %+v", items)
	}
	if s := Summarize(items); s.State != "save_failed" || s.Failed != 1 || s.Code != "EXISTS" {
		t.Fatalf("%+v", s)
	}
	items, changed := Retry(items)
	if !changed || items[1].Attempt != 1 || items[1].State != "" || items[0].State != "committed" {
		t.Fatalf("%+v", items)
	}
	moves := len(m.moves)
	items = Advance(context.Background(), m, batch, items)
	if Summarize(items).State != "committed" || len(m.moves) != moves+1 || m.moves[moves].JobID != OperationID("task1", 1, 1) {
		t.Fatalf("%+v %+v", items, m.moves)
	}
}

func TestAdvanceMarksAccessDeniedAndMismatchedReceiptsUncertain(t *testing.T) {
	items := plan(t)
	m := &fakeMover{statuses: map[string]delegate.OperationResult{
		OperationID("task1", 0, 0): {JobID: OperationID("task1", 0, 0), State: "committed", Receipt: &delegate.Receipt{Path: "/vol/dest/other", Size: 3}},
	}, moveErr: map[string]error{"Album - b.flac": &api.Error{Code: "DELEGATION_DENIED"}}}
	items = Advance(context.Background(), m, batch, items)
	if items[0].State != "uncertain" || items[0].Code != "commit_uncertain" || items[1].State != "access_denied" {
		t.Fatalf("%+v", items)
	}
}

// SDK 0.9.6 keeps the grant on APP_DENIED, so the file fails like any other
// refusal Retry can fix; it does not mean the owner lost the destination.
func TestAdvanceFailsAppDeniedFilesWithoutLosingTheDestination(t *testing.T) {
	items := plan(t)
	m := &fakeMover{statuses: map[string]delegate.OperationResult{
		OperationID("task1", 0, 0): {JobID: OperationID("task1", 0, 0), State: "interrupted", Code: "APP_DENIED"},
	}, moveErr: map[string]error{"Album - b.flac": &api.Error{Code: "APP_DENIED"}}}
	items = Advance(context.Background(), m, batch, items)
	if items[0].State != "save_failed" || items[1].State != "save_failed" || items[1].Code != "APP_DENIED" {
		t.Fatalf("%+v", items)
	}
	if s := Summarize(items); s.State != "save_failed" || s.Code != "APP_DENIED" {
		t.Fatalf("%+v", s)
	}
}

func TestAdvanceStopsAtContextDeadlineWithoutLosingProgress(t *testing.T) {
	m := &fakeMover{statuses: map[string]delegate.OperationResult{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	items := Advance(ctx, m, batch, plan(t))
	if len(m.moves) != 0 || items[0].State != "" {
		t.Fatalf("%+v", items)
	}
}

func TestCancelStopsUnfinishedFilesAndReportsKeptFiles(t *testing.T) {
	m := &fakeMover{statuses: map[string]delegate.OperationResult{}, moveErr: map[string]error{"Album - b.flac": &api.Error{Code: "EXECUTOR_UNAVAILABLE"}}}
	items := Advance(context.Background(), m, batch, plan(t))
	m.statuses[OperationID("task1", 1, 0)] = delegate.OperationResult{JobID: OperationID("task1", 1, 0), State: "copying"}
	items[1].State = "copying"
	items, done, kept, err := Cancel(context.Background(), m, batch, items)
	if err != nil || !done || !kept || items[1].State != "canceled" || len(m.canceled) != 1 {
		t.Fatalf("%v %v %v %+v %v", err, done, kept, items, m.canceled)
	}
}

func TestCancelOfUntouchedPlanIsImmediate(t *testing.T) {
	m := &fakeMover{statuses: map[string]delegate.OperationResult{}}
	_, done, kept, err := Cancel(context.Background(), m, batch, plan(t))
	if err != nil || !done || kept || len(m.canceled) != 0 {
		t.Fatal(done, kept, err)
	}
}

func TestCancelWaitsForUncertainFiles(t *testing.T) {
	items := plan(t)
	items[0].State = "uncertain"
	m := &fakeMover{statuses: map[string]delegate.OperationResult{OperationID("task1", 0, 0): {JobID: OperationID("task1", 0, 0), State: "uncertain"}}}
	_, done, _, err := Cancel(context.Background(), m, batch, items)
	if err != nil || done {
		t.Fatal(done, err)
	}
	m2 := &fakeMover{statuses: map[string]delegate.OperationResult{}}
	_, _, _, err = Cancel(context.Background(), errMover{m2}, batch, items)
	if err == nil {
		t.Fatal("hid executor error")
	}
}

type errMover struct{ *fakeMover }

func (errMover) CancelOperation(context.Context, map[string]string, delegate.OperationRef) (delegate.OperationResult, error) {
	return delegate.OperationResult{}, errors.New("down")
}

func TestOperationIDFitsSDKToken(t *testing.T) {
	long := ""
	for range 20 {
		long += "abcdefgh"
	}
	for _, id := range []string{OperationID("t", 0, 0), OperationID(long, 255, 9)} {
		if len(id) > 128 || id == "" {
			t.Fatal(id)
		}
	}
	if OperationID("t", 0, 0) == OperationID("t", 0, 1) || OperationID("t", 1, 0) == OperationID("t", 0, 1) {
		t.Fatal("ids collide")
	}
}

// An operation that ended interrupted by a paused executor is final in the
// SDK; resending its operation ID only replays that result (B1).
func TestPausedInterruptionAdvancesWithAFreshOperation(t *testing.T) {
	first := OperationID("task1", 0, 0)
	m := &fakeMover{statuses: map[string]delegate.OperationResult{first: {JobID: first, State: "interrupted", Code: "EXECUTOR_PAUSED"}}}
	items := Advance(context.Background(), m, batch, plan(t))
	if items[0].State != "" || items[0].Attempt != 1 || items[0].Code != "EXECUTOR_PAUSED" || Summarize(items).State != "pending" {
		t.Fatalf("%+v", items[0])
	}
	// Nothing of the fresh attempt was admitted, so cancelling skips it.
	if _, done, _, err := Cancel(context.Background(), m, batch, items); err != nil || !done || len(m.canceled) != 0 {
		t.Fatalf("cancel %v %v %v", done, err, m.canceled)
	}
	items = Advance(context.Background(), m, batch, items)
	if s := Summarize(items); s.State != "committed" {
		t.Fatalf("%+v %+v", s, items)
	}
	if last := m.moves[len(m.moves)-1]; last.JobID != OperationID("task1", 0, 1) {
		t.Fatalf("%+v", m.moves)
	}
}
