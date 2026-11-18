package gopeed

import (
	"context"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"testing"
)

func TestDelegateCancelRequestedOnlyQueriesOriginalRefAcrossRestart(t *testing.T) {
	s, f := stagedDelegate(t)
	ctx := context.Background()
	f.state, f.code, f.reason = "copying", "CANCEL_REQUESTED", "scope-changed"
	if err := s.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	before := readJob(t, s)
	moves := f.calls
	restarted := New(s.Data)
	restarted.Engine, restarted.Delegate, restarted.StagingDir = s.Engine, f, s.StagingDir
	f.err = &api.Error{Code: "EXECUTOR_UNAVAILABLE"}
	_ = restarted.SaveCompleted(ctx)
	if j := readJob(t, restarted); j.SaveState != "copying" || j.SaveError != "CANCEL_REQUESTED" {
		t.Fatalf("lost cancellation intent on transient status error: %#v", j)
	}
	f.err = nil
	for i := 0; i < 3; i++ {
		if err := restarted.SaveCompleted(ctx); err != nil {
			t.Fatal(err)
		}
	}
	f.state, f.code = "uncertain", ""
	if err := restarted.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	if err := restarted.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	f.state, f.code = "canceled", "OPERATION_CANCELED"
	if err := restarted.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	j := readJob(t, restarted)
	if j.SaveState != "canceled" || j.SaveReason != "scope-changed" || j.Published || j.SaveParams == nil || *j.SaveParams != *before.SaveParams {
		t.Fatalf("wrong cancellation result: %#v", j)
	}
	queries := f.queries
	for i := 0; i < 3; i++ {
		_ = restarted.SaveCompleted(ctx)
	}
	if f.calls != moves || f.queries != queries {
		t.Fatalf("cancellation replayed: moves %d->%d, queries %d->%d", moves, f.calls, queries, f.queries)
	}
}
func TestDelegateSettingsCancellationIsTerminalWithoutFailure(t *testing.T) {
	for _, reason := range []string{"scope-changed", "grant-revoked", "integration-disabled"} {
		t.Run(reason, func(t *testing.T) {
			s, f := stagedDelegate(t)
			f.state, f.code, f.reason = "canceled", "OPERATION_CANCELED", reason
			if err := s.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			j := readJob(t, s)
			if j.SaveState != "canceled" || !terminalSave(j) || j.SaveReason != reason {
				t.Fatalf("settings cancellation misclassified: %#v", j)
			}
			moves := f.calls
			_ = s.SaveCompleted(context.Background())
			if f.calls != moves {
				t.Fatal("revoked grant replayed")
			}
		})
	}
}
func TestDelegatePausedResultIsTerminalButPausedRPCIsPending(t *testing.T) {
	s, f := stagedDelegate(t)
	f.err = &api.Error{Code: "EXECUTOR_PAUSED"}
	_ = s.SaveCompleted(context.Background())
	if readJob(t, s).SaveState != "pending" {
		t.Fatal("RPC paused error must be recoverable with original tuple")
	}
	params := *readJob(t, s).SaveParams
	f.err = nil
	f.state, f.code = "interrupted", "EXECUTOR_PAUSED"
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	j := readJob(t, s)
	if j.SaveState != "interrupted" || !terminalSave(j) || j.Published || *j.SaveParams != params {
		t.Fatalf("paused result: %#v", j)
	}
	moves, queries := f.calls, f.queries
	f.state, f.code = "committed", ""
	f.receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
	_ = s.SaveCompleted(context.Background())
	if f.calls != moves || f.queries != queries || readJob(t, s).Published {
		t.Fatal("terminal paused copy replayed")
	}
}
