package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

// DefaultBTPort is the fixed TCP/UDP port of the engine's BT client (peers and
// DHT). Administrators may change it; DSM's firewall must allow it inbound.
const DefaultBTPort = 16889

// DefaultED2KPort and DefaultED2KUDPPort are eMule's customary client ports.
// Upstream listens on random ports, which no firewall rule can allow.
const (
	DefaultED2KPort    = 4662
	DefaultED2KUDPPort = 4672
)

func DefaultSettings() Settings {
	return Settings{MaxRunning: 2, Connections: 4, BTPort: DefaultBTPort, ED2KPort: DefaultED2KPort, ED2KUDPPort: DefaultED2KUDPPort}
}

func validPort(p int) bool { return p >= 1024 && p <= 65535 }

func validSettings(p Settings) bool {
	return p.MaxRunning >= 1 && p.MaxRunning <= 16 && p.Connections >= 1 && p.Connections <= 32 &&
		p.SeedRatio >= 0 && p.SeedRatio <= 100 && p.SeedMinutes >= 0 && p.SeedMinutes <= 7*24*60 &&
		validPort(p.BTPort) && validPort(p.ED2KPort) && validPort(p.ED2KUDPPort) &&
		p.ED2KPort != p.BTPort && p.ED2KUDPPort != p.BTPort && validTrackers(p.Trackers)
}

// maxTrackers bounds the administrator's extra tracker list.
const maxTrackers = 64

// validTrackers admits distinct public http(s)/udp announce URLs: the
// engine contacts them for every user's BT task, so local and private
// hosts are refused like any download address, and so are credentials.
func validTrackers(list []string) bool {
	if len(list) > maxTrackers {
		return false
	}
	seen := map[string]bool{}
	for _, raw := range list {
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 512 || seen[raw] || u.User != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "udp") || !publicHost(u.Hostname()) {
			return false
		}
		seen[raw] = true
	}
	return true
}

