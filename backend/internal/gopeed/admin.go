package gopeed

import (
	"context"
	"dsmapp/internal/binaries"
	"encoding/json"
	"errors"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Settings struct {
	MaxRunning  int     `json:"maxRunning"`
	Connections int     `json:"connections"`
	SeedRatio   float64 `json:"seedRatio"`
	SeedMinutes int     `json:"seedMinutes"`
	BTPort      int     `json:"btPort"`
	ED2KPort    int     `json:"ed2kPort"`
	ED2KUDPPort int     `json:"ed2kUdpPort"`
	// Trackers are extra public trackers every BT task announces to.
	Trackers []string `json:"trackers"`
}

// core manages the Gopeed binaries, checking releases through coreHTTP.
func (s *Service) core() binaries.Manager {
	return binaries.Manager{Data: s.Data, HTTP: s.coreHTTP}
}

func (s *Service) adminRoutes() []api.Route {
	routes := []api.Route{api.Handle("gopeed.session", func(ctx context.Context, c api.Caller, raw json.RawMessage) (any, error) {
		if e := decodeStrict(raw, &struct{}{}); e != nil {
			return nil, e
		}
		return map[string]any{"user": c.User(), "admin": c.Admin(), "nonroot": true}, nil
	})}
	handler := func(method string) api.Handler {
		return func(ctx context.Context, c api.Caller, raw json.RawMessage) (any, error) {
			if err := api.RequireAdmin(c); err != nil {
				return nil, err
			}
			if method == "gopeed.plugins.catalog" {
				return s.catalog(ctx, raw)
			}
			if method == "binaries.status" {
				if e := decodeStrict(raw, &struct{}{}); e != nil {
					return nil, e
				}
				return binaries.ReadRuntime(s.Data)
			}
			if method == "binaries.check" {
				if e := decodeStrict(raw, &struct{}{}); e != nil {
					return nil, e
				}
				ctx, cancel := context.WithTimeout(ctx, checkDeadline)
				defer cancel()
				release, e := s.core().Release(ctx)
				if e != nil {
					return nil, sourceRefused(e)
				}
				return release, nil
			}
			lockCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			defer cancel()
			m := s.core()
			unlock, e := m.Lock(lockCtx)
			if e != nil {
				return nil, refused("engine_busy")
			}
			defer unlock()
			if (binaries.Manager{Data: s.Data}).Pending() {
				return nil, refused("engine_busy")
			}
			switch method {
			case "binaries.update":
				var in struct {
					Version string `json:"version"`
					SHA256  string `json:"sha256"`
					Consent bool   `json:"consent"`
				}
				if e = decodeStrict(raw, &in); e != nil {
					return nil, e
				}
				if !in.Consent {
					return nil, refused("consent_required")
				}
				if e = s.idle(ctx, binaries.HoldsCoreUpdate, "downloads_active"); e != nil {
					return nil, e
				}
				release, e := m.Release(ctx)
				if e != nil {
					return nil, sourceRefused(e)
				}
				if release.Version != in.Version || release.SHA256 != in.SHA256 {
					return nil, refused("release_changed")
				}
				selection, e := m.Install(ctx, release)
				if e != nil {
					return nil, e
				}
				return m.Activate(ctx, selection)
			case "binaries.rollback":
				var in struct {
					Consent bool `json:"consent"`
				}
				if e = decodeStrict(raw, &in); e != nil {
					return nil, e
				}
				if !in.Consent {
					return nil, refused("consent_required")
				}
				if e = s.idle(ctx, binaries.HoldsCoreUpdate, "downloads_active"); e != nil {
					return nil, e
				}
				st, e := binaries.ReadRuntime(s.Data)
				if e != nil || st.Previous == nil {
					return nil, refused("rollback_unavailable")
				}
				return m.Activate(ctx, *st.Previous)
			case "gopeed.settings.get":
				if e = decodeStrict(raw, &struct{}{}); e != nil {
					return nil, e
				}
				cfg, e := s.ensureProtocolConfig(ctx)
				if e != nil {
					return nil, e
				}
				return visibleSettings(cfg), nil
			case "gopeed.settings.set":
				// Omitted trackers keep the saved list, so callers that know
				// only the numeric preferences do not clear it.
				var in struct {
					Settings
					Trackers *[]string `json:"trackers"`
				}
				if e = decodeStrict(raw, &in); e != nil {
					return nil, e
				}
				prefs := in.Settings
				if in.Trackers != nil {
					prefs.Trackers = *in.Trackers
				} else if saved, e := LoadSettings(s.Data); e == nil {
					prefs.Trackers = saved.Trackers
				}
				prefs.Trackers = trackerList(prefs.Trackers)
				if !validSettings(prefs) {
					return nil, refused("invalid_request")
				}
				cfg, e := s.config(ctx)
				if e != nil {
					return nil, e
				}
				if e = s.putSettings(ctx, cfg, prefs); e != nil {
					return nil, e
				}
				if e = binaries.Atomic(filepath.Join(s.Data, "settings.json"), prefs); e != nil {
					return nil, e
				}
				read, e := s.config(ctx)
				if e != nil {
					return nil, e
				}
				if !sameSettings(visibleSettings(read), prefs) {
					return nil, refused("settings_readback_failed")
				}
				return prefs, nil
			default:
				return s.plugins(ctx, method, raw)
			}
		}
	}
	return append(routes,
		api.Handle("gopeed.plugins.catalog", handler("gopeed.plugins.catalog")),
		api.Handle("gopeed.plugins.check", handler("gopeed.plugins.check")),
		api.Handle("gopeed.plugins.list", handler("gopeed.plugins.list")),
		api.Handle("gopeed.plugins.install", handler("gopeed.plugins.install")),
		api.Handle("gopeed.plugins.switch", handler("gopeed.plugins.switch")),
		api.Handle("gopeed.plugins.delete", handler("gopeed.plugins.delete")),
		api.Handle("gopeed.plugins.update", handler("gopeed.plugins.update")),
		api.Handle("gopeed.plugins.settings", handler("gopeed.plugins.settings")),
		api.Handle("gopeed.settings.get", handler("gopeed.settings.get")),
		api.Handle("gopeed.settings.set", handler("gopeed.settings.set")),
		api.Handle("binaries.status", handler("binaries.status")),
		api.Handle("binaries.check", handler("binaries.check")),
		api.Handle("binaries.update", handler("binaries.update")),
		api.Handle("binaries.rollback", handler("binaries.rollback")),
	)
}

// idle refuses with code while the engine has a task that holds the change.
func (s *Service) idle(ctx context.Context, holds func(status string) bool, code string) error {
	rep, e := s.engine(ctx, "GET", "/api/v1/tasks", nil, nil)
	if e != nil {
		return e
	}
	var tasks []struct{ Status string }
	if rep.Code != 0 || json.Unmarshal(rep.Data, &tasks) != nil {
		return refused("engine_unavailable")
	}
	for _, t := range tasks {
		if holds(t.Status) {
			return refused(code)
		}
	}
	return nil
}
func (s *Service) config(ctx context.Context) (map[string]any, error) {
	rep, e := s.engine(ctx, "GET", "/api/v1/config", nil, nil)
	if e != nil {
		return nil, e
	}
	var cfg map[string]any
	if rep.Code != 0 || json.Unmarshal(rep.Data, &cfg) != nil || cfg == nil {
		return nil, refused("engine_unavailable")
	}
	return cfg, nil
}

var repoPath = regexp.MustCompile(`^/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var pluginID = regexp.MustCompile(`^[A-Za-z0-9_@.-]{1,200}$`)

func pluginURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Scheme == "https" && u.Host == "github.com" && u.User == nil && u.RawQuery == "" && repoPath.MatchString(u.Path) && !strings.Contains(u.Path, "/.") && (u.Fragment == "" || (path.Clean(u.Fragment) == u.Fragment && !strings.HasPrefix(u.Fragment, ".") && !strings.HasPrefix(u.Fragment, "/") && !strings.ContainsAny(u.Fragment, "\\\x00\r\n")))
}

// checkDeadline bounds an update check, which waits on GitHub: a slow or
// unreachable source fails the check instead of holding the page open.
var checkDeadline = 20 * time.Second

// removeExtension deletes an extension best effort, on a context that
// outlives a canceled request so the cleanup still runs.
func (s *Service) removeExtension(ctx context.Context, identity string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	s.engine(ctx, "DELETE", "/api/v1/extensions/"+url.PathEscape(identity), nil, nil)
}

func (s *Service) plugins(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	var in struct {
		ID       string         `json:"id"`
		Version  string         `json:"version"`
		URL      string         `json:"url"`
		Enabled  bool           `json:"enabled"`
		Consent  bool           `json:"consent"`
		Settings map[string]any `json:"settings"`
	}
	if e := decodeStrict(raw, &in); e != nil {
		return nil, e
	}
	verb, p, data := "GET", "/api/v1/extensions", any(nil)
	if method != "gopeed.plugins.list" && method != "gopeed.plugins.install" {
		if !pluginID.MatchString(in.ID) {
			return nil, refused("invalid_request")
		}
		p += "/" + url.PathEscape(in.ID)
	}
	switch method {
	case "gopeed.plugins.list":
		if in.ID != "" || in.URL != "" || in.Settings != nil {
			return nil, refused("invalid_request")
		}
	case "gopeed.plugins.install":
		if !in.Consent || !pluginURL(in.URL) {
			return nil, refused("plugin_source_denied")
		}
		verb = "POST"
		data = map[string]any{"url": in.URL, "devMode": false}
	case "gopeed.plugins.switch":
		if in.Enabled && !in.Consent {
			return nil, refused("consent_required")
		}
		verb = "PUT"
		p += "/switch"
		data = map[string]any{"status": in.Enabled}
	case "gopeed.plugins.delete":
		if !in.Consent {
			return nil, refused("consent_required")
		}
		verb = "DELETE"
	case "gopeed.plugins.check", "gopeed.plugins.update":
		if method == "gopeed.plugins.update" && !in.Consent {
			return nil, refused("consent_required")
		}
		if method == "gopeed.plugins.check" {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, checkDeadline)
			defer cancel()
		}
		rep, e := s.engine(ctx, "GET", p, nil, nil)
		if e != nil {
			return nil, e
		}
		var ext struct {
			Version    string
			DevMode    bool
			Repository struct {
				URL       string
				Directory string
			}
		}
		if rep.Code != 0 || json.Unmarshal(rep.Data, &ext) != nil || !pluginURL(ext.Repository.URL+func() string {
			if ext.Repository.Directory != "" {
				return "#" + ext.Repository.Directory
			}
			return ""
		}()) {
			return nil, refused("plugin_source_denied")
		}
		if method == "gopeed.plugins.update" {
			if ext.DevMode || !binaries.NewerVersion(in.Version, ext.Version) {
				return nil, refused("plugin_release_changed")
			}
			if e = s.idle(ctx, binaries.HoldsExtensionUpdate, "downloads_running"); e != nil {
				return nil, e
			}
			_, e = (binaries.Manager{Data: s.Data}).UpdatePlugin(ctx, binaries.PluginUpdate{ID: in.ID, Version: in.Version})
			if e != nil {
				return nil, refused(e.Error())
			}
			return map[string]bool{"saved": true}, nil
		}
		p += "/update"
	case "gopeed.plugins.settings":
		if len(in.Settings) > 32 {
			return nil, refused("invalid_request")
		}
		rep, e := s.engine(ctx, "GET", p, nil, nil)
		if e != nil {
			return nil, e
		}
		var ext struct {
			Settings []struct {
				Name string
				Type string
			}
		}
		if rep.Code != 0 || json.Unmarshal(rep.Data, &ext) != nil {
			return nil, refused("plugin_failed")
		}
		schema := map[string]string{}
		for _, f := range ext.Settings {
			schema[f.Name] = f.Type
		}
		for key, v := range in.Settings {
			switch schema[key] {
			case "string":
				if str, ok := v.(string); !ok || len(str) > 4096 {
					return nil, refused("invalid_request")
				}
			case "number":
				if _, ok := v.(float64); !ok {
					return nil, refused("invalid_request")
				}
			case "boolean":
				if _, ok := v.(bool); !ok {
					return nil, refused("invalid_request")
				}
			default:
				return nil, refused("invalid_request")
			}
		}
		verb = "PUT"
		p += "/settings"
		data = map[string]any{"settings": in.Settings}
	default:
		return nil, refused("endpoint_denied")
	}
	// Git clone/update can take longer than metadata. Only this request receives
	// the bounded plugin deadline; browser cookies never reach the engine.
	copyService := *s
	copyClient := *s.HTTP
	copyClient.Timeout = 2 * time.Minute
	if tr, ok := copyClient.Transport.(*http.Transport); ok {
		clone := tr.Clone()
		clone.ResponseHeaderTimeout = 110 * time.Second
		copyClient.Transport = clone
		defer clone.CloseIdleConnections()
	}
	copyService.HTTP = &copyClient
	rep, e := copyService.engine(ctx, verb, p, nil, data)
	if e != nil {
		return nil, e
	}
	if rep.Code != 0 {
		return nil, refused("plugin_failed")
	}
	if method == "gopeed.plugins.install" {
		// Install enables the extension; an extension that cannot be shown
		// disabled is removed again so it never stays active unmanaged.
		var identity string
		known := json.Unmarshal(rep.Data, &identity) == nil && identity != "" && len(identity) <= 256
		if !known || !pluginID.MatchString(identity) {
			if known {
				s.removeExtension(ctx, identity)
			}
			return nil, errors.New("invalid plugin identity")
		}
		rep, e = s.engine(ctx, "PUT", "/api/v1/extensions/"+url.PathEscape(identity)+"/switch", nil, map[string]bool{"status": false})
		if e != nil || rep.Code != 0 {
			s.removeExtension(ctx, identity)
			return nil, refused("plugin_disable_failed")
		}
	}
	if method == "gopeed.plugins.check" {
		var check struct {
			NewVersion string `json:"newVersion"`
		}
		if json.Unmarshal(rep.Data, &check) != nil {
			return nil, refused("plugin_failed")
		}
		current, e := s.engine(ctx, "GET", "/api/v1/extensions/"+url.PathEscape(in.ID), nil, nil)
		var installed struct{ Version string }
		if e != nil || current.Code != 0 || json.Unmarshal(current.Data, &installed) != nil {
			return nil, refused("plugin_failed")
		}
		if !binaries.NewerVersion(check.NewVersion, installed.Version) {
			check.NewVersion = ""
		}
		return check, nil
	}
	if method != "gopeed.plugins.list" {
		return map[string]bool{"saved": true}, nil
	}
	var exts []map[string]any
	if json.Unmarshal(rep.Data, &exts) != nil {
		return nil, refused("plugin_failed")
	}
	out := []map[string]any{}
	for _, ext := range exts {
		clean := map[string]any{}
		for _, k := range []string{"identity", "name", "title", "description", "version", "author", "disabled", "settings", "repository"} {
			clean[k] = ext[k]
		}
		// The repository comes from the extension's own manifest; the page
		// links to it, so only a GitHub repository address is passed on.
		repo, _ := ext["repository"].(map[string]any)
		if link, _ := repo["url"].(string); !pluginURL(link) {
			clean["repository"] = nil
		}
		if status, e := binaries.ReadRuntime(s.Data); e == nil && status.Plugin != nil && status.Plugin.ID == ext["identity"] {
			clean["updateError"] = status.Plugin.Error
		}
		out = append(out, clean)
	}
	return out, nil
}
