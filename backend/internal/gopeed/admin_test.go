package gopeed

import (
	"bytes"
	"dsmapp/internal/binaries"
	"encoding/json"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type adminAuth struct{}

func (adminAuth) Authenticate(r *http.Request) (api.Identity, error) {
	return api.Identity{User: r.Header.Get("actor"), Admin: r.Header.Get("actor") == "admin"}, nil
}
func TestAdminFeaturesRefuseNormalUsersBeforeEngine(t *testing.T) {
	s, _, calls := fixture(t)
	server, e := NewServer(adminAuth{}, s.Routes()...)
	if e != nil {
		t.Fatal(e)
	}
	for _, method := range []string{"gopeed.plugins.list", "gopeed.plugins.install", "gopeed.settings.get", "gopeed.settings.set", "binaries.check", "binaries.update", "binaries.rollback"} {
		b, _ := json.Marshal(map[string]any{"method": method, "params": map[string]any{}})
		r := httptest.NewRequest("POST", "http://fixture/api.cgi", bytes.NewReader(b))
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("actor", "normal1")
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		var out struct{ Error struct{ Code string } }
		json.Unmarshal(w.Body.Bytes(), &out)
		if out.Error.Code != "admin_required" {
			t.Fatal(method, w.Body.String())
		}
	}
	if *calls != 0 {
		t.Fatal("denied request reached engine")
	}
}
func TestPluginSourceValidation(t *testing.T) {
	for _, u := range []string{"http://github.com/a/b", "https://github.com/a/b#../other", "https://github.com/a/b#x/../../z", "https://evil.invalid/a/b", "/volume1/private", "https://user:password@github.com/a/b"} {
		if pluginURL(u) {
			t.Fatal(u)
		}
	}
	if !pluginURL("https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample") {
		t.Fatal("official sample denied")
	}
}
func adminCall(t *testing.T, s *Service, method string, p any) map[string]any {
	t.Helper()
	server, e := NewServer(adminAuth{}, s.Routes()...)
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(map[string]any{"method": method, "params": p})
	r := httptest.NewRequest("POST", "http://fixture/api.cgi", bytes.NewReader(b))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("actor", "admin")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	var out map[string]any
	if json.Unmarshal(w.Body.Bytes(), &out) != nil {
		t.Fatal(w.Body.String())
	}
	recordRegistryResult(t, method, out)
	return out
}
func TestSettingsPreserveRestrictedConfigAndValidateBounds(t *testing.T) {
	s, _, _ := fixture(t)
	cfg := map[string]any{"maxRunning": float64(2), "downloadDir": "private/staging", "script": map[string]any{"enable": false}, "archive": map[string]any{"autoExtract": false}, "protocolConfig": map[string]any{"http": map[string]any{"connections": float64(4)}}}
	writes := 0
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PUT" {
			writes++
			if json.NewDecoder(r.Body).Decode(&cfg) != nil {
				t.Fatal("bad config")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": cfg})
	}))
	defer engine.Close()
	s.Engine = engine.URL
	for _, p := range []any{map[string]any{"maxRunning": 17, "connections": 4}, map[string]any{"maxRunning": 1, "connections": 0}, map[string]any{"maxRunning": 1, "connections": 4, "script": map[string]any{"enable": true}}} {
		if adminCall(t, s, "gopeed.settings.set", p)["success"] != false {
			t.Fatal(p)
		}
	}
	if writes != 0 {
		t.Fatal("invalid config reached upstream")
	}
	out := adminCall(t, s, "gopeed.settings.set", Settings{MaxRunning: 3, Connections: 6, BTPort: DefaultBTPort, ED2KPort: DefaultED2KPort, ED2KUDPPort: DefaultED2KUDPPort})
	if out["success"] != true || writes != 1 || cfg["downloadDir"] != "private/staging" || cfg["script"].(map[string]any)["enable"] != false || cfg["archive"].(map[string]any)["autoExtract"] != false {
		t.Fatal(out, cfg)
	}
}

