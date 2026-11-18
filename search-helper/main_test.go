package main

import (
	"context"
	"encoding/json"
	"github.com/mediactl/clustarr/pkg/cardigann"
	"github.com/mediactl/clustarr/pkg/torznab"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const fixture = `id: fixture
name: Fixture
description: offline
language: en-US
type: public
encoding: UTF-8
links: [https://fixture.invalid/]
caps:
  categories: {1: Movies}
  modes: {search: [q]}
search:
  path: search
  rows: {selector: tr}
  fields:
    title: {selector: td.title}
    download: {selector: a, attribute: href}
    category: {text: 1}
    size: {text: 1}
    seeders: {text: 1}
`

func TestUnsupportedFilterAndTemplateNeverRun(t *testing.T) {
	for _, body := range []string{strings.Replace(fixture, "size: {text: 1}", "size: {text: 1, filters: [{name: base64decode}]}", 1), strings.Replace(fixture, "path: search", "path: '{{ shell .Keywords }}'", 1)} {
		_, site := inspect(body)
		if site.Supported || len(site.Reasons) == 0 {
			t.Fatalf("silently accepted %+v", site)
		}
	}
}
func TestReplaceFilterNeedsANeedle(t *testing.T) {
	for _, filter := range []string{"{name: replace}", "{name: replace, args: ['', x]}"} {
		_, site := inspect(strings.Replace(fixture, "size: {text: 1}", "size: {text: 1, filters: ["+filter+"]}", 1))
		if site.Supported || len(site.Reasons) == 0 {
			t.Fatalf("%s accepted %+v", filter, site)
		}
	}
	if _, site := inspect(strings.Replace(fixture, "size: {text: 1}", "size: {text: 1, filters: [{name: replace, args: [a, b]}]}", 1)); !site.Supported {
		t.Fatalf("refused a normal replace %+v", site)
	}
}

func TestSearchOfflineAndEmptyClassification(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("empty") == "1" {
			w.Write([]byte("<html></html>"))
			return
		}
		w.Write([]byte(`<table><tr><td class="title">Ubuntu</td><td><a href="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">get</a></td></tr></table>`))
	}))
	defer srv.Close()
	if _, err := cardigann.Load([]byte(fixture)); err != nil {
		t.Fatal(err)
	}
	in := request{YAML: fixture, URL: srv.URL + "/", Query: "ubuntu", Page: 1}
	rep := run(context.Background(), in, srv.Client())
	if rep.Code != "" || len(rep.Results) != 1 {
		t.Fatalf("%+v", rep)
	}
	in.YAML = strings.Replace(fixture, "path: search", "path: search?empty=1", 1)
	rep = run(context.Background(), in, srv.Client())
	if rep.Code != "search_empty" {
		t.Fatalf("%+v", rep)
	}
}
func TestRSSAndTorznabFixtures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") == "search" && (r.URL.Query().Get("offset") != "100" || r.Header.Get("X-Api-Key") != "secret") {
			t.Error("pagination/header contract")
		}
		w.Write([]byte(`<rss version="2.0"><channel><title>Fixtures</title><item><title>Ubuntu</title><link>magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</link><enclosure url="https://fixture.invalid/test.torrent" type="application/x-bittorrent" length="10"/></item></channel></rss>`))
	}))
	defer srv.Close()
	for _, kind := range []string{"rss", "torznab"} {
		page := 1
		if kind == "torznab" {
			page = 2
		}
		rep := run(context.Background(), request{Kind: kind, URL: srv.URL, Query: "Ubuntu", Page: page, Values: map[string]string{"apikey": "secret"}}, srv.Client())
		if rep.Code != "" || len(rep.Results) != 1 {
			t.Fatalf("%s: %+v", kind, rep)
		}
	}
}

