package gopeed

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"
)

// bencode encodes test fixtures: strings, ints, lists and sorted dicts.
func bencode(v any) string {
	switch x := v.(type) {
	case string:
		return fmt.Sprintf("%d:%s", len(x), x)
	case int:
		return fmt.Sprintf("i%de", x)
	case []any:
		var b strings.Builder
		b.WriteString("l")
		for _, e := range x {
			b.WriteString(bencode(e))
		}
		return b.String() + "e"
	case map[string]any:
		keys := []string{}
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("d")
		for _, k := range keys {
			b.WriteString(bencode(k) + bencode(x[k]))
		}
		return b.String() + "e"
	}
	panic(v)
}

// fixtureTorrent is a tracker-less two-file torrent; no network is used.
func fixtureTorrent() (string, string) {
	info := map[string]any{"name": "Album", "piece length": 16384, "pieces": strings.Repeat("x", 20), "files": []any{
		map[string]any{"length": 3, "path": []any{"cd1", "a.flac"}},
		map[string]any{"length": 4, "path": []any{"cover.jpg"}},
	}}
	sum := sha1.Sum([]byte(bencode(info)))
	return bencode(map[string]any{"info": info, "created by": "regression fixture"}), hex.EncodeToString(sum[:])
}

func TestTorrentInfoHashIsSHA1OfTheRawInfoDictionary(t *testing.T) {
	raw, want := fixtureTorrent()
	got, err := torrentInfoHash([]byte(raw))
	if err != nil || got != want {
		t.Fatalf("%q %v, want %q", got, err, want)
	}
}

func TestTorrentInfoHashRefusesMalformedMetadata(t *testing.T) {
	raw, _ := fixtureTorrent()
	for _, bad := range []string{"", "de", "d4:infoi1ee", raw[:len(raw)-1], raw + "x", "l" + raw + "e", "d4:info" + bencode(map[string]any{"name": "x"}) + "e", strings.Repeat("l", 100) + strings.Repeat("e", 100), "d4:info" + bencode(map[string]any{"name": "x", "piece length": 1, "pieces": "x"}) + "e", "d3:abc1:x3:abd1:y4:infoe"} {
		if _, err := torrentInfoHash([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := torrentInfoHash([]byte("d4:info" + strings.Repeat("x", maxTorrentBytes) + "e")); err == nil {
		t.Fatal("accepted oversized torrent")
	}
}

func TestTorrentInfoHashRefusesHugeStringLengths(t *testing.T) {
	for _, bad := range []string{"d4:info9223372036854775800:e", "d4:info9223372036854775807:xe", "d9223372036854775807:xe"} {
		if _, err := torrentInfoHash([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestMagnetInfoHashAcceptsHexAndBase32(t *testing.T) {
	for raw, want := range map[string]string{
		"magnet:?xt=urn:btih:0123456789ABCDEF0123456789abcdef01234567&dn=x": "0123456789abcdef0123456789abcdef01234567",
		"magnet:?xt=urn:btih:AERUKZ4JVPG66AJDIVTYTK6N54ASGRLH":              "0123456789abcdef0123456789abcdef01234567",
	} {
		got, err := magnetInfoHash(raw)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v", raw, got, err)
		}
	}
	for _, bad := range []string{"magnet:?xt=urn:btih:xyz", "magnet:?xt=urn:btmh:1220abcd", "magnet:?dn=x", "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&xt=urn:btih:1123456789abcdef0123456789abcdef01234567"} {
		if _, err := magnetInfoHash(bad); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}
