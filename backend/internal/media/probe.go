package media

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
)

// Error carries a stable code for the UI; tool diagnostics are not forwarded.
type Error struct{ Code string }

func (e *Error) Error() string { return "media: " + e.Code }

func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Info is what the user confirms before a media download is created.
type Info struct {
	Tool        string  `json:"tool,omitempty"`
	Kind        string  `json:"kind"`
	ToolMissing bool    `json:"toolMissing,omitempty"`
	Title       string  `json:"title,omitempty"`
	ChannelURL  string  `json:"channelUrl,omitempty"`
	Site        string  `json:"site,omitempty"`
	Extractor   string  `json:"extractor,omitempty"`
	Duration    float64 `json:"duration,omitempty"`
	Size        int64   `json:"size,omitempty"`
	Playlist    bool    `json:"playlist,omitempty"`
	Entries     int     `json:"entries,omitempty"`
	Heights     []int   `json:"heights,omitempty"`
	Audio       bool    `json:"audio,omitempty"`
}

const probeMax = 16 << 20

// mediaSites are popular sites whose pages need yt-dlp. Without the tool,
// only their addresses are reported as needing it; others are plain downloads.
var mediaSites = []string{
	"youtube.com", "youtu.be", "x.com", "twitter.com", "vimeo.com", "dailymotion.com",
	"twitch.tv", "tiktok.com", "instagram.com", "facebook.com", "fb.watch",
	"bilibili.com", "b23.tv", "nicovideo.jp", "soundcloud.com",
}

// KnownMediaSite reports an address on one of mediaSites or its subdomains.
func KnownMediaSite(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	host := strings.ToLower(u.Hostname())
	for _, site := range mediaSites {
		if host == site || strings.HasSuffix(host, "."+site) {
			return true
		}
	}
	return false
}

func Probe(ctx context.Context, run Exec, env Env, url string) (Info, error) {
	info, err := probeYtDlp(ctx, run, env, url)
	if err != nil || info.Kind == "media" || env.GalleryDlp == "" {
		return info, err
	}
	gallery, err := ProbeGallery(ctx, run, env, url)
	if err != nil || gallery.Kind == "media" {
		return gallery, err
	}
	return info, nil
}

func probeYtDlp(ctx context.Context, run Exec, env Env, url string) (Info, error) {
	if env.YtDlp == "" {
		return Info{Kind: "http", ToolMissing: KnownMediaSite(url)}, nil
	}
	stdout, stderr, err := run(ctx, env.YtDlp, ProbeArgs(env, url), Environ(env.Home), probeMax)
	if ctx.Err() != nil {
		return Info{}, ctx.Err()
	}
	if err != nil {
		code := Classify(string(stderr))
		// Ordinary extraction failures never turn an HTTP download into a refusal.
		// Keep recognized content restrictions visible; DRM must never fetch a manifest.
		switch code {
		case "media_drm", "media_login_required", "media_geo_restricted", "media_unavailable", "media_too_large":
			return Info{}, &Error{code}
		}
		return Info{Kind: "http"}, nil
	}
	info, err := summarizeURL(stdout, url)
	if Code(err) == "media_failed" {
		return Info{Kind: "http"}, nil
	}
	return info, err
}

// Classify maps yt-dlp's final diagnostics to a UI reason. An empty code means
// no site extractor claimed the URL, so the generic HTTP download applies.
func Classify(stderr string) string {
	s := strings.ToLower(stderr)
	// URL paths and query strings are not tool refusal reasons. In particular,
	// an unsupported file named drm-protected.zip must still use Gopeed.
	diagnostic := s
	for _, field := range strings.Fields(s) {
		if strings.Contains(field, "://") {
			diagnostic = strings.ReplaceAll(diagnostic, field, "")
		}
	}
	switch {
	case containsAny(diagnostic, "drm protected", "drm-protected", "drm encrypted", "drm-encrypted", "drm protection", "drm detected", "digital rights management"):
		return "media_drm"
	case strings.Contains(s, "no suitable extractor") || strings.Contains(s, "unsupported url"):
		return ""
	case containsAny(s, "sign in", "log in", "login", "cookies", "authentication", "private video", "members-only"):
		return "media_login_required"
	case containsAny(s, "your country", "geo restrict", "geo-restrict"):
		return "media_geo_restricted"
	case containsAny(s, "unavailable", "has been removed", "http error 404", "does not exist"):
		return "media_unavailable"
	case containsAny(s, "max-filesize", "larger than max"):
		return "media_too_large"
	}
	return "media_failed"
}

