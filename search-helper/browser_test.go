package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
)

type noBrowserNetwork struct{ t *testing.T }

func (n noBrowserNetwork) RoundTrip(*http.Request) (*http.Response, error) {
	n.t.Fatal("browser parser used network")
	return nil, nil
}
func TestBrowserUsesCustomDefinitionFiltersWithNoNetwork(t *testing.T) {
	raw := strings.Replace(fixture, "title: {selector: td.title}", "title: {selector: td.title, filters: [{name: prepend, args: ['Custom ']}]}", 1)
	client := &http.Client{Transport: noBrowserNetwork{t}}
	p := run(context.Background(), request{Op: "browser.plan", YAML: raw, Query: "linux"}, client)
	if p.BrowserPlan == nil || p.BrowserPlan.Reason != "" || p.BrowserPlan.Rows != "tr" || p.SiteURL == "" {
		t.Fatalf("%+v", p)
	}
	out := run(context.Background(), request{Op: "browser", YAML: raw, Query: "linux", HTML: `<table><tr><td class="title">Linux</td><td><a href="magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">Get</a></td></tr></table>`}, client)
	if out.Code != "" || len(out.Results) != 1 || out.Results[0].Title != "Custom Linux" {
		t.Fatalf("%+v", out)
	}
}
func TestBrowserParsesBuiltin1337xWithoutSiteSpecificAdapter(t *testing.T) {
	b, _ := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	var bundle struct {
		Definitions map[string]string `json:"definitions"`
	}
	json.Unmarshal(b, &bundle)
	html, e := os.ReadFile("../third-party/clustarr/test/data/cardigann/1337x-search.html")
	if e != nil {
		t.Fatal(e)
	}
	out := run(context.Background(), request{Op: "browser", YAML: bundle.Definitions["1337x.yml"], Query: "ubuntu", HTML: string(html)}, &http.Client{Transport: noBrowserNetwork{t}})
	if out.Code != "" || len(out.Results) == 0 {
		t.Fatalf("%+v", out)
	}
}
func TestBrowserReportsLoginAndAPIFeatures(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{{strings.Replace(fixture, "search:\n", "login: {method: cookie}\nsearch:\n", 1), "login"}, {strings.Replace(fixture, "path: search", "paths: [{path: search, response: {type: json}}]", 1), "api"}, {strings.Replace(fixture, "path: search", "paths: [{path: one, method: post}]", 1), "post"}} {
		def, _ := inspect(tc.raw)
		if got := browserUnsupported(def); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
}

func TestBrowserContainerPreservesSourceContext(t *testing.T) {
	for rows, want := range map[string]string{"table.data tr[id]:has(a[data-download])": "table.data", "table.torrent-table > tbody > tr": "table.torrent-table", "tr:has(a)": "body", ".rows > div": "body"} {
		if got := browserContainer(rows); got != want {
			t.Fatalf("%q: %q != %q", rows, got, want)
		}
	}
}
func TestLinuxTrackerLiveChangedPaths(t *testing.T) {
	b, err := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Definitions map[string]string `json:"definitions"`
	}
	json.Unmarshal(b, &bundle)
	for _, tc := range []struct {
		file, query, code string
		count             int
	}{{"linuxtracker-unavailable.html", "the cycle", "search_no_data", 0}, {"linuxtracker-results.html", "ubuntu", "", 1}, {"linuxtracker-empty.html", "missing", "search_empty", 0}} {
		html, err := os.ReadFile("testdata/" + tc.file)
		if err != nil {
			t.Fatal(err)
		}
		out := run(context.Background(), request{Op: "browser", YAML: bundle.Definitions["linuxtracker.yml"], Query: tc.query, HTML: string(html)}, &http.Client{Transport: noBrowserNetwork{t}})
		if out.Code != tc.code || len(out.Results) != tc.count {
			t.Fatalf("%s: %+v", tc.file, out)
		}
		if len(out.Results) > 0 && (out.Results[0].Seeders == nil || *out.Results[0].Seeders != 15) {
			t.Fatalf("local/external counts concatenated: %+v", out.Results[0])
		}
	}
}

func TestKATTransportBudgetIsSourceSpecific(t *testing.T) {
	b, _ := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	var bundle struct {
		Definitions map[string]string `json:"definitions"`
	}
	json.Unmarshal(b, &bundle)
	for name, want := range map[string]int{"kickasstorrents-ws.yml": 512 << 10, "linuxtracker.yml": 0, "1337x.yml": 0} {
		out := run(context.Background(), request{Op: "browser.plan", YAML: bundle.Definitions[name], Query: "ubuntu"}, &http.Client{Transport: noBrowserNetwork{t}})
		if out.BrowserPlan == nil || out.BrowserPlan.MaxBytes != want {
			t.Fatalf("%s: %+v", name, out)
		}
	}
}

func TestKickassBrowserParsesJustNowWithoutDroppingThePage(t *testing.T) {
	b, _ := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	var bundle struct {
		Definitions map[string]string `json:"definitions"`
	}
	json.Unmarshal(b, &bundle)
	html := `<table class="data"><tr id="torrent_1"><td><a class="cellMainLink" href="/example-1.html">Example Linux image</a><div><a data-download="1" href="https://mylink.cloud/?url=magnet%3A%3Fxt%3Durn%3Abtih%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa">Get</a></div></td><td>1 GB</td><td>just now</td><td>12</td><td>2</td></tr></table>`
	out := run(context.Background(), request{Op: "browser", YAML: bundle.Definitions["kickasstorrents-ws.yml"], Query: "the cycle", Page: 1, HTML: html}, &http.Client{Transport: noBrowserNetwork{t}})
	if out.Code != "" || len(out.Results) != 1 || out.Results[0].Title != "Example Linux image" || !strings.HasPrefix(out.Results[0].Link, "magnet:") {
		t.Fatalf("%+v", out)
	}
}

// EZTV sits behind Cloudflare, so browser assist is how it runs; the page the
// browser shows must get the same andmatch row filter as a direct search.
func TestBrowserEZTVAppliesAndMatch(t *testing.T) {
	yml := bundled(t, "eztv.yml")
	client := &http.Client{Transport: noBrowserNetwork{t}}
	html := `<table class="forum_header_border"><tr><td>Latest</td></tr>` +
		eztvRow("Late Night News S01E01 720p") + eztvRow("Night Owls S02E01") + eztvRow("The Daily NEWS Night Show") + `</table>`
	out := run(context.Background(), request{Op: "browser", YAML: yml, Query: "night news", HTML: html}, client)
	if out.Code != "" || len(out.Results) != 2 || out.Results[0].Title != "Late Night News S01E01 720p" {
		t.Fatalf("%+v", out)
	}
}