func TestOfficialPluginInstallStringIdentityIsDisabled(t *testing.T) {
	s, _, _ := fixture(t)
	disabled := false
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && r.URL.Path == "/api/v1/extensions":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": "gopeed@github-release"})
		case r.Method == "PUT" && r.URL.Path == "/api/v1/extensions/gopeed@github-release/switch":
			var body struct{ Status bool }
			json.NewDecoder(r.Body).Decode(&body)
			disabled = !body.Status
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": nil})
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer engine.Close()
	s.Engine = engine.URL
	out := adminCall(t, s, "gopeed.plugins.install", map[string]any{"url": "https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample", "consent": true})
	if out["success"] != true || !disabled {
		t.Fatal(out, "disabled", disabled)
	}
}

func TestPluginInstallRemovesExtensionItCannotDisable(t *testing.T) {
	for _, tc := range []struct{ name, identity string }{{"disable fails", "gopeed@github-release"}, {"bad identity", "bad id!"}} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _ := fixture(t)
			deleted := ""
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "POST" && r.URL.Path == "/api/v1/extensions":
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": tc.identity})
				case r.Method == "PUT":
					json.NewEncoder(w).Encode(map[string]any{"code": 1})
				case r.Method == "DELETE":
					deleted = r.URL.EscapedPath()
					json.NewEncoder(w).Encode(map[string]any{"code": 0})
				}
			}))
			defer engine.Close()
			s.Engine = engine.URL
			out := adminCall(t, s, "gopeed.plugins.install", map[string]any{"url": "https://github.com/GopeedLab/gopeed-extension-samples#github-release-sample", "consent": true})
			if out["success"] == true || deleted != "/api/v1/extensions/"+url.PathEscape(tc.identity) {
				t.Fatal(out, "deleted", deleted)
			}
		})
	}
}

func TestPluginListPassesOnlyGitHubRepositoryLinks(t *testing.T) {
	s, _, _ := fixture(t)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{
			map[string]any{"identity": "a@good", "repository": map[string]any{"url": "https://github.com/GopeedLab/gopeed-extension-samples"}},
			map[string]any{"identity": "a@script", "repository": map[string]any{"url": "javascript:void(0)"}},
			map[string]any{"identity": "a@other", "repository": map[string]any{"url": "https://example.com/x"}},
			map[string]any{"identity": "a@none"},
		}})
	}))
	defer engine.Close()
	s.Engine = engine.URL
	out := adminCall(t, s, "gopeed.plugins.list", map[string]any{})
	list, _ := out["data"].([]any)
	if len(list) != 4 {
		t.Fatal(out)
	}
	for i, want := range []bool{true, false, false, false} {
		ext := list[i].(map[string]any)
		if got := ext["repository"] != nil; got != want {
			t.Fatalf("%v: repository %v", ext["identity"], ext["repository"])
		}
	}
}

// a paused download does not hold an extension update; it resumes
// with the new version. A running one does, and a core update still waits
// for every unfinished download.
func TestExtensionUpdateWaitsOnlyForRunningDownloads(t *testing.T) {
	for status, want := range map[string]string{"pause": "", "running": "downloads_running"} {
		t.Run(status, func(t *testing.T) {
			s, _, _ := fixture(t)
			engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/tasks" {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []any{map[string]any{"status": status}}})
					return
				}
				json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"version": "1.0.0", "repository": map[string]any{"url": "https://github.com/GopeedLab/gopeed-extension-samples", "directory": "github-release-sample"}}})
			}))
			defer engine.Close()
			s.Engine = engine.URL
			// The supervisor's side of the request: it answers ready.
			done := make(chan struct{})
			defer close(done)
			go func() {
				for {
					var req binaries.Request
					if b, err := os.ReadFile(filepath.Join(s.Data, "engine-request.json")); err == nil && json.Unmarshal(b, &req) == nil {
						binaries.Atomic(filepath.Join(s.Data, "runtime.json"), binaries.Runtime{Request: req.ID, Ready: true})
						return
					}
					select {
					case <-done:
						return
					case <-time.After(10 * time.Millisecond):
					}
				}
			}()
			out := adminCall(t, s, "gopeed.plugins.update", map[string]any{"id": "gopeed@sample", "version": "1.1.0", "consent": true})
			refusal, _ := out["error"].(map[string]any)
			code, _ := refusal["code"].(string)
			if code != want {
				t.Fatal(out)
			}
			if status == "pause" {
				core := adminCall(t, s, "binaries.update", map[string]any{"version": "1.9.3", "sha256": strings.Repeat("a", 64), "consent": true})
				if core["error"].(map[string]any)["code"] != "downloads_active" {
					t.Fatal(core)
				}
			}
		})
	}
}
