package binaries

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPluginUpdatePreservesSettingsAndRollsBackFailure(t *testing.T) {
	for _, failure := range []string{"none", "upstream-error", "settings-reset", "disabled-reset"} {
		fail := failure != "none"
		t.Run(failure, func(t *testing.T) {
			data := t.TempDir()
			storage := filepath.Join(data, "storage")
			os.MkdirAll(storage, 0700)
			statePath := filepath.Join(storage, "state.json")
			old := `{"identity":"gopeed@sample","version":"1.0.0","disabled":false,"settings":[{"name":"label","type":"string","value":"keep-me"}]}`
			os.WriteFile(statePath, []byte(old), 0600)
			os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/tasks" {
					w.Write([]byte(`{"code":0,"data":[]}`))
					return
				}
				if r.Method == "POST" {
					updated := strings.Replace(old, "1.0.0", "1.1.0", 1)
					if failure == "settings-reset" {
						updated = strings.Replace(updated, "keep-me", "reset", 1)
					}
					if failure == "disabled-reset" {
						updated = strings.Replace(updated, `"disabled":false`, `"disabled":true`, 1)
					}
					os.WriteFile(statePath, []byte(updated), 0600)
					if failure == "upstream-error" {
						w.Write([]byte(`{"code":1}`))
						return
					}
				}
				b, _ := os.ReadFile(statePath)
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": json.RawMessage(b)})
			}))
			defer server.Close()
			running := true
			stops := 0
			halt := func() { running = false; stops++ }
			boot := func() error { running = true; return nil }
			e := pluginTransaction(context.Background(), data, server.URL, "req", PluginUpdate{ID: "gopeed@sample", Version: "1.1.0"}, halt, boot)
			b, _ := os.ReadFile(statePath)
			if !running || stops == 0 {
				t.Fatal("process lifecycle not exercised")
			}
			if fail {
				if e == nil || string(b) != old {
					t.Fatal("old state not restored", e, string(b))
				}
			} else {
				if e != nil || !strings.Contains(string(b), "1.1.0") || !strings.Contains(string(b), "keep-me") {
					t.Fatal("state not retained", e, string(b))
				}
			}
			if _, e := os.Stat(filepath.Join(data, "plugin-transaction.json")); !os.IsNotExist(e) {
				t.Fatal("unsettled transaction", e)
			}
		})
	}
}
func TestPluginInterruptedTransactionRecoversBeforeEngineStarts(t *testing.T) {
	data := t.TempDir()
	os.MkdirAll(filepath.Join(data, "plugin-snapshot"), 0700)
	os.WriteFile(filepath.Join(data, "plugin-snapshot", "state"), []byte("old"), 0600)
	os.MkdirAll(filepath.Join(data, "storage"), 0700)
	os.WriteFile(filepath.Join(data, "storage", "state"), []byte("partial"), 0600)
	Atomic(filepath.Join(data, "plugin-transaction.json"), map[string]string{"request": "interrupted", "plugin": "sample"})
	id, e := recoverPlugin(data)
	b, _ := os.ReadFile(filepath.Join(data, "storage", "state"))
	if e != nil || id != "interrupted" || string(b) != "old" {
		t.Fatal(id, e, string(b))
	}
}

func TestPluginSnapshotRejectsSymlinksAndOversizedStore(t *testing.T) {
	source := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("private"), 0600)
	os.Symlink(outside, filepath.Join(source, "link"))
	if copyStore(source, filepath.Join(t.TempDir(), "copy")) == nil {
		t.Fatal("symlink snapshot accepted")
	}
	os.Remove(filepath.Join(source, "link"))
	f, e := os.Create(filepath.Join(source, "huge"))
	if e != nil {
		t.Fatal(e)
	}
	f.Truncate((512 << 20) + 1)
	f.Close()
	if copyStore(source, filepath.Join(t.TempDir(), "copy")) == nil {
		t.Fatal("snapshot byte bound not enforced")
	}
}
func TestPluginStableVersionComparison(t *testing.T) {
	for _, pair := range [][2]string{{"1.0.1", "1.0.0"}, {"2.0", "1.9.9"}} {
		if !NewerVersion(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
	for _, pair := range [][2]string{{"1.0.0", "1.0.0"}, {"0.9.0", "1.0.0"}, {"2.0.0-beta", "1.0.0"}, {"garbage", "1.0.0"}} {
		if NewerVersion(pair[0], pair[1]) {
			t.Fatal(pair)
		}
	}
}
func TestPendingPluginRequestFencesNewOperations(t *testing.T) {
	data := t.TempDir()
	m := Manager{Data: data}
	Atomic(filepath.Join(data, "runtime.json"), Runtime{Request: "old", Ready: true})
	Atomic(filepath.Join(data, "engine-request.json"), Request{ID: "accepted", Plugin: &PluginUpdate{ID: "sample", Version: "1.1.0"}})
	if !m.Pending() {
		t.Fatal("accepted disconnected update not fenced")
	}
	Atomic(filepath.Join(data, "runtime.json"), Runtime{Request: "accepted", Ready: true})
	if m.Pending() {
		t.Fatal("completed operation remains fenced")
	}
}

// A paused download resumes with the new version; a running or queued one
// would run on while the engine restarts.
func TestPluginUpdateWaitsOnlyForDownloadsThatRun(t *testing.T) {
	for status, want := range map[string]string{"pause": "", "done": "", "error": "", "running": "downloads_running", "wait": "downloads_running", "ready": "downloads_running"} {
		t.Run(status, func(t *testing.T) {
			data := t.TempDir()
			os.MkdirAll(filepath.Join(data, "storage"), 0700)
			os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
			version := "1.0.0"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.URL.Path == "/api/v1/tasks":
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{map[string]any{"status": status}}})
				case r.Method == "POST":
					version = "1.1.0"
					w.Write([]byte(`{"code":0}`))
				default:
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"identity": "gopeed@sample", "version": version}})
				}
			}))
			defer server.Close()
			e := pluginTransaction(context.Background(), data, server.URL, "req", PluginUpdate{ID: "gopeed@sample", Version: "1.1.0"}, func() {}, func() error { return nil })
			got := ""
			if e != nil {
				got = e.Error()
			}
			if got != want || (want == "" && version != "1.1.0") {
				t.Fatal(got, version)
			}
		})
	}
}
