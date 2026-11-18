package gopeed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func btEngine(t *testing.T, s *Service, cfg map[string]any) *int {
	t.Helper()
	writes := new(int)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			*writes++
			if json.NewDecoder(r.Body).Decode(&cfg) != nil {
				t.Error("bad config")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": cfg})
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	return writes
}

func upstreamBT() map[string]any {
	return map[string]any{"maxRunning": float64(2), "protocolConfig": map[string]any{"http": map[string]any{"connections": float64(4)}, "bt": map[string]any{"listenPort": float64(0), "trackers": []any{}, "seedKeep": false, "seedRatio": float64(1), "seedTime": float64(7200)}}}
}

func TestSettingsDefaultToNoSeedingOnAFixedPort(t *testing.T) {
	s, _, _ := fixture(t)
	cfg := upstreamBT()
	writes := btEngine(t, s, cfg)
	out := adminCall(t, s, "gopeed.settings.get", map[string]any{})
	data, _ := out["data"].(map[string]any)
	if out["success"] != true || data["btPort"] != float64(DefaultBTPort) || data["seedRatio"] != float64(0) || data["seedMinutes"] != float64(0) || *writes != 1 {
		t.Fatal(out, *writes)
	}
	bt := cfg["protocolConfig"].(map[string]any)["bt"].(map[string]any)
	// Upstream treats ratio 0 and time 0 as "seed forever"; one second stops it.
	if bt["seedKeep"] != false || bt["seedRatio"] != float64(0) || bt["seedTime"] != float64(1) || bt["listenPort"] != float64(DefaultBTPort) {
		t.Fatal(bt)
	}
	adminCall(t, s, "gopeed.settings.get", map[string]any{})
	if *writes != 1 {
		t.Fatal("aligned config rewritten")
	}
}

func TestSettingsBoundSeedingAndPortAndPersistThem(t *testing.T) {
	s, _, _ := fixture(t)
	cfg := upstreamBT()
	writes := btEngine(t, s, cfg)
	for _, p := range []Settings{
		{MaxRunning: 2, Connections: 4, BTPort: 80},
		{MaxRunning: 2, Connections: 4, BTPort: 65536},
		{MaxRunning: 2, Connections: 4, BTPort: 16889, SeedRatio: -1},
		{MaxRunning: 2, Connections: 4, BTPort: 16889, SeedRatio: 101},
		{MaxRunning: 2, Connections: 4, BTPort: 16889, SeedMinutes: 10081},
	} {
		if adminCall(t, s, "gopeed.settings.set", p)["success"] != false {
			t.Fatal(p)
		}
	}
	if *writes != 0 {
		t.Fatal("invalid settings reached the engine")
	}
	want := Settings{MaxRunning: 3, Connections: 6, SeedRatio: 1.5, SeedMinutes: 30, BTPort: 20000, ED2KPort: DefaultED2KPort, ED2KUDPPort: DefaultED2KUDPPort}
	if out := adminCall(t, s, "gopeed.settings.set", want); out["success"] != true {
		t.Fatal(out)
	}
	bt := cfg["protocolConfig"].(map[string]any)["bt"].(map[string]any)
	if bt["seedRatio"] != 1.5 || bt["seedTime"] != float64(1800) || bt["listenPort"] != float64(20000) || cfg["protocolConfig"].(map[string]any)["http"].(map[string]any)["connections"] != float64(6) {
		t.Fatal(cfg)
	}
	saved, err := LoadSettings(s.Data)
	if err != nil || !sameSettings(saved, want) {
		t.Fatal(saved, err)
	}
}

func TestLegacySettingsFileGainsBTAndED2KDefaults(t *testing.T) {
	data := t.TempDir()
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"maxRunning":4,"connections":8}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSettings(data)
	if err != nil || !sameSettings(got, Settings{MaxRunning: 4, Connections: 8, BTPort: DefaultBTPort, ED2KPort: DefaultED2KPort, ED2KUDPPort: DefaultED2KUDPPort}) {
		t.Fatal(got, err)
	}
	if got, err := LoadSettings(t.TempDir()); err != nil || !sameSettings(got, DefaultSettings()) {
		t.Fatal(got, err)
	}
	if err := os.WriteFile(filepath.Join(data, "settings.json"), []byte(`{"maxRunning":40,"connections":8}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadSettings(data); err == nil {
		t.Fatal("accepted invalid saved settings")
	}
	pc := EngineProtocolConfig(DefaultSettings())
	if pc["bt"].(map[string]any)["seedTime"] != 1 || pc["http"].(map[string]any)["connections"] != 4 {
		t.Fatal(pc)
	}
}

// Administrators set extra public trackers; they reach the engine, persist
// and read back. Omitting them keeps the saved list.
func TestSettingsTrackersAreValidatedPersistedAndKept(t *testing.T) {
	s, _, _ := fixture(t)
	cfg := upstreamBT()
	btEngine(t, s, cfg)
	base := map[string]any{"maxRunning": 2, "connections": 4, "seedRatio": 0, "seedMinutes": 0, "btPort": DefaultBTPort, "ed2kPort": DefaultED2KPort, "ed2kUdpPort": DefaultED2KUDPPort}
	with := func(trackers any) map[string]any {
		p := map[string]any{}
		for k, v := range base {
			p[k] = v
		}
		if trackers != nil {
			p["trackers"] = trackers
		}
		return p
	}
	good := []any{"udp://tracker.example.org:1337/announce", "https://tracker.example.net/announce"}
	out := adminCall(t, s, "gopeed.settings.set", with(good))
	if out["success"] != true {
		t.Fatal(out)
	}
	bt := cfg["protocolConfig"].(map[string]any)["bt"].(map[string]any)
	if got, _ := json.Marshal(bt["trackers"]); string(got) != `["udp://tracker.example.org:1337/announce","https://tracker.example.net/announce"]` {
		t.Fatal(string(got))
	}
	out = adminCall(t, s, "gopeed.settings.get", map[string]any{})
	if got, _ := json.Marshal(out["data"].(map[string]any)["trackers"]); string(got) != `["udp://tracker.example.org:1337/announce","https://tracker.example.net/announce"]` {
		t.Fatal(out)
	}
	if out = adminCall(t, s, "gopeed.settings.set", with(nil)); out["success"] != true {
		t.Fatal(out)
	}
	if got, _ := json.Marshal(cfg["protocolConfig"].(map[string]any)["bt"].(map[string]any)["trackers"]); string(got) != `["udp://tracker.example.org:1337/announce","https://tracker.example.net/announce"]` {
		t.Fatal("omitted trackers were cleared", string(got))
	}
	many := []any{}
	for i := 0; i < 65; i++ {
		many = append(many, "udp://t"+strconv.Itoa(i)+".example.org/announce")
	}
	for _, bad := range []any{
		[]any{"ftp://tracker.example.org/announce"},
		[]any{"udp://127.0.0.1:6969/announce"},
		[]any{"http://192.168.1.2/announce"},
		[]any{"http://localhost/announce"},
		[]any{"https://user:pass@tracker.example.org/announce"},
		[]any{"udp://tracker.example.org:1337/announce", "udp://tracker.example.org:1337/announce"},
		many,
	} {
		if out := adminCall(t, s, "gopeed.settings.set", with(bad)); out["success"] == true {
			t.Fatal("accepted", bad)
		}
	}
	if out = adminCall(t, s, "gopeed.settings.set", with([]any{})); out["success"] != true {
		t.Fatal(out)
	}
	raw, _ := os.ReadFile(filepath.Join(s.Data, "settings.json"))
	var saved Settings
	if json.Unmarshal(raw, &saved) != nil || len(saved.Trackers) != 0 {
		t.Fatal(string(raw))
	}
}
