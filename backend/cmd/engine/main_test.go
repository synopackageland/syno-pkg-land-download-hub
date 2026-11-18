package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"dsmapp/internal/binaries"
)

// DSM reports the package running about a second after the
// launcher starts, while it still hashes the engine. The launcher records
// that it is starting first, so the App says starting, not that the engine
// does not respond; a launcher that then fails leaves nothing starting.
func TestLauncherRecordsStartingBeforeItsOwnChecks(t *testing.T) {
	payload, pkgVar := t.TempDir(), t.TempDir()
	data := filepath.Join(pkgVar, "gopeed")
	os.MkdirAll(data, 0700)
	if err := binaries.Atomic(filepath.Join(data, "runtime.json"), binaries.Runtime{}); err != nil {
		t.Fatal(err)
	}
	// Hashing the engine blocks on this FIFO until the test opens it.
	exe := filepath.Join(payload, "bin", "gopeed")
	os.MkdirAll(filepath.Dir(exe), 0700)
	if err := syscall.Mkfifo(exe, 0600); err != nil {
		t.Fatal(err)
	}
	before := os.Args
	os.Args = []string{"engine", payload, pkgVar}
	t.Cleanup(func() { os.Args = before })
	done := make(chan error, 1)
	go func() { done <- run() }()
	starting := false
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until) && !starting; time.Sleep(10 * time.Millisecond) {
		st, err := binaries.ReadRuntime(data)
		starting = err == nil && st.StartingAt(time.Now()) && !st.Ready
	}
	// Unblock the hash; the payload has no upstream.json, so the launch fails.
	w, err := os.OpenFile(exe, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	if err := <-done; err == nil {
		t.Fatal("a payload without upstream metadata launched")
	}
	if !starting {
		t.Fatal("the launcher did not record starting before hashing the engine")
	}
	if st, err := binaries.ReadRuntime(data); err != nil || st.Starting || st.Ready {
		t.Fatalf("runtime after a failed launch: %+v %v", st, err)
	}
}
