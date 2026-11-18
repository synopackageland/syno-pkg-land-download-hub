package gopeed

import (
	"context"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGenericClassifyExposesActualRoute(t *testing.T) {
	for _, tc := range []struct{ out, diag, kind, route, code string }{
		{`{"extractor_key":"Generic","direct":true,"formats":[{"protocol":"m3u8_native"}]}`, "", "media", "yt-dlp", ""},
		{`{"extractor_key":"Generic","direct":true,"url":"https://example.com/a.zip","protocol":"https"}`, "", "file", "gopeed", ""},
		{"", "ERROR: Unsupported URL", "file", "gopeed", ""},
		{"", "tool crashed", "file", "gopeed", ""},
		{"", "ERROR: This video is DRM protected", "media", "yt-dlp", "media_drm"},
	} {
		s, runs := mediaFixture(t, tc.out, tc.diag)
		result, err := s.classify(context.Background(), mustMarshal(map[string]any{"urls": []string{"https://example.com/a"}}))
		if err != nil {
			t.Fatal(err)
		}
		row := result.(map[string]any)["results"].([]map[string]string)[0]
		if row["kind"] != tc.kind || row["route"] != tc.route || row["code"] != tc.code || *runs != 1 {
			t.Fatalf("%v runs=%d", row, *runs)
		}
	}
}

func TestGenericProbeRefusesPrivateAndSkipsNonHTTP(t *testing.T) {
	s, runs := mediaFixture(t, bunny, "")
	for _, raw := range []string{"http://127.0.0.1/a", "https://[::1]/a", "https://192.168.1.1/a"} {
		if out := probe(t, s, "alice", raw); out["success"] == true {
			t.Fatal(out)
		}
	}
	for _, raw := range []string{"magnet:?xt=urn:btih:" + strings.Repeat("a", 40), ed2kLink, "https://example.com/a.torrent"} {
		probe(t, s, "alice", raw)
	}
	if *runs != 0 {
		t.Fatalf("probed blocked/protocol link %d times", *runs)
	}
}

func TestProbeUsesGuardAndRefusesPrivateRedirectEvenWithMediaOutput(t *testing.T) {
	s, _ := mediaFixture(t, bunny, "")
	s.MediaExec = func(ctx context.Context, _ string, args, _ []string, _ int64) ([]byte, []byte, error) {
		var proxy string
		for i, a := range args {
			if a == "--proxy" {
				proxy = args[i+1]
			}
		}
		if proxy == "" {
			t.Fatal("unguarded yt-dlp")
		}
		u, _ := url.Parse(proxy)
		tr := &http.Transport{Proxy: http.ProxyURL(u)}
		defer tr.CloseIdleConnections()
		req, _ := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1/redirect-target", nil)
		resp, err := (&http.Client{Transport: tr}).Do(req)
		if err == nil {
			resp.Body.Close()
		}
		return []byte(bunny), nil, nil
	}
	out := probe(t, s, "alice", "https://example.com/a")
	if out["success"] == true || out["error"].(map[string]any)["code"] != "url_denied" {
		t.Fatal(out)
	}
}

func TestDRMPreparationNeverFallsBackToManifest(t *testing.T) {
	s, runs := mediaFixture(t, "", "ERROR: This video is DRM protected")
	_, ids, err := s.createLink(context.Background(), api.Caller{}, nil, "uid:alice", "/home", "drm-fixture", "https://example.com/a.mpd")
	if errorCodeOf(err) != "media_drm" || len(ids) != 0 || *runs != 1 {
		t.Fatalf("ids=%v err=%v runs=%d", ids, err, *runs)
	}
	st, _ := s.readState()
	if len(st.Jobs) != 0 || len(st.Resolved) != 0 {
		t.Fatal("DRM admitted a job")
	}
}
