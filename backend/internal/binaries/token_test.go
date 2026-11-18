package binaries

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEngineTokenRotatesPrivatelyWithoutChangingDownloadState(t *testing.T) {
	data := filepath.Join(t.TempDir(), "gopeed")
	if err := os.Mkdir(data, 0755); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(data, "engine.json")
	config := map[string]any{"apiToken": "previously-exposed", "storageDir": filepath.Join(data, "storage"), "downloadConfig": map[string]any{"maxRunning": 7}}
	if err := Atomic(cfg, config); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(cfg, 0644); err != nil {
		t.Fatal(err)
	}
	// Replace a legacy symlink instead of truncating a file outside private state.
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("unchanged"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(data, "engine.token")); err != nil {
		t.Fatal(err)
	}
	state := []byte(`{"jobs":{"existing":{"manualPaused":false}}}`)
	if err := os.WriteFile(filepath.Join(data, "jobs.json"), state, 0600); err != nil {
		t.Fatal(err)
	}
	previous := "previously-exposed"
	for range 2 {
		if err := rotateEngineToken(data, cfg); err != nil {
			t.Fatal(err)
		}
		token, err := os.ReadFile(filepath.Join(data, "engine.token"))
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := hex.DecodeString(string(token))
		if err != nil || len(decoded) != 32 || string(token) == previous {
			t.Fatal("token was not freshly generated")
		}
		previous = string(token)
		raw, err := os.ReadFile(cfg)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatal(err)
		}
		if got["apiToken"] != string(token) || got["storageDir"] != config["storageDir"] || got["downloadConfig"].(map[string]any)["maxRunning"] != float64(7) {
			t.Fatal("config and gateway token disagree or settings changed")
		}
		for path, mode := range map[string]os.FileMode{data: 0700, cfg: 0600, filepath.Join(data, "engine.token"): 0600} {
			info, err := os.Stat(path)
			if err != nil || info.Mode().Perm() != mode {
				t.Fatal("engine credentials are not private")
			}
		}
		entries, err := os.ReadDir(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				t.Fatal("temporary credential file left behind")
			}
		}
	}
	if got, _ := os.ReadFile(outside); string(got) != "unchanged" {
		t.Fatal("legacy symlink target was overwritten")
	}
	if got, _ := os.ReadFile(filepath.Join(data, "jobs.json")); !bytes.Equal(got, state) {
		t.Fatal("existing tasks changed")
	}
}

func TestEngineTokenStartsWithoutALegacyToken(t *testing.T) {
	data := t.TempDir()
	cfg := filepath.Join(data, "engine.json")
	if err := Atomic(cfg, map[string]any{"storageDir": filepath.Join(data, "storage")}); err != nil {
		t.Fatal(err)
	}
	if err := rotateEngineToken(data, cfg); err != nil {
		t.Fatal(err)
	}
	token, err := os.ReadFile(filepath.Join(data, "engine.token"))
	if err != nil || len(token) != 64 {
		t.Fatal("first start did not provision a token")
	}
}

func TestEngineTokenInvalidConfigDoesNotExposeContents(t *testing.T) {
	for _, raw := range []string{`null`, `{"apiToken":"sensitive-old-value", broken}`} {
		data := t.TempDir()
		cfg := filepath.Join(data, "engine.json")
		if err := os.WriteFile(cfg, []byte(raw), 0600); err != nil {
			t.Fatal(err)
		}
		err := rotateEngineToken(data, cfg)
		if err == nil || err.Error() != "invalid private engine config" {
			t.Fatal("invalid config was accepted or exposed in an error")
		}
		if _, err := os.Stat(filepath.Join(data, "engine.token")); !os.IsNotExist(err) {
			t.Fatal("invalid config published a token")
		}
	}
}
