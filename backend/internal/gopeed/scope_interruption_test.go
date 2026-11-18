package gopeed

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestScopeInterruptedCopyIsTerminalAndNeverReusesRevokedGrant(t *testing.T) {
	for _, code := range []string{"WRITE_FAILED", ""} {
		t.Run(code, func(t *testing.T) {
			s, f := stagedDelegate(t)
			ctx := context.Background()
			if err := s.SaveCompleted(ctx); err != nil {
				t.Fatal(err)
			}
			f.state, f.code = "interrupted", code
			_ = s.SaveCompleted(ctx)
			job := readJob(t, s)
			if job.SaveState != "save_failed" || job.Published || job.Uncertain {
				t.Fatalf("interrupted not terminal failure: %#v", job)
			}
			if _, err := os.Stat(filepath.Join(job.Stage, job.Filename)); err != nil {
				t.Fatal("staging lost", err)
			}
			moves, queries := f.calls, f.queries
			restarted := New(s.Data)
			restarted.Engine, restarted.Delegate, restarted.StagingDir, restarted.Owner = s.Engine, f, s.StagingDir, s.Owner
			f.state, f.code = "copying", ""
			for i := 0; i < 3; i++ {
				_ = restarted.SaveCompleted(ctx)
			}
			if f.calls != moves || f.queries != queries {
				t.Fatal("revoked tuple replayed", f.calls, f.queries)
			}
			after := readJob(t, restarted)
			if after.SaveJobID != job.SaveJobID || after.DelegationID != job.DelegationID {
				t.Fatal("reference changed")
			}
		})
	}
}
