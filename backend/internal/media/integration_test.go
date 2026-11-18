package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRealYtDlp runs the installed tools against short Creative Commons videos.
// GOPEED_MEDIA_INTEGRATION=<dir holding yt-dlp, ffmpeg and deno> enables it.
func TestRealYtDlp(t *testing.T) {
	bin := os.Getenv("GOPEED_MEDIA_INTEGRATION")
	if bin == "" {
		t.Skip("set GOPEED_MEDIA_INTEGRATION to run against real yt-dlp")
	}
	home := t.TempDir()
	for _, d := range []string{"home", "cache", "tmp"} {
		os.MkdirAll(filepath.Join(home, d), 0700)
	}
	env := Env{YtDlp: filepath.Join(bin, "yt-dlp"), FFmpegDir: bin, Deno: filepath.Join(bin, "deno"), Home: home}
	ctx := context.Background()
	if info, err := Probe(ctx, Run, env, "https://example.com/file.zip"); err != nil || info.Kind != "http" {
		t.Fatalf("ordinary link: %+v %v", info, err)
	}
	for _, c := range []struct{ url, title, format string }{
		{"https://www.youtube.com/watch?v=SkVqJ1SGeL0", "Caminandes 3", "360"},
		{"https://commons.wikimedia.org/wiki/File:Caminandes-_Llama_Drama_-_Short_Movie.ogv", "Caminandes", "best"},
	} {
		start := time.Now()
		info, err := Probe(ctx, Run, env, c.url)
		if err != nil || info.Kind != "media" || !strings.Contains(info.Title, c.title) || info.Duration < 60 {
			t.Fatalf("%s: %+v %v", c.url, info, err)
		}
		t.Logf("probe %s: %q %.0fs size=%d heights=%v in %v", info.Extractor, info.Title, info.Duration, info.Size, info.Heights, time.Since(start).Round(time.Millisecond))
		out, control := t.TempDir(), t.TempDir()
		os.MkdirAll(filepath.Join(control, "tmp"), 0700)
		args := DownloadArgs(env, Download{URL: c.url, Format: c.format, Output: out, Control: control, MaxBytes: 1 << 30})
		run := func() (int, string, error) {
			progress := 0
			p, err := Start(env.YtDlp, args, Environ(home), func(l string) {
				if _, _, _, ok := ParseProgress(l); ok {
					progress++
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			stderr, err := p.Wait()
			return progress, stderr, err
		}
		start = time.Now()
		progress, stderr, err := run()
		files, _ := os.ReadDir(out)
		if err != nil || len(files) != 1 || progress == 0 {
			t.Fatalf("download: %v %v progress=%d\n%s", err, files, progress, stderr)
		}
		st, _ := files[0].Info()
		t.Logf("download %q %d bytes, %d progress lines in %v", files[0].Name(), st.Size(), progress, time.Since(start).Round(time.Millisecond))
		// The archive makes a resumed run skip completed items.
		progress, stderr, err = run()
		if err != nil || progress != 0 {
			t.Fatalf("rerun downloaded again: %v progress=%d\n%s", err, progress, stderr)
		}
	}
}
