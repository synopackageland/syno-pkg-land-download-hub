package gopeed

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMediaRunnerSharesSlotsAcrossOwners(t *testing.T) {
	s, r, _ := runnerFixture(t)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		var sample Job
		for _, sample = range st.Jobs {
			break
		}
		st.Jobs = map[string]Job{}
		for _, id := range []string{"a1", "a2", "a3", "b1", "b2"} {
			j := sample
			m := *sample.Media
			j.Media, j.Owner, j.Stage = &m, string(id[0]), filepath.Join(s.StagingDir, id)
			st.Jobs[id] = j
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := state(t, s)
	if st.Jobs["a1"].Media.Status != "running" || st.Jobs["b1"].Media.Status != "running" {
		t.Fatal("one owner monopolized the available media slots")
	}
}

func TestMediaRunnerDeletedInterruptedJobDoesNotTakeAnOwnerTurn(t *testing.T) {
	s, r, _ := runnerFixture(t)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		var sample Job
		for _, sample = range st.Jobs {
			break
		}
		st.Jobs = map[string]Job{}
		for _, id := range []string{"a0", "a1", "b1", "c1"} {
			j := sample
			m := *sample.Media
			j.Media, j.Owner, j.Stage = &m, string(id[0]), filepath.Join(s.StagingDir, id)
			if id == "a0" {
				j.DeleteRequested, m.Status = true, "running"
			}
			st.Jobs[id] = j
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := state(t, s)
	if st.Jobs["a0"].Media.Status != "canceled" || st.Jobs["a1"].Media.Status != "running" || st.Jobs["b1"].Media.Status != "running" {
		t.Fatal("deleted work consumed its owner's turn")
	}
}

func TestOwnerQueueRotatesAndAccountsForActiveRuns(t *testing.T) {
	jobs := map[string]Job{"a1": {Owner: "alice"}, "a2": {Owner: "alice"}, "b1": {Owner: "bob"}, "c1": {Owner: "carol"}}
	ids := fairJobIDs(jobs, map[string]int{"alice": 1}, "bob")
	if len(ids) != 4 || ids[0] != "c1" || ids[1] != "b1" {
		t.Fatalf("active owner or cursor ignored: %v", ids)
	}
	ids = fairJobIDs(jobs, nil, "alice")
	if ids[0] != "b1" || ids[1] != "c1" || ids[2] != "a1" || ids[3] != "a2" {
		t.Fatalf("owners did not rotate: %v", ids)
	}
}

func TestPreparationRunnerGivesTheNextOwnerATurn(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) { return 1, nil }
	a := createMany(t, s, "alice", item(1, "https://example.com/a1"), item(2, "https://example.com/a2"))
	b := createMany(t, s, "bob", item(3, "https://example.com/b1"))
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs = map[string]Job{"a1": st.Jobs[admissionID(a[0])], "a2": st.Jobs[admissionID(a[1])], "b1": st.Jobs[admissionID(b[0])]}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &PreparationRunner{Service: s}
	defer r.Close()
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-r.done
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-r.done
	if len(f.resolves) != 2 || f.resolves[1]["req"].(map[string]any)["url"] != "https://example.com/b1" {
		t.Fatalf("preparation monopolized: %v", f.resolves)
	}
}
