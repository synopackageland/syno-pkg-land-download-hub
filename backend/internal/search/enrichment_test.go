package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSourceURLRedactionAndUnchangedEdit(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	for _, kind := range []string{"rss", "torznab"} {
		raw := "https://example.org/passkey/private-path/feed?apikey=private-key&username=private-user&password=private-password&custom=private-cookie"
		src, err := s.SaveSource(ctx, "1:a", SourceInput{Kind: kind, Name: "Before", URL: raw, Enabled: true, Values: map[string]string{"cookie": "private-cookie"}})
		if err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(src)
		if src.URL == "" || !strings.Contains(src.URL, "example.org") || strings.Contains(string(b), "private-") {
			t.Fatalf("unsafe/missing view: %s", b)
		}
		for _, url := range []string{src.URL, ""} {
			edited, err := s.SaveSource(ctx, "1:a", SourceInput{ID: src.ID, Kind: kind, Name: "After", URL: url, Enabled: true})
			if err != nil {
				t.Fatal(err)
			}
			if edited.URL != src.URL {
				t.Fatalf("view changed: %+v", edited)
			}
		}
		s.Run = func(_ context.Context, in Request) (Reply, error) {
			if kind == "rss" && in.URL != raw {
				t.Errorf("lost original RSS URL")
			}
			if kind == "torznab" && in.Values["apikey"] != "private-key" {
				t.Errorf("lost API key")
			}
			return Reply{Results: []Hit{hit("ok")}}, nil
		}
		if _, err = s.Search(ctx, "1:a", Query{SourceID: src.ID, Page: 1}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSourceTestUsesProbeAndOwnerBoundary(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	src, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	calls := 0
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		calls++
		if in.Op != "test" {
			t.Errorf("not a probe: %+v", in)
		}
		return Reply{Code: "search_cloudflare"}, nil
	}
	out, err := s.TestSource(ctx, "1:a", src.ID)
	if err != nil || out.Status != "cloudflare" || out.Code != "search_cloudflare" {
		t.Fatalf("%+v %v", out, err)
	}
	if _, err = s.TestSource(ctx, "2:b", src.ID); Code(err) != "search_source_missing" || calls != 1 {
		t.Fatalf("owner boundary: %v calls=%d", err, calls)
	}
}

func TestEndpointExtractedKeysStillRespectValueValidation(t *testing.T) {
	s := fixtureService(t)
	_, err := s.SaveSource(context.Background(), "1:a", SourceInput{Kind: "torznab", Name: "Invalid", URL: "https://example.org/api?apikey=bad%0Aheader", Enabled: true})
	if Code(err) != "invalid_request" {
		t.Fatalf("extracted key bypassed validation: %v", err)
	}
}
