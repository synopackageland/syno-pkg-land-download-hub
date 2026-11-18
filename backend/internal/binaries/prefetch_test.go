package binaries

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Gopeed 1.9.3 prefetches a resolved HTTP resource into
// $TMPDIR/gopeed-prefetch-* and removes it only when that resolve starts a
// download. A resolve the dialog dropped, or an engine stopped meanwhile,
// left the file for good (270 MB on test-nas). The supervisor removes them
// whenever its engine is not running: before every start and after the
// last stop.
func TestSupervisorRemovesLeftPrefetchFilesAroundTheEngine(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	t.Setenv("TMPDIR", tmp)
	t.Setenv("FAKE_GOPEED_DELAY_MS", "0")
	left := filepath.Join(tmp, "gopeed-prefetch-123")
	other := filepath.Join(tmp, "other-file")
	dir := filepath.Join(tmp, "gopeed-prefetch-dir")
	os.WriteFile(left, []byte("left"), 0600)
	os.WriteFile(other, []byte("other"), 0600)
	os.Mkdir(dir, 0700)
	f := superviseFake(t, 0, 0, nil)
	f.waitRuntime(t, func(st Runtime) bool { return st.Ready })
	if _, err := os.Stat(left); !os.IsNotExist(err) {
		t.Fatal("a left prefetch file survived the engine start", err)
	}
	during := filepath.Join(tmp, "gopeed-prefetch-456")
	os.WriteFile(during, []byte("dropped resolve"), 0600)
	f.stop()
	if err := <-f.done; err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(during); !os.IsNotExist(err) {
		t.Fatal("a prefetch file survived the engine stop", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another file was removed", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatal("a directory was removed", err)
	}
}

// A prefetch file another process still has open, such as an engine a
// killed daemon left running, stays. /proc names it by its real path.
func TestSweepPrefetchKeepsOpenFiles(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	open, closed := filepath.Join(tmp, "gopeed-prefetch-open"), filepath.Join(tmp, "gopeed-prefetch-closed")
	os.WriteFile(open, nil, 0600)
	os.WriteFile(closed, nil, 0600)
	sweepPrefetch(tmp, map[string]bool{open: true})
	if _, err := os.Stat(open); err != nil {
		t.Fatal("an open prefetch file was removed", err)
	}
	if _, err := os.Stat(closed); !os.IsNotExist(err) {
		t.Fatal("a closed prefetch file stayed", err)
	}
}

func TestOpenFilesListsThisProcessesFiles(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("open files are read from /proc")
	}
	p := filepath.Join(t.TempDir(), "gopeed-prefetch-held")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if !openFiles()[p] {
		t.Fatal("an open file is not listed")
	}
}
