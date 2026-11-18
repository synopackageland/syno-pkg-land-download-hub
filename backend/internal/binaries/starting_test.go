package binaries

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// between the package start and the engine's first answer the
// supervisor records that the engine is starting, so the App says the
// package is starting instead of that the engine does not respond.
func TestSupervisorRecordsStartingUntilTheEngineAnswers(t *testing.T) {
	t.Setenv("FAKE_GOPEED_DELAY_MS", "500")
	f := superviseFake(t, 0, 0, nil)
	f.waitRuntime(t, func(st Runtime) bool { return st.Starting && !st.Ready })
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready && !st.Starting })
	f.stop()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
}

// An engine that never answers is not starting any more: the daemon ends
// and leaves runtime.json not ready and not starting.
func TestSupervisorWhoseEngineNeverAnswersStopsStarting(t *testing.T) {
	before := startTimeout
	startTimeout = 300 * time.Millisecond
	t.Cleanup(func() { startTimeout = before })
	t.Setenv("FAKE_GOPEED_DELAY_MS", "5000")
	f := superviseFake(t, 0, 0, nil)
	if err := <-f.done; err == nil {
		t.Fatal("an engine that never answered was accepted")
	}
	if st, err := ReadRuntime(f.data); err != nil || st.Ready || st.Starting {
		t.Fatalf("runtime after a failed start: %+v %v", st, err)
	}
}

// An engine executable that cannot run leaves nothing starting either.
func TestSupervisorWhoseEngineCannotRunStopsStarting(t *testing.T) {
	data := t.TempDir()
	missing := Selection{Path: filepath.Join(data, "missing"), Version: "1.9.3"}
	if err := Supervise(context.Background(), data, missing, filepath.Join(data, "engine.json"), "http://127.0.0.1:1"); err == nil {
		t.Fatal("a missing engine executable was accepted")
	}
	if st, err := ReadRuntime(data); err != nil || st.Ready || st.Starting {
		t.Fatalf("runtime after a failed start: %+v %v", st, err)
	}
}

// the launcher marks starting before its own checks; a first
// install has no record yet and starts from an empty one.
func TestMarkStartingKeepsTheRecordAndStartsAnEmptyOne(t *testing.T) {
	data := t.TempDir()
	if err := MarkStarting(data); err != nil {
		t.Fatal(err)
	}
	if st, err := ReadRuntime(data); err != nil || !st.StartingAt(time.Now()) || st.Ready {
		t.Fatalf("first starting record: %+v %v", st, err)
	}
	kept := Runtime{Active: Selection{Path: "/p/gopeed", Version: "1.9.3"}, Request: "r1", Ready: true}
	Atomic(filepath.Join(data, "runtime.json"), kept)
	if err := MarkStarting(data); err != nil {
		t.Fatal(err)
	}
	st, err := ReadRuntime(data)
	if err != nil || !st.StartingAt(time.Now()) || st.Ready || st.Active != kept.Active || st.Request != "r1" {
		t.Fatalf("starting record: %+v %v", st, err)
	}
	EndStarting(data)
	if st, err := ReadRuntime(data); err != nil || st.Starting || st.StartingSince != 0 || st.Active != kept.Active {
		t.Fatalf("ended starting record: %+v %v", st, err)
	}
}

// A daemon killed while starting (kill -9) leaves starting:true behind; it
// counts only within startingLimit, and a record without a time (0.2.2-0032)
// not at all.
func TestAStaleStartingRecordIsNotStarting(t *testing.T) {
	now := time.Now()
	fresh := Runtime{Starting: true, StartingSince: now.Add(-10 * time.Second).UnixMilli()}
	stale := Runtime{Starting: true, StartingSince: now.Add(-startingLimit - time.Second).UnixMilli()}
	if !fresh.StartingAt(now) || stale.StartingAt(now) || (Runtime{Starting: true}).StartingAt(now) {
		t.Fatal(fresh.StartingAt(now), stale.StartingAt(now))
	}
}

// The record the launcher wrote on a first install names no engine; the
// supervisor runs the packaged one instead of refusing an empty selection.
func TestSupervisorStartsFromTheLaunchersFirstRecord(t *testing.T) {
	t.Setenv("FAKE_GOPEED_DELAY_MS", "0")
	f := superviseFakeRecord(t, 0, 0, &Runtime{Starting: true, StartingSince: time.Now().UnixMilli()}, healthPolicy, false)
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready && !st.Starting && st.StartingSince == 0 })
	f.stop()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
}
