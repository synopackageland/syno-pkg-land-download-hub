package media

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func fake(stdout, stderr string, err error) Exec {
	return func(context.Context, string, []string, []string, int64) ([]byte, []byte, error) {
		return []byte(stdout), []byte(stderr), err
	}
}

func TestProbeSummarizesVideo(t *testing.T) {
	out := `{"_type":"video","extractor_key":"Youtube","webpage_url_domain":"youtube.com","title":"Big Buck Bunny","duration":635.2,"filesize_approx":743212891,
	"formats":[{"height":360,"vcodec":"avc1"},{"height":1080,"vcodec":"vp9"},{"height":1080,"vcodec":"avc1"},{"vcodec":"none","acodec":"opus"},{"height":4320,"vcodec":"none"}]}`
	info, err := Probe(context.Background(), fake(out, "", nil), Env{YtDlp: "/y", Home: "/h"}, "https://youtube.com/watch?v=x")
	if err != nil {
		t.Fatal(err)
	}
	if info.Kind != "media" || info.Title != "Big Buck Bunny" || info.Site != "youtube.com" || info.Extractor != "Youtube" || info.Duration != 635 || info.Size != 743212891 || info.Playlist {
		t.Fatalf("%+v", info)
	}
	if len(info.Heights) != 2 || info.Heights[0] != 1080 || info.Heights[1] != 360 || !info.Audio {
		t.Fatalf("heights %v audio %v", info.Heights, info.Audio)
	}
}

func TestProbePlaylistAndTitleBound(t *testing.T) {
	out := `{"_type":"playlist","extractor_key":"YoutubeTab","title":"` + strings.Repeat("é", 400) + `","entries":[{"id":"a"},{"id":"b"},{"id":"c"}]}`
	info, err := Probe(context.Background(), fake(out, "", nil), Env{YtDlp: "/y", Home: "/h"}, "https://youtube.com/playlist?list=x")
	if err != nil || !info.Playlist || info.Entries != 3 || len([]rune(info.Title)) != 200 {
		t.Fatalf("%+v %v", info, err)
	}
}

