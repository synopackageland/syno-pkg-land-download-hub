package binaries

import (
	"os"
	"path/filepath"
)

// prefetchPattern is Gopeed 1.9.3's os.CreateTemp pattern for the data an
// HTTP resolve prefetches (internal/protocol/http/fetcher.go asyncPrefetch).
// Upstream removes the file only when that resolve starts a download.
const prefetchPattern = "gopeed-prefetch-*"

// sweepEngineTemp removes the prefetch files the engine left in the TMPDIR
// it inherits. Call it only while this daemon's engine is not running.
func sweepEngineTemp() {
	sweepPrefetch(os.TempDir(), openFiles())
}

// sweepPrefetch removes the regular prefetch files directly in dir that no
// process has open.
func sweepPrefetch(dir string, open map[string]bool) {
	// /proc names open files by their real path, not the package's links.
	if real, e := filepath.EvalSymlinks(dir); e == nil {
		dir = real
	}
	paths, _ := filepath.Glob(filepath.Join(dir, prefetchPattern))
	for _, p := range paths {
		if info, e := os.Lstat(p); e == nil && info.Mode().IsRegular() && !open[p] {
			os.Remove(p)
		}
	}
}

// openFiles lists the files open in the processes this account may inspect;
// none where /proc is missing.
func openFiles() map[string]bool {
	open := map[string]bool{}
	fds, _ := filepath.Glob("/proc/[0-9]*/fd/*")
	for _, fd := range fds {
		if p, e := os.Readlink(fd); e == nil {
			open[p] = true
		}
	}
	return open
}
