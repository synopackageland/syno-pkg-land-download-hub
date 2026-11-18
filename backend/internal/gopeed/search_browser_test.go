package gopeed

import (
	"context"
	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"testing"
	"time"
)

func browserJobFixture(t *testing.T) (*Service, string, string) {
	t.Helper()
	return browserJobCodeFixture(t, "search_cloudflare")
}
func browserJobCodeFixture(t *testing.T, code string) (*Service, string, string) {
	t.Helper()
	s, _ := asyncSearchFixture(t)
	ctx := context.Background()
	src, e := s.Search.SaveSource(ctx, "uid:normal1", search.SourceInput{Kind: "cardigann", Name: "Custom", YAML: "id: fixture", Enabled: true})
	if e != nil {
		t.Fatal(e)
	}
	original := s.Search.Run
	s.Search.Run = func(ctx context.Context, in search.Request) (search.Reply, error) {
		switch in.Op {
		case "browser.plan":
			return search.Reply{SiteURL: "https://custom.example/search", BrowserPlan: &search.BrowserPlan{Origins: []string{"https://custom.example"}, Rows: "tr"}}, nil
		case "browser":
			return search.Reply{Results: []search.Hit{{Title: "Linux", Link: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}, nil
		}
		return original(ctx, in)
	}
	s.Search.Stream = func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		siteURL := ""
		if code == "search_cloudflare" {
			siteURL = "https://custom.example/search"
		}
		return emit(0, search.Reply{Code: code, SiteURL: siteURL})
	}
	job := startSearchJob(t, s, src.ID, "rto-job")
	runSearchJob(t, s)
	return s, src.ID, job
}
func TestBrowserJobWaitMergesOwnerBoundResults(t *testing.T) {
	s, src, job := browserJobFixture(t)
	out, _ := call(t, s, "normal1", "search.jobs.wait", map[string]any{"id": job, "timeout": 1})
	if out["success"] != true {
		t.Fatal(out)
	}
	item := out["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if item["status"] != "needs_browser" || item["siteURL"] != "https://custom.example/search" {
		t.Fatal(item)
	}
	for _, user := range []string{"normal2", "admin"} {
		out, _ = call(t, s, user, "search.browser.start", map[string]any{"id": job, "sourceId": src})
		if out["success"] != false {
			t.Fatal("foreign browser job", out)
		}
	}
	out, _ = call(t, s, "normal1", "search.browser.start", map[string]any{"id": job, "sourceId": src})
	if out["success"] != true {
		t.Fatal(out)
	}
	request := out["data"].(map[string]any)
	done := make(chan map[string]any, 1)
	go func() {
		out, _ := call(t, s, "normal1", "search.jobs.wait", map[string]any{"id": job, "timeout": 2, "browser": true})
		done <- out
	}()
	out, _ = call(t, s, "normal1", "search.browser.deliver", map[string]any{"id": request["id"], "token": request["token"], "url": request["url"], "html": "<table>Linux</table>"})
	if out["success"] != true {
		t.Fatal(out)
	}
	select {
	case merged := <-done:
		if merged["success"] != true {
			t.Fatal(merged)
		}
		items := merged["data"].(map[string]any)["items"].([]any)
		results := items[0].(map[string]any)["results"].([]any)
		if len(results) != 1 {
			t.Fatal(merged)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CLI browser wait timed out")
	}
	out, _ = call(t, s, "normal1", "search.browser.deliver", map[string]any{"id": request["id"], "token": request["token"], "url": request["url"], "html": "replay"})
	if out["success"] != false {
		t.Fatal("token replay", out)
	}
}
func TestBrowserCancelRefusesLateDelivery(t *testing.T) {
	s, src, job := browserJobFixture(t)
	out, _ := call(t, s, "normal1", "search.browser.start", map[string]any{"id": job, "sourceId": src})
	request := out["data"].(map[string]any)
	out, _ = call(t, s, "normal1", "search.browser.cancel", map[string]any{"id": job})
	if out["success"] != true {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal1", "search.browser.deliver", map[string]any{"id": request["id"], "token": request["token"], "url": request["url"], "html": "late"})
	if out["success"] != false {
		t.Fatal(out)
	}
}
func TestLegacyCookieJobsPurgeThroughSDKWithoutTouchingDownloads(t *testing.T) {
	s, _ := asyncSearchFixture(t)
	ctx := context.Background()
	owner := background.Submitter{User: "normal1", UID: "1201"}
	store := background.NewRequestStore(s.Data + "/search-jobs")
	_, _, e := store.Submit(ctx, background.SubmitRequest{Key: "legacy-cookie", Name: "cookie-set", Submitter: owner, Data: []byte(`{"cookie":"legacy-secret"}`)})
	if e != nil {
		t.Fatal(e)
	}
	jobs, e := s.searchJobStore().List(ctx)
	if e != nil || len(jobs) != 0 {
		t.Fatal("legacy input retained", jobs, e)
	}
}

func TestNetworkFailuresNeverOfferBrowserAssistance(t *testing.T) {
	for _, code := range []string{"search_connection_reset", "search_connection", "search_tls", "search_timeout", "search_network", "search_dns"} {
		t.Run(code, func(t *testing.T) {
			s, src, job := browserJobCodeFixture(t, code)
			out, _ := call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": job, "offset": 0, "limit": 100})
			item := out["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
			if item["status"] == "needs_browser" || item["error"].(map[string]any)["code"] != code {
				t.Fatal(item)
			}
			out, _ = call(t, s, "normal1", "search.browser.start", map[string]any{"id": job, "sourceId": src})
			if out["success"] != false {
				t.Fatal("browser offered for network failure", out)
			}
		})
	}
}
