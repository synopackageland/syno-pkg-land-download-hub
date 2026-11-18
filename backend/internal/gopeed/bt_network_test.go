package gopeed

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestBTMetadataRejectsNonPublicTrackersAndWebSeeds(t *testing.T) {
	raw, _ := fixtureTorrent()
	for _, key := range []string{"announce", "announce-list", "url-list", "httpseeds"} {
		for _, address := range []string{"http://127.0.0.1/announce", "https://[::ffff:169.254.169.254]/metadata", "http://localhost./seed", "http://100.64.0.1/seed"} {
			var value any = address
			if key == "announce-list" {
				value = []any{[]any{address}}
			}
			if key == "httpseeds" {
				value = []any{address}
			}
			metadata := "d" + bencode(key) + bencode(value) + raw[1:]
			if _, err := torrentInfoHash([]byte(metadata)); err == nil {
				t.Errorf("%s admitted %s", key, address)
			}
		}
	}
	metadata := "d" + bencode("announce") + bencode("udp://tracker.example:6969/announce") + bencode("url-list") + bencode([]any{"https://seed.example/file"}) + raw[1:]
	if _, err := torrentInfoHash([]byte(metadata)); err != nil {
		t.Fatal("public endpoints refused", err)
	}
}

func TestMagnetRejectsNonPublicAuxiliaryEndpoints(t *testing.T) {
	s := New(t.TempDir())
	base := "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)
	for _, key := range []string{"tr", "ws", "xs", "as", "x.pe"} {
		address := "http://127.0.0.1/private"
		if key == "x.pe" {
			address = "127.0.0.1:6881"
		}
		if _, err := s.btSource(context.Background(), SourceMagnet, base+"&"+key+"="+url.QueryEscape(address)); err == nil {
			t.Errorf("magnet %s reached the engine", key)
		}
	}
	if _, err := s.btSource(context.Background(), SourceMagnet, base+"&tr="+url.QueryEscape("udp://tracker.example:6969/announce")); err != nil {
		t.Fatal(err)
	}
}
