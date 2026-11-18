// Package media runs yt-dlp from the package account for supported media
// sites. Every argument is built here; callers never supply yt-dlp options.
package media

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Env names the managed tool paths and the package-owned media home.
// FFmpegDir and Deno are empty when the administrator has not installed them.
type Env struct {
	YtDlp, GalleryDlp, FFmpegDir, Deno, Home string
}

// Download is one owner job: Output is its private staging folder and
// Control its package-owned archive, temp and progress folder.
type Download struct {
	URL, Format, Output, Control string
	MaxBytes                     int64
	Options                      Options
	Cookies                      string
	Archive                      string
	Playlist                     bool
}

const ProgressPrefix = "gopeed-progress"

var formatRE = regexp.MustCompile(`^(best|audio|2160|1440|1080|720|480|360)$`)

func ValidFormat(f string) bool { return formatRE.MatchString(f) }

// Configuration, plugins, cookies, --exec hooks and remote components can run
// or read data outside the package; clear each explicitly before adding ours.
func common(env Env) []string {
	args := []string{"--ignore-config", "--no-config-locations", "--no-plugin-dirs", "--no-update", "--no-exec", "--no-batch-file", "--no-cookies", "--no-cookies-from-browser", "--no-remote-components", "--no-js-runtimes"}
	if env.Deno != "" {
		args = append(args, "--js-runtimes", "deno:"+env.Deno)
	}
	return append(args, "--cache-dir", filepath.Join(env.Home, "cache"), "--socket-timeout", "30", "--no-color")
}

// ProbeArgs lets site and generic extractors inspect a URL without downloading it.
func ProbeArgs(env Env, url string) []string {
	args := append(common(env), "--use-extractors", "default", "--retries", "0", "--extractor-retries", "0", "-J", "--flat-playlist", "--no-playlist", "--skip-download")
	return append(args, "--", url)
}

func DownloadArgs(env Env, d Download) []string {
	merge := env.FFmpegDir != ""
	o := d.Options
	o.Format = d.Format
	if o.AudioFormat != "" {
		d.Format = "audio"
	}
	args := append(common(env), "-f", selector(d.Format, merge))
	if merge {
		args = append(args, "--ffmpeg-location", env.FFmpegDir)
		if d.Format == "audio" {
			audio := o.AudioFormat
			if audio == "" {
				audio = "m4a"
			}
			args = append(args, "-x", "--audio-format", audio)
		} else {
			args = append(args, "--merge-output-format", "mp4/mkv")
		}
	}
	if d.MaxBytes > 0 {
		args = append(args, "--max-filesize", strconv.FormatInt(d.MaxBytes, 10))
	}
	args = append(args, optionArgs(o)...)
	if d.Cookies != "" {
		args = append(args, "--cookies", d.Cookies)
	}
	archive := d.Archive
	if archive == "" {
		archive = filepath.Join(d.Control, "archive.txt")
	}
	output := "%(title).150B [%(id)s].%(ext)s"
	if d.Playlist {
		// Bounded by multisave.MaxFiles; the index keeps playlist order.
		args = append(args, "--yes-playlist", "--playlist-items", "1:256")
		output = "%(playlist_index)03d - %(title).120B [%(id)s].%(ext)s"
	} else {
		args = append(args, "--no-playlist")
	}
	args = append(args, "--newline", "--continue", "--no-mtime", "--windows-filenames",
		"--progress-template", "download:"+ProgressPrefix+" %(progress.downloaded_bytes)s %(progress.total_bytes)s %(progress.total_bytes_estimate)s %(progress.speed)s",
		"-P", d.Output, "-P", "temp:"+filepath.Join(d.Control, "tmp"),
		"-o", output,
		"--download-archive", archive)
	return append(args, "--", d.URL)
}

func selector(format string, merge bool) string {
	switch {
	case format == "audio" && merge:
		return "ba/b"
	case format == "audio":
		return "ba[ext=m4a]/ba/b"
	case format == "best" && merge:
		return "bv*+ba/b"
	case format == "best":
		return "b"
	case merge:
		return "bv*[height<=" + format + "]+ba/b[height<=" + format + "]/b"
	default:
		return "b[height<=" + format + "]/b"
	}
}

// Environ never inherits the daemon environment: proxies, PYTHONPATH and the
// caller's HOME could change which code or configuration yt-dlp loads.
func Environ(home string) []string {
	return []string{"HOME=" + filepath.Join(home, "home"), "XDG_CONFIG_HOME=" + filepath.Join(home, "home", ".config"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"), "TMPDIR=" + filepath.Join(home, "tmp"), "PATH=/usr/bin:/bin", "LC_ALL=C.UTF-8", "PYTHONIOENCODING=utf-8"}
}

// PrepareHome creates the private directories that Environ points to.
func PrepareHome(home string) error {
	for _, dir := range []string{"home", "cache", "tmp"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0700); err != nil {
			return err
		}
	}
	return nil
}

// ParseProgress reads one --progress-template line. yt-dlp prints NA for
// unknown values; a missing total uses the estimate.
func ParseProgress(line string) (downloaded, total, speed int64, ok bool) {
	f := strings.Fields(line)
	if len(f) != 5 || f[0] != ProgressPrefix {
		return 0, 0, 0, false
	}
	var v [4]int64
	for i, s := range f[1:] {
		if s == "NA" || s == "None" {
			continue
		}
		n, err := strconv.ParseFloat(s, 64)
		if err != nil || n < 0 || n > 1<<50 {
			return 0, 0, 0, false
		}
		v[i] = int64(n)
	}
	total = v[1]
	if total == 0 {
		total = v[2]
	}
	return v[0], total, v[3], true
}

// SearchArgs uses the same closed tool configuration as probes and downloads.
// Page n selects 100 entries from the provider's first n*100 results.
func SearchArgs(env Env, provider, query string, page int) []string {
	args := append(common(env), "--flat-playlist", "--dump-json", "--skip-download", "--no-warnings", "--playlist-items", strconv.Itoa((page-1)*100+1)+":"+strconv.Itoa(page*100))
	return append(args, "--", provider+strconv.Itoa(page*100)+":"+query)
}
