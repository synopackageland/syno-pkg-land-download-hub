package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const mirrorPage = `<table><tr><td class="title">Ubuntu</td><td><a href="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">get</a></td></tr></table>`

func mirrored(links ...string) string {
	return strings.Replace(fixture, "links: [https://fixture.invalid/]", "links: ["+strings.Join(links, ", ")+"]", 1)
}

func stub(status int, body string, hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
}

// A mirror that is blocked or broken hands over to the next link of the definition.
func TestMirrorFallbackOnBlockedOrBrokenMirror(t *testing.T) {
	for _, status := range []int{403, 502, 503} {
		var a, b atomic.Int32
		bad, good := stub(status, "nope", &a), stub(200, mirrorPage, &b)
		rep := run(context.Background(), request{YAML: mirrored(bad.URL+"/", good.URL+"/"), Query: "ubuntu", Page: 1, Op: "search"}, http.DefaultClient)
		if rep.Code != "" || len(rep.Results) != 1 || a.Load() != 1 || b.Load() != 1 {
			t.Fatalf("status %d: %+v hits %d/%d", status, rep, a.Load(), b.Load())
		}
		bad.Close()
		good.Close()
	}
}

// A dead first mirror (connection refused) is skipped.
func TestMirrorFallbackOnConnectionError(t *testing.T) {
	var b atomic.Int32
	dead := httptest.NewServer(http.NotFoundHandler())
	dead.Close()
	good := stub(200, mirrorPage, &b)
	defer good.Close()
	rep := run(context.Background(), request{YAML: mirrored(dead.URL+"/", good.URL+"/"), Query: "ubuntu", Page: 1, Op: "search"}, http.DefaultClient)
	if rep.Code != "" || len(rep.Results) != 1 {
		t.Fatalf("%+v", rep)
	}
}

// A site that answered (even with no results, or 404) is not a reason to ask another site.
func TestNoFallbackWhenTheMirrorAnswered(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		code   string
	}{{200, "<html></html>", "search_empty"}, {404, "gone", "search_http_404"}} {
		var a, b atomic.Int32
		first, second := stub(tc.status, tc.body, &a), stub(200, mirrorPage, &b)
		rep := run(context.Background(), request{YAML: mirrored(first.URL+"/", second.URL+"/"), Query: "ubuntu", Page: 1, Op: "search"}, http.DefaultClient)
		if rep.Code != tc.code || b.Load() != 0 {
			t.Fatalf("%d: %+v second hits %d", tc.status, rep, b.Load())
		}
		first.Close()
		second.Close()
	}
}

// A user-pinned URL is never replaced by a mirror.
func TestNoFallbackWhenURLIsPinned(t *testing.T) {
	var a, b atomic.Int32
	bad, good := stub(503, "x", &a), stub(200, mirrorPage, &b)
	defer bad.Close()
	defer good.Close()
	rep := run(context.Background(), request{YAML: mirrored(bad.URL+"/", good.URL+"/"), URL: bad.URL + "/", Query: "ubuntu", Page: 1, Op: "search"}, http.DefaultClient)
	if rep.Code != "search_http_503" || b.Load() != 0 {
		t.Fatalf("%+v", rep)
	}
}

// A mirror that hangs may use at most half the time left; the next one still answers in time.
func TestMirrorFallbackStaysWithinTheDeadline(t *testing.T) {
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer hang.Close()
	defer close(release)
	var b atomic.Int32
	good := stub(200, mirrorPage, &b)
	defer good.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	rep := run(ctx, request{YAML: mirrored(hang.URL+"/", good.URL+"/"), Query: "ubuntu", Page: 1, Op: "search"}, http.DefaultClient)
	if rep.Code != "" || len(rep.Results) != 1 || time.Since(start) > 2*time.Second {
		t.Fatalf("%+v after %s", rep, time.Since(start))
	}
}

// When every mirror fails, a Cloudflare answer wins: it is the one the browser can solve.
func TestMirrorFallbackReportsCloudflare(t *testing.T) {
	var a, b atomic.Int32
	plain := stub(503, "x", &a)
	cf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b.Add(1)
		w.Header().Set("cf-mitigated", "challenge")
		w.WriteHeader(403)
		w.Write([]byte("<title>Just a moment...</title>"))
	}))
	defer plain.Close()
	defer cf.Close()
	rep := run(context.Background(), request{YAML: mirrored(plain.URL+"/", cf.URL+"/"), Query: "ubuntu", Page: 1, Op: "search"}, http.DefaultClient)
	if rep.Code != "search_cloudflare" || !strings.HasPrefix(rep.SiteURL, cf.URL) {
		t.Fatalf("%+v", rep)
	}
}
