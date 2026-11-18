package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func browserFixture(t *testing.T) (*Service, string, time.Time) {
	t.Helper()
	s := fixtureService(t)
	ctx := context.Background()
	src, e := s.SaveSource(ctx, "1001:alice", SourceInput{Kind: "cardigann", Name: "Custom site", YAML: "id: fixture", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	old := s.Run
	s.Run = func(ctx context.Context, in Request) (Reply, error) {
		if in.Op == "browser.plan" {
			return Reply{SiteURL: "https://custom.example/search?q=linux", BrowserPlan: &BrowserPlan{Origins: []string{"https://custom.example"}, Rows: "table.results tr"}}, nil
		}
		return old(ctx, in)
	}
	started := time.Now()
	p := SourcePage{Page: Page{SourceID: src.ID, Page: 1, SiteURL: "https://custom.example/search?q=linux"}}
	if e = s.OfferBrowser(ctx, "1001:alice", "job-a", started, p, "linux"); e != nil {
		t.Fatal(e)
	}
	return s, src.ID, started
}
func TestBrowserTokenBindsOwnerJobSourceURLAndIsOneTime(t *testing.T) {
	s, src, started := browserFixture(t)
	ctx := context.Background()
	if _, e := s.StartBrowser(ctx, "1002:bob", "job-a", src, started); e == nil {
		t.Fatal("foreign owner")
	}
	if _, e := s.StartBrowser(ctx, "1001:alice", "job-b", src, started); e == nil {
		t.Fatal("foreign job")
	}
	if _, e := s.StartBrowser(ctx, "1001:alice", "job-a", src, started.Add(time.Second)); e == nil {
		t.Fatal("worker attempt")
	}
	r, e := s.StartBrowser(ctx, "1001:alice", "job-a", src, started)
	if e != nil {
		t.Fatal(e)
	}
	in := BrowserDelivery{ID: r.ID, Token: r.Token, URL: r.URL, HTML: "<table>results</table>"}
	bad := in
	bad.URL = "https://custom.example/other"
	if _, e = s.DeliverBrowser(ctx, "1001:alice", bad); e == nil {
		t.Fatal("url mutation")
	}
	if _, e = s.DeliverBrowser(ctx, "1002:bob", in); e == nil {
		t.Fatal("cross owner")
	}
	p, e := s.DeliverBrowser(ctx, "1001:alice", in)
	if e != nil || len(p.Results) != 1 {
		t.Fatalf("%+v %v", p, e)
	}
	if _, e = s.DeliverBrowser(ctx, "1001:alice", in); e == nil {
		t.Fatal("replay")
	}
	cached, e := s.Result(ctx, "1001:alice", p.Results[0].ResultID)
	if e != nil || !cached.Browser {
		t.Fatal("browser provenance lost", e)
	}
	pages, e := s.BrowserPages(ctx, "1001:alice", "job-a", started)
	if e != nil || len(pages) != 1 {
		t.Fatal(pages, e)
	}
	pages, _ = s.BrowserPages(ctx, "1002:bob", "job-a", started)
	if len(pages) != 0 {
		t.Fatal("pages owner leak")
	}
}
func TestBrowserExpiryCancelRevisionAndBadDOMDestinations(t *testing.T) {
	for _, mode := range []string{"expired", "canceled", "revision", "foreign-url", "invalid-magnet", "large", "retry"} {
		t.Run(mode, func(t *testing.T) {
			s, src, started := browserFixture(t)
			ctx := context.Background()
			r, _ := s.StartBrowser(ctx, "1001:alice", "job-a", src, started)
			in := BrowserDelivery{ID: r.ID, Token: r.Token, URL: r.URL, HTML: "<table>rows</table>"}
			switch mode {
			case "expired":
				s.withState(ctx, "1001:alice", func(st *state) error {
					x := st.Browser[r.ID]
					x.Expires = time.Now().Add(-time.Second)
					st.Browser[r.ID] = x
					return nil
				})
			case "canceled":
				s.CancelBrowser(ctx, "1001:alice", "job-a")
			case "revision":
				s.SetEnabled(ctx, "1001:alice", src, false)
			case "foreign-url", "invalid-magnet":
				old := s.Run
				s.Run = func(ctx context.Context, q Request) (Reply, error) {
					if q.Op == "browser" {
						link := "https://127.0.0.1/private"
						if mode == "invalid-magnet" {
							link = "magnet:?xt=urn:btih:broken"
						}
						return Reply{Results: []Hit{{Title: "untrusted", Link: link}}}, nil
					}
					return old(ctx, q)
				}
			case "large":
				in.HTML = strings.Repeat("x", BrowserHTMLLimit+1)
			case "retry":
				s.StartBrowser(ctx, "1001:alice", "job-a", src, started)
			}
			if _, e := s.DeliverBrowser(ctx, "1001:alice", in); e == nil {
				t.Fatal("accepted", mode)
			}
		})
	}
}
func TestBrowserDownloadNeverMakesAnUntrustedServerFetch(t *testing.T) {
	s, src, started := browserFixture(t)
	ctx := context.Background()
	r, _ := s.StartBrowser(ctx, "1001:alice", "job-a", src, started)
	old := s.Run
	s.Run = func(ctx context.Context, q Request) (Reply, error) {
		if q.Op == "browser" {
			return Reply{Results: []Hit{{Title: "Detail", Link: "https://custom.example/detail/1"}}}, nil
		}
		return old(ctx, q)
	}
	p, e := s.DeliverBrowser(ctx, "1001:alice", BrowserDelivery{ID: r.ID, Token: r.Token, URL: r.URL, HTML: "rows"})
	if e != nil {
		t.Fatal(e)
	}
	rid := p.Results[0].ResultID
	if _, e = s.Resolve(ctx, "1001:alice", rid); Code(e) != "search_browser_required" {
		t.Fatal("untrusted fetch", e)
	}
	d, e := s.StartBrowserDownload(ctx, "1001:alice", rid)
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DeliverBrowser(ctx, "1001:alice", BrowserDelivery{ID: d.ID, Token: d.Token, URL: d.URL, Magnet: "magnet:?xt=urn:btih:" + strings.Repeat("b", 40) + "&tr=http://127.0.0.1/private"})
	if e != nil {
		t.Fatal(e)
	}
	link, e := s.Resolve(ctx, "1001:alice", rid)
	if e != nil || !strings.Contains(link, strings.Repeat("b", 40)) || strings.Contains(link, "127.0.0.1") {
		t.Fatal(link, e)
	}
}
func TestLegacyBrowserCredentialsAreRemovedFromStoredSources(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	src, _ := s.SaveSource(ctx, "alice", SourceInput{Kind: "cardigann", Name: "legacy", YAML: "id: fixture", Enabled: true})
	path := filepath.Join(s.Data, "users", digest([]byte("alice"))+".json")
	raw, _ := os.ReadFile(path)
	var st map[string]any
	json.Unmarshal(raw, &st)
	delete(st, "browserMigration")
	source := st["sources"].(map[string]any)[src.ID].(map[string]any)
	source["values"] = map[string]string{"cookie": "cf_clearance=old-secret", "useragent": "old-agent", "password": "keep"}
	source["clearance"] = map[string]string{"state": "unlocked"}
	raw, _ = json.Marshal(st)
	os.WriteFile(path, raw, 0600)
	if _, e := s.Sources(ctx, "alice"); e != nil {
		t.Fatal(e)
	}
	raw, _ = os.ReadFile(path)
	if strings.Contains(string(raw), "old-secret") || strings.Contains(string(raw), "old-agent") || strings.Contains(string(raw), "clearance") {
		t.Fatal("legacy credentials retained")
	}
	if !strings.Contains(string(raw), "keep") {
		t.Fatal("unrelated credential removed")
	}
}

func TestBrowserEmptyAndNoDataAreRetainedSourcePages(t *testing.T) {
	for _, code := range []string{"search_empty", "search_no_data"} {
		t.Run(code, func(t *testing.T) {
			s, src, started := browserFixture(t)
			old := s.Run
			s.Run = func(ctx context.Context, in Request) (Reply, error) {
				if in.Op == "browser" {
					return Reply{Code: code}, nil
				}
				return old(ctx, in)
			}
			r, err := s.StartBrowser(context.Background(), "1001:alice", "job-a", src, started)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.DeliverBrowser(context.Background(), "1001:alice", BrowserDelivery{ID: r.ID, Token: r.Token, URL: r.URL, HTML: "<p>No rows</p>"})
			if err != nil || p.Error == nil || p.Error.Code != code {
				t.Fatalf("%+v %v", p, err)
			}
			pages, err := s.BrowserPages(context.Background(), "1001:alice", "job-a", started)
			if err != nil || pages[src].Error == nil || pages[src].Error.Code != code {
				t.Fatalf("%+v %v", pages, err)
			}
		})
	}
}

// A parse cut short by a slow NAS is not the page's failure: the token comes
// back, and the same page delivered again is parsed and kept.
func TestBrowserDeliveryOnASlowNASHandsTheTokenBackForTheSamePage(t *testing.T) {
	s, src, started := browserFixture(t)
	old := s.Run
	slow := true
	s.Run = func(ctx context.Context, in Request) (Reply, error) {
		if in.Op == "browser" {
			if slow {
				<-ctx.Done()
				return Reply{}, ctx.Err()
			}
			return Reply{Results: []Hit{{Title: "Ubuntu", Link: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}}}, nil
		}
		return old(ctx, in)
	}
	r, err := s.StartBrowser(context.Background(), "1001:alice", "job-a", src, started)
	if err != nil {
		t.Fatal(err)
	}
	in := BrowserDelivery{ID: r.ID, Token: r.Token, URL: r.URL, HTML: "<table class=results><tr><td>Ubuntu</td></tr></table>"}
	ctx, cancel := context.WithTimeout(context.Background(), browserKeepMargin+200*time.Millisecond)
	_, err = s.DeliverBrowser(ctx, "1001:alice", in)
	cancel()
	if Code(err) != "search_browser_busy" {
		t.Fatal(err)
	}
	slow = false
	p, err := s.DeliverBrowser(context.Background(), "1001:alice", in)
	if err != nil || len(p.Results) != 1 {
		t.Fatalf("%+v %v", p, err)
	}
	if _, err = s.DeliverBrowser(context.Background(), "1001:alice", in); Code(err) != "search_browser_expired" {
		t.Fatal("a kept page's token was reusable", err)
	}
}

// The owner's search file waits for a slow writer instead of failing after a
// fixed time; a wait ended by the caller says the NAS is busy.
func TestSearchStateWaitsForASlowWriterAndNamesABusyNAS(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	release := make(chan struct{})
	held := make(chan struct{})
	go s.withState(ctx, "1001:alice", func(*state) error { close(held); <-release; return nil })
	<-held
	go func() { time.Sleep(2500 * time.Millisecond); close(release) }()
	if err := s.withState(ctx, "1001:alice", func(*state) error { return nil }); err != nil {
		t.Fatal("a write slower than two seconds failed the wait", err)
	}
	release2 := make(chan struct{})
	held2 := make(chan struct{})
	go s.withState(ctx, "1001:alice", func(*state) error { close(held2); <-release2; return nil })
	<-held2
	defer close(release2)
	short, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	if err := s.withState(short, "1001:alice", func(*state) error { return nil }); Code(err) != "search_storage_busy" {
		t.Fatal(err)
	}
}
