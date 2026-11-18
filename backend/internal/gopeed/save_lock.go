package gopeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// A per-task cross-process lock orders save admission and pause cancellation,
// without holding the journal lock or blocking another task's admission/list.
func (s *Service) lockSave(ctx context.Context, id string) (func(), error) {
	sum := sha256.Sum256([]byte(id))
	if err := os.MkdirAll(filepath.Join(s.Data, "save-locks"), 0700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(s.Data, "save-locks", hex.EncodeToString(sum[:])), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