func TestTorznabHTTP200AuthenticationErrorIsLoginFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<error code="100" description="bad credentials"/>`))
	}))
	defer srv.Close()
	rep := feed(context.Background(), request{Kind: "torznab", URL: srv.URL, Page: 1}, srv.Client())
	if rep.Code != "search_login" {
		t.Fatalf("%+v", rep)
	}
}

func TestTorznabQueryAuthenticationAndRedactedDownloadLinks(t *testing.T) {
	secret := "PRIVATE-KEY"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != secret {
			t.Error("Torznab did not authenticate through query")
		}
		if r.URL.Path == "/download" {
			w.Write([]byte("d1:ai1ee"))
			return
		}
		w.Write([]byte(`<rss version="2.0"><channel><item><title>Ubuntu</title><link>` + "https://jackett.example/download?apikey=" + secret + `</link></item></channel></rss>`))
	}))
	defer srv.Close()
	rep := feed(context.Background(), request{Kind: "torznab", URL: srv.URL + "/api/v2.0/indexers/all/results/torznab", Page: 1, Values: map[string]string{"apikey": secret}}, srv.Client())
	if rep.Code != "" || len(rep.Results) != 1 {
		t.Fatalf("%+v", rep)
	}
	if strings.Contains(rep.Results[0].Link, secret) || !strings.Contains(rep.Results[0].Link, "apikey=***") {
		t.Fatal("helper exposed key")
	}
	rep = feed(context.Background(), request{Op: "download", Kind: "torznab", URL: srv.URL + "/download?apikey=***", Values: map[string]string{"apikey": secret}}, srv.Client())
	if rep.Code != "" || rep.Torrent == "" {
		t.Fatalf("%+v", rep)
	}
}

// A category becomes Torznab's cat parameter and the Cardigann query's categories.
func TestCategoriesReachTorznabAndCardigann(t *testing.T) {
	var cat string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cat = r.URL.Query().Get("cat")
		w.Write([]byte(`<rss version="2.0"><channel></channel></rss>`))
	}))
	defer srv.Close()
	feed(context.Background(), request{Kind: "torznab", URL: srv.URL + "/api", Page: 1, Query: "x", Categories: []int{5000, 5070}}, srv.Client())
	if cat != "5000,5070" {
		t.Fatalf("cat %q", cat)
	}
	feed(context.Background(), request{Kind: "torznab", URL: srv.URL + "/api", Page: 1, Query: "x"}, srv.Client())
	if cat != "" {
		t.Fatalf("cat %q without category", cat)
	}
	q := cardigannQuery(request{Query: "x", Categories: []int{2000}})
	if q.Q != "x" || q.Type != "search" || len(q.Categories) != 1 || q.Categories[0] != 2000 {
		t.Fatalf("%+v", q)
	}
	if q := cardigannQuery(request{Query: "x"}); q.Categories != nil {
		t.Fatalf("%+v", q)
	}
}

// bundled returns a definition from the bundled Jackett set.
func bundled(t *testing.T, file string) string {
	t.Helper()
	b, err := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct{ Definitions map[string]string }
	if err := json.Unmarshal(b, &bundle); err != nil || bundle.Definitions[file] == "" {
		t.Fatalf("no %s: %v", file, err)
	}
	return bundle.Definitions[file]
}

// TPB answers a JSON array at the root; rows.count is "$[0].id".
func TestThePirateBayRootArray(t *testing.T) {
	body := `[{"id":"1","name":"Ubuntu 24.04","info_hash":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","leechers":"1","seeders":"9","num_files":"1","size":"1000","username":"u","added":"1700000000","category":"207","imdb":""}]`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/q.php" || r.URL.Query().Get("q") != "ubuntu" {
			t.Errorf("unexpected %s", r.URL)
		}
		w.Write([]byte(body))
	}))
	defer srv.Close()
	// The definition's default apiurl (apibay.org) must apply; dial the stub instead.
	client := srv.Client()
	client.Transport.(*http.Transport).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, srv.Listener.Addr().String())
	}
	client.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true // stub cert is not for apibay.org
	in := request{YAML: bundled(t, "thepiratebay.yml"), Kind: "builtin", URL: "https://x.invalid/", Query: "ubuntu", Page: 1}
	rep := run(context.Background(), in, client)
	if rep.Code != "" || len(rep.Results) != 1 || rep.Results[0].Title != "Ubuntu 24.04" || !strings.HasPrefix(rep.Results[0].Link, "magnet:?") {
		t.Fatalf("%+v", rep)
	}
	// TPB's "no results" answer is a single zero-id row.
	body = `[{"id":"0","name":"No results returned","info_hash":"0000000000000000000000000000000000000000","leechers":"0","seeders":"0","num_files":"0","size":"0","username":"","added":"0","category":"0","imdb":""}]`
	if rep := run(context.Background(), in, client); rep.Code != "search_empty" {
		t.Fatalf("%+v", rep)
	}
}

