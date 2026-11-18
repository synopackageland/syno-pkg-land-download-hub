package gopeed

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

// relocateLegacyStage moves only a completed package-private legacy directory.
// A deterministic target recovers a crash between rename and metadata commit.
// The engine lock serializes this with all product writes and updates.
func (s *Service) relocateLegacyStage(id string, job *Job) error {
	if filepath.Dir(job.Stage) == s.StagingDir {
		return nil
	}
	oldRoot := filepath.Join(s.Data, "staging")
	if filepath.Dir(job.Stage) != oldRoot || !validName(filepath.Base(job.Stage)) || !validName(job.Filename) {
		return refused("unsafe_file")
	}
	if err := os.MkdirAll(s.StagingDir, 0700); err != nil {
		return err
	}
	for _, dir := range []string{oldRoot, s.StagingDir} {
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return refused("unsafe_file")
		}
	}
	target := filepath.Join(s.StagingDir, fmt.Sprintf("legacy-%x", sha256.Sum256([]byte(id))))
	source, srcErr := os.Lstat(job.Stage)
	dest, dstErr := os.Lstat(target)
	if srcErr == nil {
		if !source.IsDir() || source.Mode()&os.ModeSymlink != 0 || !os.IsNotExist(dstErr) {
			return refused("unsafe_file")
		}
		root, err := os.OpenRoot(job.Stage)
		if err != nil {
			return err
		}
		info, err := root.Lstat(job.Filename)
		root.Close()
		if err != nil || !info.Mode().IsRegular() {
			return refused("unsafe_file")
		}
		if err = os.Rename(job.Stage, target); err != nil {
			return err
		}
	} else if !os.IsNotExist(srcErr) || dstErr != nil || !dest.IsDir() || dest.Mode()&os.ModeSymlink != 0 {
		return refused("unsafe_file")
	}
	for _, dir := range []string{oldRoot, s.StagingDir} {
		f, err := os.Open(dir)
		if err != nil {
			return err
		}
		err = f.Sync()
		f.Close()
		if err != nil {
			return err
		}
	}
	job.Stage = target
	return nil
}