func TestProbeClassifiesFailures(t *testing.T) {
	failed := errors.New("exit status 1")
	for _, tc := range []struct{ stderr, code string }{
		{"ERROR: No suitable extractor found for URL https://e.com/a.zip", ""},
		{"ERROR: Unsupported URL: https://e.com/", ""},
		{"ERROR: [youtube] x: Sign in to confirm your age. This video may be inappropriate", "media_login_required"},
		{"ERROR: [twitter] 1: NSFW tweet requires authentication. Use --cookies", "media_login_required"},
		{"ERROR: [youtube] x: Private video. Sign in if you've been granted access", "media_login_required"},
		{"ERROR: [youtube] x: The uploader has not made this video available in your country", "media_geo_restricted"},
		{"ERROR: [youtube] x: Video unavailable. This video has been removed by the uploader", "media_unavailable"},
		{"ERROR: [generic] Unable to download webpage: HTTP Error 404: Not Found", "media_unavailable"},
		{"ERROR: something else", ""},
	} {
		info, err := Probe(context.Background(), fake("", tc.stderr, failed), Env{YtDlp: "/y", Home: "/h"}, "https://e.com/")
		if tc.code == "" {
			if err != nil || info.Kind != "http" {
				t.Fatalf("%q: %+v %v", tc.stderr, info, err)
			}
			continue
		}
		if Code(err) != tc.code {
			t.Fatalf("%q: got %v", tc.stderr, err)
		}
	}
	for _, run := range []Exec{fake("not json", "", nil), fake("", "", context.DeadlineExceeded), fake("", "", ErrOutputLimit)} {
		if info, err := Probe(context.Background(), run, Env{YtDlp: "/y", Home: "/h"}, "https://e.com/"); err != nil || info.Kind != "http" {
			t.Fatalf("%+v %v", info, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Probe(ctx, fake("", "", context.Canceled), Env{YtDlp: "/y"}, "https://e.com/"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestProbeWithoutToolIsHTTP(t *testing.T) {
	called := false
	run := func(context.Context, string, []string, []string, int64) ([]byte, []byte, error) {
		called = true
		return nil, nil, nil
	}
	info, err := Probe(context.Background(), run, Env{Home: "/h"}, "https://m.youtube.com/watch?v=x")
	if err != nil || info.Kind != "http" || !info.ToolMissing || called {
		t.Fatalf("%+v %v", info, err)
	}
}

// Only an address of a known media site is said to need the missing tool;
// any other address is an ordinary download.
func TestProbeWithoutToolReportsItOnlyForMediaSites(t *testing.T) {
	for url, missing := range map[string]bool{
		"https://youtu.be/x":                     true,
		"https://x.com/a/status/1":               true,
		"https://www.bilibili.com/video/BV1":     true,
		"https://github.com/jqlang/jq/releases/": false,
		"https://notyoutube.com/watch":           false,
		"https://e.com/":                         false,
	} {
		info, err := Probe(context.Background(), nil, Env{Home: "/h"}, url)
		if err != nil || info.Kind != "http" || info.ToolMissing != missing {
			t.Fatalf("%s: %+v %v", url, info, err)
		}
	}
}

func script(t *testing.T, body string) string {
	p := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRunBoundsOutputTimeAndEnvironment(t *testing.T) {
	ctx := context.Background()
	out, _, err := Run(ctx, script(t, `printf '%s|%s' "$HOME" "$1"`), []string{"--flag"}, []string{"HOME=/pkg"}, 1024)
	if err != nil || string(out) != "/pkg|--flag" {
		t.Fatalf("%q %v", out, err)
	}
	if _, _, err = Run(ctx, script(t, `head -c 5000 /dev/zero`), nil, nil, 1024); !errors.Is(err, ErrOutputLimit) {
		t.Fatalf("limit %v", err)
	}
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	// The child shell's own child must be stopped with the process group.
	if _, _, err = Run(short, script(t, `sleep 30 & wait`), nil, nil, 1024); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 5*time.Second {
		t.Fatalf("timeout %v after %v", err, time.Since(start))
	}
	_, stderr, err := Run(ctx, script(t, `head -c 200000 /dev/zero >&2; exit 3`), nil, nil, 1024)
	if err == nil || len(stderr) > StderrLimit {
		t.Fatalf("stderr %d %v", len(stderr), err)
	}
}

func TestStartStreamsLinesAndStopsTheGroup(t *testing.T) {
	var lines []string
	p, err := Start(script(t, `echo "$HOME"; echo two; echo 'ERROR: bad' >&2; exit 2`), nil, []string{"HOME=/pkg"}, func(l string) { lines = append(lines, l) })
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := p.Wait()
	if err == nil || len(lines) != 2 || lines[0] != "/pkg" || !strings.Contains(stderr, "ERROR: bad") {
		t.Fatalf("%v %v %q", err, lines, stderr)
	}
	p, _ = Start(script(t, `trap '' TERM; sleep 30 & wait; sleep 30`), nil, nil, func(string) {})
	start := time.Now()
	p.grace = 300 * time.Millisecond
	p.Stop()
	if _, err = p.Wait(); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("stop %v after %v", err, time.Since(start))
	}
}

func TestGenericProbeRouting(t *testing.T) {
	for _, tc := range []struct{ name, raw, kind string }{
		{"hls", `{"extractor_key":"Generic","direct":true,"formats":[{"url":"https://example.com/a.m3u8","protocol":"m3u8_native"}]}`, "media"},
		{"dash", `{"extractor_key":"Generic","direct":true,"formats":[{"url":"https://example.com/a.mpd","protocol":"http_dash_segments"}]}`, "media"},
		{"embedded", `{"extractor_key":"Generic","formats":[{"url":"https://cdn.example.com/a.mp4","protocol":"https"}]}`, "media"},
		{"zip", `{"extractor_key":"Generic","direct":true,"url":"https://example.com/a.zip","ext":"zip","protocol":"https"}`, "http"},
		{"mp4", `{"extractor_key":"Generic","direct":true,"formats":[{"url":"https://example.com/a.mp4","ext":"mp4","protocol":"https"}]}`, "http"},
		{"plain-format", `{"extractor_key":"Generic","formats":[{"url":"https://example.com/a","protocol":"https","ext":"pdf"}]}`, "http"},
		{"no-media", `{"extractor_key":"Generic","title":"nothing"}`, "http"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "https://example.com/a"
			if tc.name == "zip" || tc.name == "mp4" {
				source += "." + tc.name
			}
			info, err := Probe(context.Background(), fake(tc.raw, "", nil), Env{YtDlp: "/y", Home: "/h"}, source)
			if err != nil || info.Kind != tc.kind {
				t.Fatalf("%+v %v", info, err)
			}
		})
	}
}

func TestProbeCrashFallsBackAndDRMRefuses(t *testing.T) {
	for _, tc := range []struct{ out, diag, code string }{
		{"", "segmentation fault", ""},
		{"", "ERROR: Unsupported URL: https://example.com/drm-protected.zip", ""},
		{"", "ERROR: This video is DRM protected", "media_drm"},
		{`{"extractor_key":"Generic","has_drm":true}`, "", "media_drm"},
		{`{"extractor_key":"Generic","formats":[{"has_drm":true,"protocol":"m3u8_native"}]}`, "", "media_drm"},
	} {
		var failure error
		if tc.diag != "" {
			failure = errors.New("exit")
		}
		info, err := Probe(context.Background(), fake(tc.out, tc.diag, failure), Env{YtDlp: "/y"}, "https://example.com/a")
		if Code(err) != tc.code || (tc.code == "" && (err != nil || info.Kind != "http")) {
			t.Fatalf("%+v %v", info, err)
		}
	}
	run := func(ctx context.Context, _ string, _, _ []string, _ int64) ([]byte, []byte, error) {
		if _, ok := ctx.Deadline(); ok {
			t.Fatal("probe added a wall-clock deadline")
		}
		return []byte(`{"extractor_key":"Generic","direct":true}`), nil, nil
	}
	Probe(context.Background(), run, Env{YtDlp: "/y"}, "https://example.com/a")
}
