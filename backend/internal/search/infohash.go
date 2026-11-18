package search

import (
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
)

var (
	hexHash    = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
	base32Hash = regexp.MustCompile(`^[A-Za-z2-7]{32}$`)
)

// infoHash names a hit's torrent in lower-case hex: the hash the site gave,
// else the one in its magnet link; "" when neither is a BitTorrent v1 hash.
func infoHash(h Hit) string {
	if v := normalHash(h.Hash); v != "" {
		return v
	}
	if !strings.HasPrefix(h.Link, "magnet:?") {
		return ""
	}
	q, err := url.ParseQuery(strings.TrimPrefix(h.Link, "magnet:?"))
	if err != nil {
		return ""
	}
	for _, xt := range q["xt"] {
		if len(xt) > 9 && strings.EqualFold(xt[:9], "urn:btih:") {
			if v := normalHash(xt[9:]); v != "" {
				return v
			}
		}
	}
	return ""
}

func normalHash(v string) string {
	switch {
	case hexHash.MatchString(v):
		return strings.ToLower(v)
	case base32Hash.MatchString(v):
		b, err := base32.StdEncoding.DecodeString(strings.ToUpper(v))
		if err != nil {
			return ""
		}
		return hex.EncodeToString(b)
	}
	return ""
}
