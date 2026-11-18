package gopeed

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// staleStage is the age after which an unused empty stage is an orphan. A
// younger one may belong to a magnet resolve still waiting for the engine.
const staleStage = time.Hour

// stageInUse reports a stage folder a job or preview still refers to.
func stageInUse(st *State, stage string) bool {
	for _, j := range st.Jobs {
		if j.Stage == stage {
			return true
		}
	}
	for _, j := range st.Resolved {
		if j.Stage == stage {
			return true
		}
	}
	return false
}

// removeEmptyStage removes a stage folder nothing refers to any more. A
// folder that still holds files is kept for diagnostics.
func (s *Service) removeEmptyStage(st *State, stage string) {
	if stage == "" || filepath.Dir(stage) != s.StagingDir || stageInUse(st, stage) {
		return
	}
	_ = os.Remove(stage)
}

// downloadStage is the name of a download's or a preview's own stage
// (randomID); BT, legacy and SDK entries are named otherwise.
var downloadStage = regexp.MustCompile(`^[0-9a-f]{32}$`)

// SweepStaging removes old empty stage folders no job or preview refers to,
// left by resolves that never became tasks and by deleted tasks. An old
// download stage nothing refers to goes with its files: a download removed
// before deletions dropped their stage left it.
func (s *Service) SweepStaging(ctx context.Context, now time.Time) error {
	if err := s.ReapDeletedMediaUsers(ctx); err != nil {
		return err
	}
	_, err := s.withState(ctx, func(st *State) (any, error) {
		entries, err := os.ReadDir(s.StagingDir)
		if err != nil {
			return nil, nil
		}
		for _, e := range entries {
			info, err := e.Info()
			if err != nil || !e.IsDir() || now.Sub(info.ModTime()) < staleStage {
				continue
			}
			stage := filepath.Join(s.StagingDir, e.Name())
			if downloadStage.MatchString(e.Name()) && !stageInUse(st, stage) {
				_ = os.RemoveAll(stage)
				continue
			}
			s.removeEmptyStage(st, stage)
		}
		return nil, nil
	})
	return err
}

// dropUnsavedStage removes the private stage of a deleted download whose save
// never started: nothing was published from it and the SDK never took its
// file. The engine keeps files on delete, and a stalled fetcher may never
// let it remove them. A started save's file is the SDK's to discard; BT and
// media stages have their own cleanup.
func (s *Service) dropUnsavedStage(st *State, job Job) {
	if job.SaveStarted || job.DeleteKept || job.Hash != "" || job.Media != nil || job.Stage == "" || filepath.Dir(job.Stage) != s.StagingDir || stageInUse(st, job.Stage) {
		return
	}
	_ = os.RemoveAll(job.Stage)
}
