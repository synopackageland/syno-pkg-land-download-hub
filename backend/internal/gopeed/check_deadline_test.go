package gopeed

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsmapp/internal/media"
)

// stall answers nothing until the test ends, like an unreachable GitHub.
func stall(t *testing.T) chan struct{} {
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })
	return done
}

func shortCheckDeadline(t *testing.T) {
	old := checkDeadline
	checkDeadline = 100 * time.Millisecond
	t.Cleanup(func() { checkDeadline = old })
}

func TestToolCheckGivesUpOnAStalledSource(t *testing.T) {
	s, _, _ := fixture(t)
	shortCheckDeadline(t)
	done := stall(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	s.MediaTools = func() media.Tools {
		return media.Tools{Home: filepath.Join(s.Data, "media"), API: srv.URL, Web: srv.URL, Hosts: []string{u.Host}, Arch: "amd64", HTTP: srv.Client()}
	}
	start := time.Now()
	out := adminCall(t, s, "media.tools.check", map[string]string{"tool": "yt-dlp"})
	if out["error"].(map[string]any)["code"] != "tool_source_unavailable" || time.Since(start) > 5*time.Second {
		t.Fatal(out, time.Since(start))
	}
}

func TestPluginCheckGivesUpOnAStalledSource(t *testing.T) {
	s, _, _ := fixture(t)
	shortCheckDeadline(t)
	done := stall(t)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/extensions/sample":
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"version": "1.0.0", "repository": map[string]any{"url": "https://github.com/GopeedLab/gopeed-extension-samples", "directory": "sample"}}})
		case "/api/v1/extensions/sample/update":
			select {
			case <-done:
			case <-r.Context().Done():
			}
		}
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	start := time.Now()
	out := adminCall(t, s, "gopeed.plugins.check", map[string]string{"id": "sample"})
	if out["success"] != false || time.Since(start) > 5*time.Second {
		t.Fatal(out, time.Since(start))
	}
}

func TestCoreCheckGivesUpOnAStalledSource(t *testing.T) {
	s, _, _ := fixture(t)
	shortCheckDeadline(t)
	done := stall(t)
	s.coreHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		select {
		case <-done:
		case <-r.Context().Done():
		}
		return nil, r.Context().Err()
	})}
	start := time.Now()
	out := adminCall(t, s, "binaries.check", map[string]any{})
	if out["error"].(map[string]any)["code"] != "tool_source_unavailable" || time.Since(start) > 5*time.Second {
		t.Fatal(out, time.Since(start))
	}
}

func TestCoreUpdateSaysTheSourceIsUnreachable(t *testing.T) {
	s, _, _ := fixture(t)
	idle := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	t.Cleanup(idle.Close)
	s.Engine = idle.URL
	s.coreHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("no route to GitHub")
	})}
	out := adminCall(t, s, "binaries.update", map[string]any{"version": "1.9.4", "sha256": strings.Repeat("a", 64), "consent": true})
	if out["error"].(map[string]any)["code"] != "tool_source_unavailable" {
		t.Fatal(out)
	}
}
