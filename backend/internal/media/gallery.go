package media

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

// GalleryArgs uses only package-controlled options. URL-derived ASCII names
// are bounded and stable across retries, even when post-local numbers repeat.
func GalleryArgs(env Env, d Download, probe bool) []string {
	args := []string{"--config-ignore", "--no-input", "--no-colors", "--http-timeout", "30", "--retries", "0",
		"-o", "cache.file=null", "-o", "cookies-update=false", "-o", "postprocessors=[]",
		"-o", "path-restrict=ascii", "-o", "path-strip=windows", "--range", "1-256", "--post-range", "1-256",
		"--filter", "extension in ('jpg', 'jpeg', 'png', 'gif', 'webp', 'avif', 'bmp', 'tif', 'tiff')"}
	if probe {
		args = append(args, "--dump-json", "--range", "1", "--post-range", "1")
	} else {
		args = append(args, "--directory", d.Output, "-o", "metadata-url=gdl_file_url", "--filename", "\fE hash_sha1(gdl_file_url) + '.' + extension[:16]", "--filesize-max", strconv.FormatInt(d.MaxBytes, 10), "-o", "skip=true")
	}
	if d.Cookies != "" {
		args = append(args, "--cookies", d.Cookies)
	}
	return append(args, "--", d.URL)
}

func ProbeGallery(ctx context.Context, run Exec, env Env, raw string) (Info, error) {
	out, stderr, err := run(ctx, env.GalleryDlp, GalleryArgs(env, Download{URL: raw, Control: filepath.Join(env.Home, "tmp")}, true), Environ(env.Home), probeMax)
	if ctx.Err() != nil {
		return Info{}, ctx.Err()
	}
	if err != nil {
		code := Classify(string(stderr))
		if code == "media_login_required" || code == "media_geo_restricted" || code == "media_drm" {
			return Info{}, &Error{code}
		}
		return Info{Kind: "http"}, nil
	}
	var messages [][]json.RawMessage
	if json.Unmarshal(out, &messages) != nil {
		return Info{Kind: "http"}, nil
	}
	u, _ := url.Parse(raw)
	title := u.Hostname()
	for _, message := range messages {
		var kind int
		if len(message) < 3 || json.Unmarshal(message[0], &kind) != nil || kind != 3 {
			continue
		}
		var metadata map[string]any
		_ = json.Unmarshal(message[2], &metadata)
		for _, key := range []string{"title", "description"} {
			if v, ok := metadata[key].(string); ok && strings.TrimSpace(v) != "" {
				title = v
				break
			}
		}
		return Info{Kind: "media", Tool: "gallery-dl", Title: bound(title, 150), Site: u.Hostname(), Extractor: "gallery-dl"}, nil
	}
	return Info{Kind: "http"}, nil
}
