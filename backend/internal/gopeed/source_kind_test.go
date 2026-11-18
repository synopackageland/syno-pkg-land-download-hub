package gopeed

import (
	"net/url"
	"testing"
)

const ed2kLink = "ed2k://|file|ubuntu.iso|1048576|0123456789ABCDEF0123456789ABCDEF|/"

func TestClassifySourceRoutesEachAddressToOneDownloader(t *testing.T) {
	media := func(u *url.URL) bool { return u.Hostname() == "video.example" }
	for raw, want := range map[string]SourceKind{
		"magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x":  SourceMagnet,
		"MAGNET:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567":       SourceMagnet,
		"https://example.com/dist/ubuntu.iso.torrent":                        SourceTorrentURL,
		"https://example.com/dist/UBUNTU.TORRENT?sig=1":                      SourceTorrentURL,
		"data:application/x-bittorrent;base64,ZGU=":                          SourceTorrentFile,
		"https://video.example/watch?v=1":                                    SourceMedia,
		"https://example.com/file.zip":                                       SourceEngine,
		ed2kLink:                                                             SourceEngine,
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&xl=1&dn=a.iso": SourceEngine,
		"ftp://example.com/pub/file.zip":                                     SourceEngine,
		"pan-share:abc123":                                                   SourceEngine,
	} {
		got, err := ClassifySource(raw, media)
		if err != nil || got != want {
			t.Fatalf("%s: %v %v, want %v", raw, got, err, want)
		}
	}
	if got, err := ClassifySource("https://video.example/watch", nil); err != nil || got != SourceEngine {
		t.Fatalf("nil media detector: %v %v", got, err)
	}
}

// The engine opens local files for *.torrent addresses and could read other
// local or loopback resources; those stay refused for every scheme.
func TestClassifySourceRefusesLocalAndPrivateAddresses(t *testing.T) {
	for _, raw := range []string{
		"", "file:///etc/passwd", "FILE:/var/packages/gopeed/var/engine.token",
		"data:text/plain;base64,eA==", "DATA:application/x-bittorrent;base64,ZGU=",
		"/var/packages/gopeed/var/torrents/x.torrent", "torrents/x.torrent", "x.torrent",
		"ftp://example.com/a.torrent", "pan-share:/var/packages/gopeed/var/a.TORRENT", "http://[bad",
		"magnet:?dn=only-name", "magnet:?xt=urn:btmh:1220abcd",
	} {
		if kind, err := ClassifySource(raw, nil); err == nil {
			t.Fatalf("accepted %q as %v", raw, kind)
		}
	}
}

// Host names are checked as before for every scheme that carries one.
func TestSafeSourceRefusesLoopbackPrivateAndCredentials(t *testing.T) {
	for _, raw := range []string{
		"http://localhost/x", "https://a.localhost/x", "https://127.0.0.1/x", "http://10.0.0.1/x",
		"ftp://192.168.1.2/x", "ftp://[::1]/x", "sftp://169.254.1.1/x", "ftp://0.0.0.0/x",
		"ftp://user:pw@example.com/x", "https://user@example.com/x", "file:///etc/passwd", "x.torrent",
		"http://[::1%25lo]:5000/x", "https://[fe80::1%25eth0]/x", "ftp://[::1%25lo]/x",
	} {
		if safeSource(raw) {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"https://example.com/a.zip", "ftp://example.com/a.zip", ed2kLink, "pan-share:abc123"} {
		if !safeSource(raw) {
			t.Fatalf("refused %q", raw)
		}
	}
}

// ED2K links may name peers and servers; non-public ones are refused like
// any other host, in case a later engine starts dialing them.
func TestEd2kSourcesAndServersMustBePublic(t *testing.T) {
	file := "ed2k://|file|a.iso|1048576|0123456789ABCDEF0123456789ABCDEF|/"
	for _, raw := range []string{
		file + "|sources,192.168.1.2:4662|/",
		file + "|sources,203.0.113.7:4662,127.0.0.1:4662|/",
		file + "|sources,[::1]:4662|/",
		file + "|sources,localhost:4662|/",
		"ed2k://|server|10.0.0.1|4661|/",
		"ed2k://|server|169.254.1.1|4661|/",
	} {
		if safeSource(raw) {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{
		file + "|sources,203.0.113.7:4662,peer.example.net:4662|/",
		file + "|h=ABCDEFGHIJKLMNOPQRSTUVWXYZ234567|/",
		"ed2k://|server|203.0.113.9|4661|/",
	} {
		if !safeSource(raw) {
			t.Fatalf("refused %q", raw)
		}
	}
}

func TestEngineAddressTurnsEd2kMagnetsIntoEd2kLinks(t *testing.T) {
	for raw, want := range map[string]string{
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&xl=1048576&dn=ubuntu.iso": "ed2k://|file|ubuntu.iso|1048576|0123456789ABCDEF0123456789ABCDEF|/",
		"magnet:?dn=My+Album.zip&xt=urn:ed2k:0123456789ABCDEF0123456789ABCDEF&xl=7":     "ed2k://|file|My+Album.zip|7|0123456789ABCDEF0123456789ABCDEF|/",
		ed2kLink:                       ed2kLink,
		"https://example.com/file.zip": "https://example.com/file.zip",
	} {
		got, err := engineAddress(raw)
		if err != nil || got != want {
			t.Fatalf("%s: %q %v, want %q", raw, got, err, want)
		}
	}
	for _, raw := range []string{
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&dn=a.iso",
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&xl=1",
		"magnet:?xt=urn:ed2k:0123&xl=1&dn=a.iso",
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&xl=0&dn=a.iso",
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&xl=1&dn=a%7Cb",
	} {
		if got, err := engineAddress(raw); err == nil {
			t.Fatalf("converted %q to %q", raw, got)
		}
	}
}
