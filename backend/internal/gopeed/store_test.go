package gopeed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReadOnlyPollingPreservesDurableStateFile(t *testing.T) {
	s := New(t.TempDir())
	seed(t, s)
	p := filepath.Join(s.Data, "jobs.json")
	before, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	for range 3 {
		if _, e = s.withState(context.Background(), func(st *State) (any, error) { return len(st.Jobs), nil }); e != nil {
			t.Fatal(e)
		}
	}
	after, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("read-only polling replaced durable state")
	}
}