func eztvRow(title string) string {
	return `<tr name="hover" class="forum_header_border"><td>x</td><td><a href="/ep/1/" title="` + title + ` (500 MB)">t</a></td><td><a class="magnet" href="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">m</a></td><td>500 MB</td><td>1 day</td><td>5</td></tr>`
}

// andmatch keeps a row only when every query word is in its title.
func TestAndMatchRowFilterOnEZTV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<table class="forum_header_border"><tr><td>Latest</td></tr>` +
			eztvRow("Late Night News S01E01 720p") + eztvRow("Night Owls S02E01") + eztvRow("The Daily NEWS Night Show") + `</table>`))
	}))
	defer srv.Close()
	yml := bundled(t, "eztv.yml")
	if _, site := inspect(yml); !site.Supported {
		t.Fatalf("eztv unsupported: %v", site.Reasons)
	}
	titles := func(query string) []string {
		rep := run(context.Background(), request{YAML: yml, URL: srv.URL + "/", Query: query, Page: 1}, srv.Client())
		var out []string
		for _, r := range rep.Results {
			out = append(out, r.Title)
		}
		if len(out) == 0 && rep.Code != "search_empty" {
			t.Fatalf("%+v", rep)
		}
		return out
	}
	if got := titles("night news"); len(got) != 2 || got[0] != "Late Night News S01E01 720p" {
		t.Fatalf("got %q", got)
	}
	if got := titles("News-Night"); len(got) != 2 {
		t.Fatalf("punctuation split: %q", got)
	}
	if got := titles("owls news"); len(got) != 0 {
		t.Fatalf("got %q", got)
	}
	if got := titles(""); len(got) != 3 {
		t.Fatalf("keywordless search must not filter: %q", got)
	}
}

// andmatch is a row filter only; as a field filter the engine would fail on it.
func TestAndMatchOnlyAllowedAsRowFilter(t *testing.T) {
	ok := strings.Replace(fixture, "rows: {selector: tr}", "rows: {selector: tr, filters: [{name: andmatch}]}", 1)
	if _, site := inspect(ok); !site.Supported {
		t.Fatalf("row filter refused: %v", site.Reasons)
	}
	bad := strings.Replace(fixture, "size: {text: 1}", "size: {text: 1, filters: [{name: andmatch}]}", 1)
	if _, site := inspect(bad); site.Supported {
		t.Fatal("andmatch accepted as a field filter")
	}
}

func TestAndMatchCharacterLimit(t *testing.T) {
	rows := []torznab.Release{{Title: "ubuntu server"}, {Title: "server ubuntu"}}
	got := andMatch(rows, "ubuntu", 6)
	if len(got) != 1 || got[0].Title != "ubuntu server" {
		t.Fatalf("%+v", got)
	}
}

// Optional text settings are the ones nobody has to fill in: a default is there,
// or the site has no login that could need them.
func TestCatalogMarksSettingsThatNeedNoInput(t *testing.T) {
	_, tpb := inspect(bundled(t, "thepiratebay.yml"))
	for _, s := range tpb.Settings {
		if s.Type == "text" && !s.Optional {
			t.Fatalf("TPB %s needs no input: %+v", s.Name, tpb.Settings)
		}
	}
	raw, err := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct{ Definitions map[string]string }
	json.Unmarshal(raw, &bundle)
	needInput := 0
	for file, yml := range bundle.Definitions {
		def, site := inspect(yml)
		if def == nil || def.Login == nil {
			continue
		}
		for _, f := range def.Settings {
			if (f.Type == "text" || f.Type == "password") && string(f.Default) == "" {
				for _, s := range site.Settings {
					if s.Name == f.Name && s.Optional {
						t.Errorf("%s: %s is required by the login but marked optional", file, f.Name)
					}
				}
				needInput++
			}
		}
	}
	if needInput == 0 {
		t.Fatal("no login definition with a required setting found")
	}
}

func TestUnparsedPublishDateIsLeftOut(t *testing.T) {
	if got := pubDate(time.Time{}); got != "" {
		t.Fatalf("zero date = %q", got)
	}
	d := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	if got := pubDate(d); got != "2026-10-17T00:00:00Z" {
		t.Fatalf("date = %q", got)
	}
}
