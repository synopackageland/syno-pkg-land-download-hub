package gopeed

import (
	"dsmapp/internal/multisave"
	"errors"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"path/filepath"
	"strings"
)

func saveCode(err error) string {
	var a *api.Error
	if errors.As(err, &a) {
		return a.Code
	}
	return "save_unavailable"
}

// accessDenied reports a revoked grant. APP_DENIED keeps the grant (SDK 0.9.6)
// and fails only that save.
func accessDenied(code string) bool {
	return code == "DELEGATION_DENIED"
}

func markAccessDenied(job *Job) {
	job.SaveState = "access_denied"
	job.Uncertain = false
}

func terminalSave(job Job) bool {
	return job.SaveState == "canceled" || job.SaveState == "interrupted" || job.SaveState == "access_denied" || job.SaveState == "save_failed" || job.SaveState == "download_failed"
}

// retrySave reports a save that stays pending; single and multi-file saves
// retry the same codes.
func retrySave(code string) bool { return multisave.Retryable(code) }

// Only the old transient classification is migrated. Never reconstruct a
// missing tuple, change a reference, or revive a protected refusal/receipt.
func (s *Service) recoverLegacySave(job Job) Job {
	if job.Published || job.SaveState != "save_failed" || !job.SaveStarted || job.Owner == "" || job.Stage == "" || job.Destination == "" || job.DelegationID == "" || job.SaveJobID == "" || job.SaveParams == nil {
		return job
	}
	switch job.SaveError {
	case "jobs_busy", "jobs_unavailable", "canceled":
	default:
		return job
	}
	p := job.SaveParams
	rel, err := filepath.Rel(s.StagingDir, filepath.Join(job.Stage, job.Filename))
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, "../") || !validName(job.Filename) || job.SaveSize < 0 || job.SaveSize > delegate.MaxMoveBytes || p.DelegationID != job.DelegationID || p.JobID != job.SaveJobID || p.Staging != filepath.ToSlash(rel) || p.Name != job.Filename || p.Size != job.SaveSize {
		return job
	}
	job.SaveState = "pending"
	job.Uncertain = false
	return job
}