func containsAny(s string, parts ...string) bool {
	for _, p := range parts {
		if strings.Contains(s, p) {
			return true
		}
	}
	return false
}

func summarizeURL(raw []byte, source string) (Info, error) {
	var in struct {
		Direct        bool     `json:"direct"`
		URL           string   `json:"url"`
		Protocol      string   `json:"protocol"`
		Live          bool     `json:"is_live"`
		LiveStatus    string   `json:"live_status"`
		DRM           any      `json:"has_drm"`
		ExtractorName string   `json:"extractor"`
		Type          string   `json:"_type"`
		Extractor     string   `json:"extractor_key"`
		Domain        string   `json:"webpage_url_domain"`
		Title         string   `json:"title"`
		ChannelURL    string   `json:"channel_url"`
		Duration      float64  `json:"duration"`
		Size          int64    `json:"filesize"`
		Approx        float64  `json:"filesize_approx"`
		PlaylistCount int      `json:"playlist_count"`
		Entries       []any    `json:"entries"`
		Formats       []format `json:"formats"`
	}
	if json.Unmarshal(raw, &in) != nil || (in.Type != "" && in.Type != "video" && in.Type != "playlist") {
		return Info{}, &Error{"media_failed"}
	}
	if in.Live || in.LiveStatus == "is_live" || in.LiveStatus == "is_upcoming" {
		return Info{}, &Error{"media_live"}
	}
	if drmProtected(in.DRM) {
		return Info{}, &Error{"media_drm"}
	}
	for _, f := range in.Formats {
		if drmProtected(f.DRM) {
			return Info{}, &Error{"media_drm"}
		}
	}
	generic := strings.EqualFold(in.Extractor, "generic") || strings.EqualFold(in.ExtractorName, "generic")
	if generic && in.Type != "playlist" {
		stream := streamProtocol(in.Protocol)
		for _, f := range in.Formats {
			stream = stream || streamProtocol(f.Protocol)
		}
		// A manifest remains media even when Generic labels its input "direct".
		// Embedded HTTP media has a different URL from the input page.
		plain := len(in.Formats) == 1 && in.Formats[0].URL == source && plainProtocol(in.Formats[0].Protocol)
		if !stream && (in.Direct || plain || (len(in.Formats) == 0 && in.URL == "")) {
			return Info{Kind: "http"}, nil
		}
	}
	info := Info{Kind: "media", ChannelURL: bound(in.ChannelURL, 2048), Title: bound(in.Title, 200), Site: bound(in.Domain, 100), Extractor: bound(in.Extractor, 64), Duration: float64(int64(in.Duration)), Size: in.Size}
	if info.Size == 0 && in.Approx > 0 {
		info.Size = int64(in.Approx)
	}
	if in.Type == "playlist" {
		info.Playlist, info.Entries = true, len(in.Entries)
		if info.Entries == 0 {
			info.Entries = in.PlaylistCount
		}
		return info, nil
	}
	seen := map[int]bool{}
	for _, f := range in.Formats {
		if f.VCodec != "none" && f.Height > 0 && f.Height <= 4320 && !seen[f.Height] {
			seen[f.Height] = true
			info.Heights = append(info.Heights, f.Height)
		}
		if f.ACodec != "" && f.ACodec != "none" {
			info.Audio = true
		}
	}
	sort.Sort(sort.Reverse(sort.IntSlice(info.Heights)))
	return info, nil
}

func drmProtected(v any) bool { return v == true || v == "true" }

func streamProtocol(p string) bool {
	return strings.Contains(p, "m3u8") || strings.Contains(p, "dash") || strings.Contains(p, "f4m") || strings.Contains(p, "ism")
}
func plainProtocol(p string) bool { return p == "http" || p == "https" }

type format struct {
	URL      string `json:"url"`
	Protocol string `json:"protocol"`
	DRM      any    `json:"has_drm"`
	Height   int    `json:"height"`
	VCodec   string `json:"vcodec"`
	ACodec   string `json:"acodec"`
}

func bound(s string, n int) string {
	r := []rune(strings.ToValidUTF8(s, ""))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}