// trackerList is list without blank entries, and never nil, so a saved and
// a read-back empty list compare equal.
func trackerList(list []string) []string {
	out := []string{}
	for _, t := range list {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func sameSettings(a, b Settings) bool {
	a.Trackers, b.Trackers = trackerList(a.Trackers), trackerList(b.Trackers)
	return reflect.DeepEqual(a, b)
}

// LoadSettings reads the administrator's saved engine preferences. Files
// written before BT or ED2K support lack those ports and receive defaults.
func LoadSettings(data string) (Settings, error) {
	p := DefaultSettings()
	raw, err := os.ReadFile(filepath.Join(data, "settings.json"))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.BTPort, p.ED2KPort, p.ED2KUDPPort = 0, 0, 0
	if json.Unmarshal(raw, &p) != nil {
		return p, refused("settings_invalid")
	}
	if p.BTPort == 0 {
		p.BTPort = DefaultBTPort
	}
	if p.ED2KPort == 0 && p.ED2KUDPPort == 0 {
		p.ED2KPort, p.ED2KUDPPort = DefaultED2KPort, DefaultED2KUDPPort
	}
	if !validSettings(p) {
		return p, refused("settings_invalid")
	}
	return p, nil
}

// EngineProtocolConfig maps preferences to upstream protocolConfig. Upstream
// seeds forever when both the ratio and time limits are 0, so "stop when the
// download completes" is a one-second time limit; the time limit always
// bounds seeding, and a nonzero ratio may stop it earlier.
func EngineProtocolConfig(p Settings) map[string]any {
	seconds := p.SeedMinutes * 60
	if seconds == 0 {
		seconds = 1
	}
	trackers := []any{}
	for _, t := range p.Trackers {
		trackers = append(trackers, t)
	}
	return map[string]any{
		"http": map[string]any{"connections": p.Connections},
		"bt":   map[string]any{"listenPort": p.BTPort, "trackers": trackers, "seedKeep": false, "seedRatio": p.SeedRatio, "seedTime": seconds},
	}
}

func number(v any) (float64, bool) {
	n, ok := v.(float64)
	return n, ok
}

// visibleSettings reads the administrator-facing values back from the engine.
func visibleSettings(cfg map[string]any) Settings {
	p := DefaultSettings()
	if n, ok := number(cfg["maxRunning"]); ok {
		p.MaxRunning = int(n)
	}
	pc, _ := cfg["protocolConfig"].(map[string]any)
	if hc, ok := pc["http"].(map[string]any); ok {
		if n, ok := number(hc["connections"]); ok {
			p.Connections = int(n)
		}
	}
	if bt, ok := pc["bt"].(map[string]any); ok {
		if n, ok := number(bt["listenPort"]); ok {
			p.BTPort = int(n)
		}
		if n, ok := number(bt["seedRatio"]); ok {
			p.SeedRatio = n
		}
		if n, ok := number(bt["seedTime"]); ok {
			p.SeedMinutes = int(n) / 60
		}
		p.Trackers = []string{}
		if list, ok := bt["trackers"].([]any); ok {
			for _, t := range list {
				if s, ok := t.(string); ok {
					p.Trackers = append(p.Trackers, s)
				}
			}
		}
	}
	if ed2k, ok := pc["ed2k"].(map[string]any); ok {
		if n, ok := number(ed2k["listenPort"]); ok {
			p.ED2KPort = int(n)
		}
		if n, ok := number(ed2k["udpPort"]); ok {
			p.ED2KUDPPort = int(n)
		}
	}
	return p
}

// aligned reports whether the engine's BT config is exactly the mapping of p
// and its ED2K ports are p's.
func aligned(cfg map[string]any, p Settings) bool {
	pc, _ := cfg["protocolConfig"].(map[string]any)
	want, _ := json.Marshal(EngineProtocolConfig(p)["bt"])
	var normalized any
	_ = json.Unmarshal(want, &normalized)
	if _, ok := pc["ed2k"].(map[string]any); ok {
		live := visibleSettings(cfg)
		if live.ED2KPort != p.ED2KPort || live.ED2KUDPPort != p.ED2KUDPPort {
			return false
		}
	}
	return reflect.DeepEqual(pc["bt"], normalized)
}

// putSettings writes p into cfg, preserving every other upstream field.
func (s *Service) putSettings(ctx context.Context, cfg map[string]any, p Settings) error {
	cfg["maxRunning"] = p.MaxRunning
	pc, ok := cfg["protocolConfig"].(map[string]any)
	if !ok {
		pc = map[string]any{}
	}
	hc, ok := pc["http"].(map[string]any)
	if !ok {
		hc = map[string]any{}
	}
	hc["connections"] = p.Connections
	pc["http"] = hc
	pc["bt"] = EngineProtocolConfig(p)["bt"]
	// Only the ports change; the engine filled in its server and node
	// lists, and a map without them would leave ED2K with no servers.
	if ed2k, ok := pc["ed2k"].(map[string]any); ok {
		ed2k["listenPort"], ed2k["udpPort"] = p.ED2KPort, p.ED2KUDPPort
	}
	cfg["protocolConfig"] = pc
	rep, err := s.engine(ctx, "PUT", "/api/v1/config", nil, cfg)
	if err != nil {
		return err
	}
	if rep.Code != 0 {
		return refused("settings_failed")
	}
	return nil
}

// ensureProtocolConfig applies the saved protocol preferences before use. Engine
// config is only seeded on first start, so upgraded installs still carry
// upstream's seed-for-two-hours defaults until this runs.
func (s *Service) ensureProtocolConfig(ctx context.Context) (map[string]any, error) {
	cfg, err := s.config(ctx)
	if err != nil {
		return nil, err
	}
	prefs, err := LoadSettings(s.Data)
	if err != nil {
		return nil, err
	}
	if aligned(cfg, prefs) {
		return cfg, nil
	}
	prefs.MaxRunning, prefs.Connections = visibleSettings(cfg).MaxRunning, visibleSettings(cfg).Connections
	if err = s.putSettings(ctx, cfg, prefs); err != nil {
		return nil, err
	}
	return s.config(ctx)
}
