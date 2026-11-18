package media

import (
	"slices"
	"strings"
	"testing"
)

func value(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func TestCommonArgsIsolateUserConfiguration(t *testing.T) {
	env := Env{Home: "/var/pkg/media"}
	for _, args := range [][]string{ProbeArgs(env, "https://example.com/v"), DownloadArgs(env, Download{URL: "https://example.com/v", Format: "best", Output: "/stage/a", Control: "/var/pkg/media/jobs/a"})} {
		for _, flag := range []string{"--ignore-config", "--no-config-locations", "--no-plugin-dirs", "--no-update", "--no-exec", "--no-batch-file", "--no-cookies", "--no-cookies-from-browser", "--no-remote-components", "--no-js-runtimes"} {
			if !slices.Contains(args, flag) {
				t.Fatalf("missing %s in %v", flag, args)
			}
		}
		if got := value(args, "--cache-dir"); len(got) != 1 || got[0] != "/var/pkg/media/cache" {
			t.Fatalf("cache dir %v", got)
		}
		// The URL is the only positional argument and follows the option terminator.
		if args[len(args)-2] != "--" || args[len(args)-1] != "https://example.com/v" {
			t.Fatalf("url not terminated: %v", args)
		}
		for _, a := range args {
			if strings.HasPrefix(a, "--exec") && a != "--no-exec" || a == "--cookies" || a == "--config-locations" || a == "--load-info-json" {
				t.Fatalf("unsafe flag %s", a)
			}
		}
	}
}

func TestProbeArgsIncludeGenericExtractor(t *testing.T) {
	args := ProbeArgs(Env{Home: "/h"}, "https://example.com/file.zip")
	if got := value(args, "--use-extractors"); len(got) != 1 || got[0] != "default" {
		t.Fatalf("extractors %v", got)
	}
	for _, flag := range []string{"-J", "--flat-playlist", "--no-playlist", "--skip-download"} {
		if !slices.Contains(args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
}

func TestJSRuntimeOnlyFromManagedPath(t *testing.T) {
	args := ProbeArgs(Env{Home: "/h", Deno: "/h/tools/deno/1/deno"}, "https://example.com/v")
	if got := value(args, "--js-runtimes"); len(got) != 1 || got[0] != "deno:/h/tools/deno/1/deno" {
		t.Fatalf("runtime %v", got)
	}
	if slices.Index(args, "--no-js-runtimes") > slices.Index(args, "--js-runtimes") {
		t.Fatal("runtime must follow the clearing flag")
	}
}

func TestDownloadFormats(t *testing.T) {
	ff := Env{Home: "/h", FFmpegDir: "/h/tools/ffmpeg/1"}
	plain := Env{Home: "/h"}
	for _, tc := range []struct {
		env    Env
		format string
		want   string
		merge  bool
	}{
		{ff, "best", "bv*+ba/b", true},
		{ff, "1080", "bv*[height<=1080]+ba/b[height<=1080]/b", true},
		{ff, "audio", "ba/b", false},
		{plain, "best", "b", false},
		{plain, "720", "b[height<=720]/b", false},
		{plain, "audio", "ba[ext=m4a]/ba/b", false},
	} {
		args := DownloadArgs(tc.env, Download{URL: "https://e.com/v", Format: tc.format, Output: "/s", Control: "/c"})
		if got := value(args, "-f"); len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s %+v: %v", tc.format, tc.env, got)
		}
		if merge := value(args, "--merge-output-format"); (len(merge) == 1 && merge[0] == "mp4/mkv") != tc.merge {
			t.Fatalf("%s merge %v", tc.format, merge)
		}
		if tc.env.FFmpegDir != "" && value(args, "--ffmpeg-location")[0] != tc.env.FFmpegDir {
			t.Fatal("ffmpeg location")
		}
		if tc.env.FFmpegDir == "" && len(value(args, "--ffmpeg-location")) != 0 {
			t.Fatal("unmanaged ffmpeg")
		}
	}
	if !ValidFormat("1080") || ValidFormat("1081") || ValidFormat("bv*+ba") || ValidFormat("") {
		t.Fatal("format allowlist")
	}
	args := DownloadArgs(ff, Download{URL: "https://e.com/v", Format: "audio", Output: "/s", Control: "/c"})
	if !slices.Contains(args, "-x") || value(args, "--audio-format")[0] != "m4a" {
		t.Fatal("audio extraction")
	}
}

func TestDownloadPathsAndResume(t *testing.T) {
	args := DownloadArgs(Env{Home: "/h"}, Download{URL: "https://e.com/v", Format: "best", Output: "/stage/x", Control: "/h/jobs/x", MaxBytes: 1 << 30})
	paths := value(args, "-P")
	if !slices.Equal(paths, []string{"/stage/x", "temp:/h/jobs/x/tmp"}) {
		t.Fatalf("paths %v", paths)
	}
	if value(args, "--download-archive")[0] != "/h/jobs/x/archive.txt" {
		t.Fatal("archive")
	}
	if value(args, "--max-filesize")[0] != "1073741824" {
		t.Fatal("size")
	}
	if value(args, "-o")[0] != "%(title).150B [%(id)s].%(ext)s" {
		t.Fatal("template")
	}
	for _, flag := range []string{"--no-playlist", "--newline", "--windows-filenames", "--no-mtime", "--continue"} {
		if !slices.Contains(args, flag) {
			t.Fatalf("missing %s", flag)
		}
	}
	if !strings.HasPrefix(value(args, "--progress-template")[0], "download:"+ProgressPrefix) {
		t.Fatal("progress")
	}
}

func TestEnvironmentIsBuiltFromScratch(t *testing.T) {
	env := Environ("/h")
	for _, want := range []string{"HOME=/h/home", "XDG_CONFIG_HOME=/h/home/.config", "XDG_CACHE_HOME=/h/cache", "TMPDIR=/h/tmp", "PATH=/usr/bin:/bin", "LC_ALL=C.UTF-8", "PYTHONIOENCODING=utf-8"} {
		if !slices.Contains(env, want) {
			t.Fatalf("missing %s in %v", want, env)
		}
	}
	if len(env) != 7 {
		t.Fatalf("unexpected inherited environment %v", env)
	}
}

func TestPlaylistDownloadKeepsOrderAndBound(t *testing.T) {
	args := DownloadArgs(Env{Home: "/h"}, Download{URL: "https://e.com/p", Format: "best", Output: "/s", Control: "/c", Playlist: true})
	if slices.Contains(args, "--no-playlist") || !slices.Contains(args, "--yes-playlist") {
		t.Fatal("playlist mode")
	}
	if value(args, "--playlist-items")[0] != "1:256" || value(args, "-o")[0] != "%(playlist_index)03d - %(title).120B [%(id)s].%(ext)s" {
		t.Fatal(args)
	}
}

func TestParseProgress(t *testing.T) {
	for line, want := range map[string][3]int64{
		ProgressPrefix + " 100 400 NA 12.5":  {100, 400, 12},
		ProgressPrefix + " 100 NA 1000.7 NA": {100, 1000, 0},
		ProgressPrefix + " NA NA NA NA":      {0, 0, 0},
		"[download] Destination: x.mp4":      {-1},
		ProgressPrefix + " 1 2 3":            {-1},
		ProgressPrefix + " -5 2 3 4":         {-1},
	} {
		d, total, speed, ok := ParseProgress(line)
		if want[0] == -1 {
			if ok {
				t.Fatalf("%q accepted", line)
			}
			continue
		}
		if !ok || d != want[0] || total != want[1] || speed != want[2] {
			t.Fatalf("%q: %d %d %d %v", line, d, total, speed, ok)
		}
	}
}
