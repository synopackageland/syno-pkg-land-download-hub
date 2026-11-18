package gopeed

import (
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// SourceKind selects the downloader for one pasted address or uploaded file.
// The download form has a single input; the backend, not the user, decides.
type SourceKind string

const (
	// SourceEngine is every address the Gopeed engine and its enabled
	// extensions resolve: HTTP(S), ED2K and any scheme an extension handles.
	SourceEngine      SourceKind = "engine"
	SourceMagnet      SourceKind = "magnet"
	SourceTorrentURL  SourceKind = "torrent-url"
	SourceTorrentFile SourceKind = "torrent-file"
	SourceMedia       SourceKind = "media"
	// SourceExtension marks a preview of an extension's resolve result.
	SourceExtension SourceKind = "extension"
)

// MediaDetector reports HTTP(S) addresses handled by the media downloader
// (YouTube, X and similar). A nil detector disables media routing.
type MediaDetector func(*url.URL) bool

const torrentDataPrefix = "data:application/x-bittorrent;base64,"

var sourceScheme = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]*):`)

// ClassifySource routes BT first (magnet, uploaded .torrent, *.torrent URL),
// then media, and leaves every other address to the engine, which decides
// with its protocols and enabled extensions whether it can download it.
// Addresses that would make the engine read local files are refused: the
// file and data schemes, scheme-less paths and any non-HTTP(S) address the
// engine's BT file filter (a .torrent suffix) would open from disk.
// A server Content-Type of application/x-bittorrent is recognized later, when
// the HTTP address is resolved; this function never contacts the network.
func ClassifySource(raw string, media MediaDetector) (SourceKind, error) {
	if len(raw) > 8<<10 && !strings.HasPrefix(raw, torrentDataPrefix) {
		return "", refused("url_denied")
	}
	if strings.HasPrefix(raw, torrentDataPrefix) {
		return SourceTorrentFile, nil
	}
	m := sourceScheme.FindStringSubmatch(raw)
	if m == nil {
		return "", refused("url_denied")
	}
	scheme := strings.ToLower(m[1])
	torrentSuffix := strings.HasSuffix(strings.ToLower(raw), ".torrent")
	switch scheme {
	case "file", "data":
		return "", refused("url_denied")
	case "magnet":
		u, err := url.Parse(raw)
		if err != nil {
			return "", refused("url_denied")
		}
		for _, xt := range u.Query()["xt"] {
			xt = strings.ToLower(xt)
			if strings.HasPrefix(xt, "urn:btih:") {
				return SourceMagnet, nil
			}
			if strings.HasPrefix(xt, "urn:ed2k:") && !torrentSuffix {
				return SourceEngine, nil
			}
		}
		return "", refused("url_denied")
	case "http", "https":
		u, err := url.Parse(raw)
		if err != nil {
			return "", refused("url_denied")
		}
		if strings.HasSuffix(strings.ToLower(u.Path), ".torrent") {
			return SourceTorrentURL, nil
		}
		if media != nil && media(u) {
			return SourceMedia, nil
		}
		return SourceEngine, nil
	}
	if torrentSuffix || (opaqueAuthority(raw, m[0]) == "" && !parses(raw)) {
		return "", refused("url_denied")
	}
	return SourceEngine, nil
}

// opaqueAuthority returns the "//|...|" part of links such as ED2K's, which
// are not RFC 3986 URLs and carry no host.
func opaqueAuthority(raw, scheme string) string {
	rest := strings.TrimPrefix(raw, scheme)
	if strings.HasPrefix(rest, "//|") {
		return rest
	}
	return ""
}

func parses(raw string) bool {
	_, err := url.Parse(raw)
	return err == nil
}

// safeSource is the network floor for addresses handed to the engine. Any
// host is checked like the HTTP downloader's: no loopback, private,
// link-local or unspecified literal and no localhost name. Credentials in
// the address are refused. DNS names that resolve to such addresses are
// not checked here; the engine dials them itself.
func safeSource(raw string) bool {
	m := sourceScheme.FindStringSubmatch(raw)
	if m == nil {
		return false
	}
	switch strings.ToLower(m[1]) {
	case "file", "data":
		return false
	case "http", "https":
		return safeURL(raw)
	}
	if rest := opaqueAuthority(raw, m[0]); rest != "" {
		return publicEd2kHosts(rest)
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return false
	}
	return u.Host == "" || publicHost(u.Hostname())
}

// publicEd2kHosts checks the hosts an ED2K link names: a server link's
// address and every peer of a file link's "sources," field.
func publicEd2kHosts(rest string) bool {
	fields := strings.Split(rest, "|")
	var hosts []string
	if len(fields) > 2 && strings.EqualFold(fields[1], "server") {
		hosts = append(hosts, fields[2])
	}
	for _, f := range fields {
		if list, ok := strings.CutPrefix(strings.ToLower(f), "sources,"); ok {
			hosts = append(hosts, strings.Split(list, ",")...)
		}
	}
	for _, h := range hosts {
		if host, _, err := net.SplitHostPort(h); err == nil {
			h = host
		}
		if !publicHost(h) {
			return false
		}
	}
	return true
}

func publicHost(h string) bool {
	return publicHostname(h)
}

var ed2kHash = regexp.MustCompile(`^[0-9A-Fa-f]{32}$`)

// engineAddress is the address the engine receives. The engine's ED2K
// protocol only reads ed2k:// links, so an eMule magnet with name, size and
// hash becomes the equivalent file link.
func engineAddress(raw string) (string, error) {
	if !strings.HasPrefix(strings.ToLower(raw), "magnet:") {
		return raw, nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", refused("url_denied")
	}
	q := u.Query()
	for _, xt := range q["xt"] {
		if !strings.HasPrefix(strings.ToLower(xt), "urn:ed2k:") {
			continue
		}
		hash := xt[len("urn:ed2k:"):]
		size, err := strconv.ParseInt(q.Get("xl"), 10, 64)
		name := q.Get("dn")
		if !ed2kHash.MatchString(hash) || err != nil || size <= 0 || !validName(name) || strings.Contains(name, "|") {
			return "", refused("ed2k_link_incomplete")
		}
		return "ed2k://|file|" + url.QueryEscape(name) + "|" + strconv.FormatInt(size, 10) + "|" + strings.ToUpper(hash) + "|/", nil
	}
	return raw, nil
}
