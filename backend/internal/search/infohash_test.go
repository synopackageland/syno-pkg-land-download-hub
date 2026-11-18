package search

import "testing"

func TestInfoHash(t *testing.T) {
	hex := "c12fe1c06bba254a9dc9f519b335aa7c1367a88a"
	for _, c := range []struct {
		hit  Hit
		want string
	}{
		{Hit{Hash: "C12FE1C06BBA254A9DC9F519B335AA7C1367A88A"}, hex},
		{Hit{Link: "magnet:?dn=x&xt=urn:btih:" + hex + "&tr=udp%3A%2F%2Ft"}, hex},
		{Hit{Link: "magnet:?xt=urn:btih:YEX6DQDLXISUVHOJ6UM3GNNKPQJWPKEK"}, hex},
		{Hit{Hash: "not-a-hash", Link: "https://a.example/x.torrent"}, ""},
		{Hit{Link: "magnet:?xt=urn:btmh:1220" + hex}, ""},
	} {
		if got := infoHash(c.hit); got != c.want {
			t.Errorf("infoHash(%+v) = %q, want %q", c.hit, got, c.want)
		}
	}
}
