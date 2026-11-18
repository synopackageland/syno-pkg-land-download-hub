package gopeed

import (
	"bytes"
	"crypto/sha1"
	"encoding/base32"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
)

// maxTorrentBytes bounds uploaded and fetched .torrent metadata. It keeps the
// base64 upload inside the CGI's 1 MiB request body.
const maxTorrentBytes = 512 << 10

// bvalue is one parsed bencode value; raw keeps its exact encoding.
type bvalue struct {
	raw  []byte
	str  []byte
	num  int64
	list []bvalue
	dict map[string]bvalue
	kind byte // 's', 'i', 'l' or 'd'
}

type bdecoder struct {
	b     []byte
	pos   int
	depth int
}

func (d *bdecoder) value() (bvalue, bool) {
	start := d.pos
	if d.pos >= len(d.b) {
		return bvalue{}, false
	}
	var v bvalue
	switch c := d.b[d.pos]; {
	case c == 'i':
		end := bytes.IndexByte(d.b[d.pos:], 'e')
		if end < 2 {
			return v, false
		}
		n, err := strconv.ParseInt(string(d.b[d.pos+1:d.pos+end]), 10, 64)
		if err != nil {
			return v, false
		}
		v.kind, v.num = 'i', n
		d.pos += end + 1
	case c >= '0' && c <= '9':
		colon := bytes.IndexByte(d.b[d.pos:], ':')
		if colon < 1 {
			return v, false
		}
		n, err := strconv.Atoi(string(d.b[d.pos : d.pos+colon]))
		start := d.pos + colon + 1
		// Compare against the remaining bytes: start+n can overflow.
		if err != nil || n < 0 || n > len(d.b)-start {
			return v, false
		}
		v.kind, v.str = 's', d.b[start:start+n]
		d.pos = start + n
	case c == 'l' || c == 'd':
		if d.depth++; d.depth > 32 {
			return v, false
		}
		d.pos++
		v.kind = c
		if c == 'd' {
			v.dict = map[string]bvalue{}
		}
		for d.pos < len(d.b) && d.b[d.pos] != 'e' {
			if c == 'l' {
				e, ok := d.value()
				if !ok {
					return v, false
				}
				v.list = append(v.list, e)
				continue
			}
			k, ok := d.value()
			if !ok || k.kind != 's' {
				return v, false
			}
			e, ok := d.value()
			if _, dup := v.dict[string(k.str)]; !ok || dup {
				return v, false
			}
			v.dict[string(k.str)] = e
		}
		if d.pos >= len(d.b) {
			return v, false
		}
		d.pos++
		d.depth--
	default:
		return v, false
	}
	v.raw = d.b[start:d.pos]
	return v, true
}

// torrentInfoHash validates .torrent metadata and returns its BitTorrent v1
// info hash, the identity the engine reports as res.hash.
func torrentInfoHash(b []byte) (string, error) {
	if len(b) == 0 || len(b) > maxTorrentBytes {
		return "", refused("torrent_invalid")
	}
	d := bdecoder{b: b}
	top, ok := d.value()
	if !ok || d.pos != len(b) || top.kind != 'd' {
		return "", refused("torrent_invalid")
	}
	if !publicTorrentEndpoints(top) {
		return "", refused("url_denied")
	}
	info, ok := top.dict["info"]
	if !ok || info.kind != 'd' {
		return "", refused("torrent_invalid")
	}
	name, pieceLen, pieces := info.dict["name"], info.dict["piece length"], info.dict["pieces"]
	length, files := info.dict["length"], info.dict["files"]
	if name.kind != 's' || len(name.str) == 0 || pieceLen.kind != 'i' || pieceLen.num <= 0 || pieces.kind != 's' || len(pieces.str) == 0 || len(pieces.str)%20 != 0 || (length.kind != 'i' && files.kind != 'l') {
		return "", refused("torrent_invalid")
	}
	sum := sha1.Sum(info.raw)
	return hex.EncodeToString(sum[:]), nil
}

// magnetInfoHash returns the single v1 info hash of a magnet link in lower
// case hex. v2-only (btmh) links are not supported by this adapter.
func magnetInfoHash(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || !strings.EqualFold(u.Scheme, "magnet") {
		return "", refused("url_denied")
	}
	hash := ""
	for _, xt := range u.Query()["xt"] {
		if !strings.HasPrefix(strings.ToLower(xt), "urn:btih:") {
			continue
		}
		if hash != "" {
			return "", refused("url_denied")
		}
		hash = xt[len("urn:btih:"):]
	}
	var sum []byte
	switch len(hash) {
	case 40:
		sum, err = hex.DecodeString(hash)
	case 32:
		sum, err = base32.StdEncoding.DecodeString(strings.ToUpper(hash))
	default:
		err = refused("url_denied")
	}
	if err != nil || len(sum) != 20 {
		return "", refused("url_denied")
	}
	return hex.EncodeToString(sum), nil
}
