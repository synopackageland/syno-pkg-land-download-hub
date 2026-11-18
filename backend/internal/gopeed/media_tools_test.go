package gopeed

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"dsmapp/internal/media"
)

// toolsSource is a GitHub that names one stable yt-dlp release.
type toolsSource struct {
	version string
	limited bool
	sums    string
}

// toolsFixture serves one yt-dlp release the way github.com does: a
// releases/latest redirect and the release's checksum file.
func toolsFixture(t *testing.T, s *Service) *toolsSource {
	src := &toolsSource{version: "2026.08.19", sums: strings.Repeat("ab", 32)}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case src.limited:
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "4102444800")
			w.WriteHeader(http.StatusForbidden)
		case r.URL.Path == "/yt-dlp/yt-dlp/releases/latest":
			http.Redirect(w, r, "/yt-dlp/yt-dlp/releases/tag/"+src.version, http.StatusFound)
		case r.URL.Path == "/yt-dlp/yt-dlp/releases/download/"+src.version+"/SHA2-256SUMS":
			fmt.Fprintf(w, "%s  yt-dlp_linux\n", src.sums)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	s.MediaTools = func() media.Tools {
		return media.Tools{Home: filepath.Join(s.Data, "media"), API: srv.URL, Web: srv.URL, Hosts: []string{u.Host}, Arch: "amd64", HTTP: srv.Client()}
	}
	return src
}

func errorOf(out map[string]any) (string, string) {
	e, _ := out["error"].(map[string]any)
	code, _ := e["code"].(string)
	message, _ := e["message"].(string)
	return code, message
}

func TestToolCheckSaysWhenGitHubLimitsQueries(t *testing.T) {
	s, _, _ := fixture(t)
	src := toolsFixture(t, s)
	src.limited = true
	code, message := errorOf(adminCall(t, s, "media.tools.check", map[string]string{"tool": "yt-dlp"}))
	if code != "tool_source_rate_limited" || !strings.Contains(message, "resets at 2100-01-01T00:00:00Z") {
		t.Fatal(code, message)
	}
	src.limited = false
	if code, _ = errorOf(adminCall(t, s, "media.tools.check", map[string]string{"tool": "yt-dlp"})); code != "tool_source_rate_limited" {
		t.Fatal("the limit is not shared until it resets:", code)
	}
}

func TestCoreCheckSaysWhenGitHubLimitsQueries(t *testing.T) {
	s, _, _ := fixture(t)
	s.coreHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "0")
		h.Set("X-RateLimit-Reset", "4102444800")
		return &http.Response{StatusCode: 403, Header: h, Body: http.NoBody}, nil
	})}
	code, message := errorOf(adminCall(t, s, "binaries.check", map[string]any{}))
	if code != "tool_source_rate_limited" || !strings.Contains(message, "resets at 2100-01-01T00:00:00Z") {
		t.Fatal(code, message)
	}
}

func TestToolCheckSeparatesAnUnverifiedReleaseFromAnUnreachableSource(t *testing.T) {
	s, _, _ := fixture(t)
	src := toolsFixture(t, s)
	src.sums = "not-a-digest"
	if code, _ := errorOf(adminCall(t, s, "media.tools.check", map[string]string{"tool": "yt-dlp"})); code != "tool_release_unverified" {
		t.Fatal(code)
	}
}

func TestMediaToolRoutesAreAdministratorOnly(t *testing.T) {
	s, _, _ := fixture(t)
	toolsFixture(t, s)
	for _, method := range []string{"media.tools.status", "media.tools.check", "media.tools.update", "media.tools.rollback"} {
		out, _ := call(t, s, "normal1", method, map[string]any{})
		if out["error"].(map[string]any)["code"] != "admin_required" {
			t.Fatal(method, out)
		}
	}
}

func TestMediaToolUpdateIsCheckedAndQueuedForTheDaemon(t *testing.T) {
	s, _, _ := fixture(t)
	src := toolsFixture(t, s)
	check := adminCall(t, s, "media.tools.check", map[string]string{"tool": "yt-dlp"})
	data := check["data"].(map[string]any)
	latest := data["latest"].(map[string]any)
	if data["update"] != true || latest["version"] != "2026.08.19" {
		t.Fatal(check)
	}
	update := func(v string, consent bool) map[string]any {
		return adminCall(t, s, "media.tools.update", map[string]any{"tool": "yt-dlp", "version": v, "sha256": latest["sha256"], "consent": consent})
	}
	if out := update("2026.08.19", false); out["error"].(map[string]any)["code"] != "consent_required" {
		t.Fatal(out)
	}
	// The update is checked against the shared release check, not a new call.
	src.version = "2026.09.01"
	if out := update("2026.09.01", true); out["error"].(map[string]any)["code"] != "release_changed" {
		t.Fatal(out)
	}
	first := update("2026.08.19", true)
	if first["success"] != true {
		t.Fatal(first)
	}
	status := adminCall(t, s, "media.tools.status", map[string]any{})["data"].(map[string]any)
	if status["pending"] != true || status["arch"] == "" {
		t.Fatal(status)
	}
	if out := update("2026.08.19", true); out["success"] != true || out["data"].(map[string]any)["request"] != first["data"].(map[string]any)["request"] {
		t.Fatal("duplicate request did not reuse admission", out)
	}
	if out := adminCall(t, s, "media.tools.rollback", map[string]any{"tool": "ffmpeg", "consent": true}); out["error"].(map[string]any)["code"] != "rollback_unavailable" {
		t.Fatal(out)
	}
	if out := adminCall(t, s, "media.tools.check", map[string]string{"tool": "sh"}); out["success"] == true {
		t.Fatal(out)
	}
}
