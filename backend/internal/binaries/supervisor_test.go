package binaries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOfficialSupervisorOwnershipRecoveryPrecedesResume(t *testing.T) {
	binary := os.Getenv("GOPEED_TEST_BINARY")
	if binary == "" {
		t.Skip("official executable not selected")
	}
	binary, _ = filepath.Abs(binary)
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			data := t.TempDir()
			data, _ = filepath.EvalSymlinks(data)
			h, err := Hash(binary)
			if err != nil {
				t.Fatal(err)
			}
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			port := l.Addr().(*net.TCPAddr).Port
			l.Close()
			os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
			cfg := filepath.Join(data, "engine.json")
			Atomic(cfg, map[string]any{"address": "127.0.0.1", "port": port, "apiToken": strings.Repeat("a", 64), "storageDir": filepath.Join(data, "storage")})
			// resumeInterrupted must see the recovered journal, not this old one.
			os.WriteFile(filepath.Join(data, "jobs.json"), []byte("{"), 0600)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			called := make(chan struct{}, 1)
			done := make(chan error, 1)
			go func() {
				done <- Supervise(ctx, data, Selection{Path: binary, Version: "1.9.3", SHA256: h}, cfg, fmt.Sprintf("http://127.0.0.1:%d", port), func(context.Context) error {
					called <- struct{}{}
					if fail {
						return errors.New("injected ownership recovery failure")
					}
					return Atomic(filepath.Join(data, "jobs.json"), map[string]any{"jobs": map[string]any{}})
				})
			}()
			select {
			case <-called:
			case <-ctx.Done():
				t.Fatal("ownership callback not called")
			}
			if fail {
				if err := <-done; err == nil {
					t.Fatal("recovery failure ignored")
				}
				st, _ := ReadRuntime(data)
				if st.Ready {
					t.Fatal("failed ownership recovery marked ready")
				}
				return
			}
			ready := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				st, _ := ReadRuntime(data)
				if st.Ready {
					ready = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if !ready {
				t.Fatal("resume ran before ownership journal recovery")
			}
		})
	}
}

func TestOfficialSupervisorUpdateRollbackAndFailedCandidate(t *testing.T) {
	binary := os.Getenv("GOPEED_TEST_BINARY")
	if binary == "" {
		t.Skip("official executable not selected")
	}
	binary, _ = filepath.Abs(binary)
	data := t.TempDir()
	data, _ = filepath.EvalSymlinks(data)
	h, e := Hash(binary)
	if e != nil {
		t.Fatal(e)
	}
	bundled := Selection{Path: binary, Version: "1.9.3", SHA256: h}
	// SPK upgrades replace the fixed bundled path; persisted metadata may
	// describe the previous package payload, which cannot be rolled back.
	oldBundle := Selection{Path: binary, Version: "1.9.2", SHA256: strings.Repeat("0", 64)}
	if e := Atomic(filepath.Join(data, "runtime.json"), Runtime{Active: oldBundle, Previous: &oldBundle}); e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	token := strings.Repeat("a", 64)
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(token), 0600)
	cfg := filepath.Join(data, "engine.json")
	b, _ := json.Marshal(map[string]any{"address": "127.0.0.1", "port": port, "apiToken": token, "storageDir": filepath.Join(data, "storage")})
	os.WriteFile(cfg, b, 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- Supervise(ctx, data, bundled, cfg, fmt.Sprintf("http://127.0.0.1:%d", port)) }()
	defer func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(10 * time.Second):
			t.Error("supervisor did not stop")
		}
	}()
	ready := false
	for until := time.Now().Add(10 * time.Second); time.Now().Before(until); {
		st, e := ReadRuntime(data)
		if e == nil && st.Ready {
			if st.Active != bundled || st.Previous == nil || *st.Previous != bundled {
				t.Fatal("stale bundled metadata after SPK upgrade", st)
			}
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		t.Fatal("not ready")
	}
	raw, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(data, "managed", "1.9.3", "gopeed")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, raw, 0700)
	candidate := Selection{Path: p, Version: "1.9.3", SHA256: h}
	m := Manager{Data: data}
	st, e := m.Activate(ctx, candidate)
	if e != nil || st.Active != candidate || st.Previous == nil || *st.Previous != bundled {
		t.Fatal("managed activation", st, e)
	}
	st, e = m.Activate(ctx, *st.Previous)
	if e != nil || st.Active != bundled {
		t.Fatal("rollback", st, e)
	}
	// The real executable reports 1.9.3. A candidate claiming 1.9.4 must be
	// rejected and the original healthy process restored, not merely selected.
	candidate.Version = "1.9.4"
	st, e = m.Activate(ctx, candidate)
	if e == nil || !st.Ready || st.Active != bundled || st.Error != "candidate_failed_previous_restored" {
		t.Fatal("failed candidate recovery", st, e)
	}
}

func TestOfficialSupervisorBadRecoveryJournalIsNotReady(t *testing.T) {
	binary := os.Getenv("GOPEED_TEST_BINARY")
	if binary == "" {
		t.Skip("official executable not selected")
	}
	binary, _ = filepath.Abs(binary)
	data := t.TempDir()
	data, _ = filepath.EvalSymlinks(data)
	h, e := Hash(binary)
	if e != nil {
		t.Fatal(e)
	}
	bundled := Selection{Path: binary, Version: "1.9.3", SHA256: h}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	token := strings.Repeat("a", 64)
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(token), 0600)
	cfg := filepath.Join(data, "engine.json")
	Atomic(cfg, map[string]any{"address": "127.0.0.1", "port": port, "apiToken": token, "storageDir": filepath.Join(data, "storage")})
	Atomic(filepath.Join(data, "runtime.json"), Runtime{Active: bundled, Ready: true})
	os.WriteFile(filepath.Join(data, "jobs.json"), []byte(`{`), 0600)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e := Supervise(ctx, data, bundled, cfg, fmt.Sprintf("http://127.0.0.1:%d", port)); e == nil {
		t.Fatal("invalid recovery journal accepted")
	}
	st, e := ReadRuntime(data)
	if e != nil || st.Ready {
		t.Fatal("stale ready after startup recovery failure", st, e)
	}
}
