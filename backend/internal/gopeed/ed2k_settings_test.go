package gopeed

import (
	"testing"
)

// upstreamProtocols is the 1.9.3 default config: ED2K listens on random
// ports (0) and bootstraps from its built-in server and node lists.
func upstreamProtocols() map[string]any {
	cfg := upstreamBT()
	cfg["protocolConfig"].(map[string]any)["ed2k"] = map[string]any{"listenPort": float64(0), "udpPort": float64(0), "serverAddr": "45.82.80.155:5687", "serverMet": "ed2k://|serverlist|http://upd.emule-security.org/server.met|/", "nodesDat": "https://upd.emule-security.org/nodes.dat"}
	return cfg
}

func TestSettingsPinTheED2KPortsAndKeepItsServers(t *testing.T) {
	s, _, _ := fixture(t)
	cfg := upstreamProtocols()
	writes := btEngine(t, s, cfg)
	out := adminCall(t, s, "gopeed.settings.get", map[string]any{})
	data, _ := out["data"].(map[string]any)
	if out["success"] != true || data["ed2kPort"] != float64(DefaultED2KPort) || data["ed2kUdpPort"] != float64(DefaultED2KUDPPort) || *writes != 1 {
		t.Fatal(out, *writes)
	}
	ed2k := cfg["protocolConfig"].(map[string]any)["ed2k"].(map[string]any)
	if ed2k["listenPort"] != float64(DefaultED2KPort) || ed2k["udpPort"] != float64(DefaultED2KUDPPort) || ed2k["serverAddr"] != "45.82.80.155:5687" || ed2k["nodesDat"] != "https://upd.emule-security.org/nodes.dat" {
		t.Fatal(ed2k)
	}
	adminCall(t, s, "gopeed.settings.get", map[string]any{})
	if *writes != 1 {
		t.Fatal("aligned config rewritten")
	}
}

func TestSettingsBoundTheED2KPortsApartFromBT(t *testing.T) {
	s, _, _ := fixture(t)
	cfg := upstreamProtocols()
	writes := btEngine(t, s, cfg)
	valid := Settings{MaxRunning: 2, Connections: 4, BTPort: 16889, ED2KPort: 4662, ED2KUDPPort: 4672}
	for _, change := range []func(*Settings){
		func(p *Settings) { p.ED2KPort = 80 },
		func(p *Settings) { p.ED2KPort = 65536 },
		func(p *Settings) { p.ED2KUDPPort = 1023 },
		func(p *Settings) { p.ED2KPort = p.BTPort },
		func(p *Settings) { p.ED2KUDPPort = p.BTPort },
	} {
		p := valid
		change(&p)
		if adminCall(t, s, "gopeed.settings.set", p)["success"] != false {
			t.Fatal(p)
		}
	}
	if *writes != 0 {
		t.Fatal("invalid settings reached the engine")
	}
	want := valid
	want.ED2KPort, want.ED2KUDPPort = 20001, 20001
	if out := adminCall(t, s, "gopeed.settings.set", want); out["success"] != true {
		t.Fatal(out)
	}
	ed2k := cfg["protocolConfig"].(map[string]any)["ed2k"].(map[string]any)
	if ed2k["listenPort"] != float64(20001) || ed2k["udpPort"] != float64(20001) || ed2k["serverMet"] == nil {
		t.Fatal(ed2k)
	}
	if saved, err := LoadSettings(s.Data); err != nil || !sameSettings(saved, want) {
		t.Fatal(saved, err)
	}
}

func TestEd2kResolveAlignsThePortsFirst(t *testing.T) {
	s, f := newEngineFixture(t)
	if out := btResolve(t, s, "admin", ed2kLink); out["success"] != true {
		t.Fatal(out)
	}
	if ed2k := f.cfg["protocolConfig"].(map[string]any)["ed2k"].(map[string]any); ed2k["listenPort"] != float64(DefaultED2KPort) {
		t.Fatal(ed2k)
	}
}
