package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mediactl/clustarr/pkg/newznab"
	"github.com/mediactl/clustarr/pkg/torznab"
)

func TestReleaseMetadata(t *testing.T) {
	seed, peers, zero := int32(10), int32(13), int32(0)
	for _, tc := range []struct {
		cats []newznab.CategoryID
		want string
	}{{nil, ""}, {[]newznab.CategoryID{2000}, "movie"}, {[]newznab.CategoryID{5040}, "series"}, {[]newznab.CategoryID{2000, 5070}, "anime"}, {[]newznab.CategoryID{3000}, "music"}, {[]newznab.CategoryID{4000}, "software"}, {[]newznab.CategoryID{7000}, "book"}, {[]newznab.CategoryID{8000}, "other"}} {
		r := mapReleases([]torznab.Release{{Title: "X", Link: "magnet:?xt=x", Seeders: &seed, Peers: &peers, Categories: tc.cats}}).Results[0]
		if r.Leechers == nil || *r.Leechers != 3 || r.Category != tc.want {
			t.Fatalf("%+v", r)
		}
	}
	r := mapReleases([]torznab.Release{{Title: "X", Link: "magnet:?xt=x", Seeders: &seed, Peers: &peers, Leechers: &zero}}).Results[0]
	if r.Leechers == nil || *r.Leechers != 0 {
		t.Fatal("explicit zero lost")
	}
	r = mapReleases([]torznab.Release{{Title: "X", Link: "magnet:?xt=x", Peers: &peers}}).Results[0]
	if r.Leechers != nil {
		t.Fatal("peers without seeds is not leechers")
	}
}

func TestDedicatedFeedProbe(t *testing.T) {
	for _, tc := range []struct {
		kind, body, want string
		status           int
	}{
		{"torznab", `<caps><searching><search available="yes" supportedParams="q"/></searching></caps>`, "", 200},
		{"rss", `<rss version="2.0"><channel><title>Empty feed</title></channel></rss>`, "", 200},
		{"torznab", `<error code="100" description="bad key"/>`, "search_login", 200},
		{"rss", `<html><title>Just a moment...</title><div id="cf-chl-test"></div></html>`, "search_cloudflare", 403},
		{"rss", `nope`, "search_parse", 200},
		{"rss", `denied`, "search_login", 401},
		{"rss", `broken`, "search_http_500", 500},
	} {
		t.Run(tc.kind+tc.want, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.kind == "torznab" && (r.URL.Query().Get("t") != "caps" || r.URL.Query().Has("q") || r.URL.Query().Get("apikey") != "fixture-key") {
					t.Errorf("not a caps probe: %s", r.URL)
				}
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer server.Close()
			rep := run(context.Background(), request{Op: "test", Kind: tc.kind, URL: server.URL, Values: map[string]string{"apikey": "fixture-key"}}, server.Client())
			if rep.Code != tc.want || len(rep.Results) != 0 {
				t.Fatalf("%+v want %s", rep, tc.want)
			}
		})
	}
}

func TestCardigannProbeUsesLoginTestWithoutSearch(t *testing.T) {
	hits := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, r.URL.Path)
		w.Write([]byte(`<a class="logout">Signed in</a>`))
	}))
	defer server.Close()
	raw := fixture
	raw = strings.Replace(raw, "\nsearch:", "\nlogin:\n  method: cookie\n  test:\n    path: /account\n    selector: a.logout\nsearch:", 1)
	_, meta := inspect(raw)
	if !meta.Supported {
		t.Fatalf("meta: %+v yaml=%s", meta, raw)
	}
	rep := run(context.Background(), request{Op: "test", YAML: raw, URL: server.URL + "/", Values: map[string]string{"cookie": "session=fixture"}}, server.Client())
	if rep.Code != "" || len(hits) != 1 || hits[0] != "/account" {
		t.Fatalf("%+v hits=%v", rep, hits)
	}
}
