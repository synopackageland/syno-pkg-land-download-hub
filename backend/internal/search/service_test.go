package search

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixtureService(t *testing.T) *Service {
	t.Helper()
	return &Service{Data: t.TempDir(), Run: func(ctx context.Context, in Request) (Reply, error) {
		switch in.Op {
		case "validate", "catalog":
			return Reply{Sites: []Site{{ID: "fixture", Name: "Fixture", Supported: true, Reasons: []string{}, Settings: []Setting{{Name: "password", Type: "password"}}}}}, nil
		}
		return Reply{Results: []Hit{{Title: "Ubuntu", Link: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Hash: strings.Repeat("a", 40)}}}, nil
	}}
}
func TestSourcesAndResultsAreOwnerBoundAndSecretsNeverEcho(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	a, err := s.SaveSource(ctx, "1001:alice", SourceInput{Kind: "cardigann", Name: "Mine", YAML: "id: fixture", Enabled: true, Values: map[string]string{"password": "topsecret"}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), "topsecret") {
		t.Fatal("credentials leaked")
	}
	list, err := s.Sources(ctx, "1002:bob")
	if err != nil || len(list) != 0 {
		t.Fatalf("%+v %v", list, err)
	}
	page, err := s.Search(ctx, "1001:alice", Query{SourceID: a.ID, Text: "ubuntu", Page: 1})
	if err != nil || len(page.Results) != 1 {
		t.Fatalf("%+v %v", page, err)
	}
	raw, _ = json.Marshal(page)
	if strings.Contains(string(raw), "magnet:") {
		t.Fatal("result links leaked")
	}
	if _, err = s.Result(ctx, "1002:bob", page.Results[0].ResultID); Code(err) != "search_result_expired" {
		t.Fatalf("cross-owner result: %v", err)
	}
	if _, err = s.Result(ctx, "1001:alice", page.Results[0].ResultID); err != nil {
		t.Fatal(err)
	}
	a.Enabled = false
	if _, err = s.SaveSource(ctx, "1001:alice", SourceInput{ID: a.ID, Kind: "cardigann", Name: "Mine", YAML: "id: fixture", Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Result(ctx, "1001:alice", page.Results[0].ResultID); Code(err) != "search_result_expired" {
		t.Fatalf("disabled source result usable: %v", err)
	}
}
func TestEmptyAndDeadlineAreNotSuccess(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	src, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "RSS", URL: "https://example.org/feed", Enabled: true})
	s.Run = func(context.Context, Request) (Reply, error) { return Reply{}, nil }
	if _, err := s.Search(ctx, "1:a", Query{SourceID: src.ID, Page: 1}); Code(err) != "search_empty" {
		t.Fatalf("%v", err)
	}
	s.Run = func(ctx context.Context, _ Request) (Reply, error) { <-ctx.Done(); return Reply{}, ctx.Err() }
	c, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	if _, err := s.Search(c, "1:a", Query{SourceID: src.ID, Page: 1}); Code(err) != "search_timeout" {
		t.Fatalf("%v", err)
	}
}
func TestFlockLimitsAcrossServiceInstances(t *testing.T) {
	s := fixtureService(t)
	s.SlotWait = 50 * time.Millisecond
	entered := make(chan struct{}, slotCount+1)
	release := make(chan struct{})
	s.Run = func(ctx context.Context, _ Request) (Reply, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return Reply{Code: "search_empty"}, nil
	}
	src, _ := s.SaveSource(context.Background(), "1:a", SourceInput{Kind: "rss", Name: "RSS", URL: "https://example.org/feed", Enabled: true})
	var wg sync.WaitGroup
	busy := make(chan string, 1)
	for i := 0; i < slotCount+1; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			other := *s
			if _, err := other.Search(context.Background(), "1:a", Query{SourceID: src.ID, Page: 1}); Code(err) == "search_busy" {
				busy <- "search_busy"
			}
		}()
	}
	for i := 0; i < slotCount; i++ {
		<-entered
	}
	// The extra search waits for a slot, then answers busy without running.
	if code := <-busy; code != "search_busy" {
		t.Error(code)
	}
	select {
	case <-entered:
		t.Errorf("exceeded %d CGI slots", slotCount)
	default:
	}
	close(release)
	wg.Wait()
}

func TestDurableTicketSurvivesResultExpiryAndRefusesRevokedSource(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	src, err := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Search(ctx, "1:a", Query{SourceID: src.ID, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	key, err := s.Pin(ctx, "1:a", page.Results[0].ResultID, "btih:aaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	s.withState(ctx, "1:a", func(st *state) error { st.Results = map[string]Cached{}; return nil })
	restarted := *s
	if link, err := restarted.ResolveTicket(ctx, "1:a", key); err != nil || !strings.HasPrefix(link, "magnet:") {
		t.Fatalf("%s %v", link, err)
	}
	if _, err := restarted.ResolveTicket(ctx, "1:b", key); err == nil {
		t.Fatal("ticket crossed owners")
	}
	if err := s.SetEnabled(ctx, "1:a", src.ID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveTicket(ctx, "1:a", key); Code(err) != "search_source_disabled" {
		t.Fatal("revoked ticket resolved", err)
	}
}
func TestDownloadDoesNotForwardCredentialsToOffOriginEnclosures(t *testing.T) {
	s := fixtureService(t)
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		if in.Op == "download" && len(in.Values) > 0 {
			t.Fatal("off-origin credentials")
		}
		return Reply{Link: "magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}, nil
	}
	_, err := s.resolveCached(context.Background(), "1:a", Cached{Source: Source{SourceInput: SourceInput{Kind: "torznab", URL: "https://tracker.example/api", Values: map[string]string{"apikey": "secret"}}}, Hit: Hit{Link: "https://cdn.example/get.torrent"}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicRSSCanHandMediaAndHTTPEnclosuresToExistingIntake(t *testing.T) {
	s := fixtureService(t)
	s.Run = func(context.Context, Request) (Reply, error) {
		t.Fatal("public RSS should not be downloaded during ticket resolution")
		return Reply{}, nil
	}
	for _, link := range []string{"https://www.youtube.com/watch?v=fixture", "https://example.org/file.zip", "https://example.org/file.torrent"} {
		got, err := s.resolveCached(context.Background(), "1:a", Cached{Source: Source{SourceInput: SourceInput{Kind: "rss"}}, Hit: Hit{Link: link}})
		if got != link || err != nil {
			t.Fatalf("%s %v", got, err)
		}
	}
}

func TestSourceCredentialEditMergesAndClearReplacesWithoutEcho(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	a, err := s.SaveSource(ctx, "1:a", SourceInput{Kind: "cardigann", Name: "Fixture", YAML: "id: fixture", Enabled: true, Values: map[string]string{"username": "name", "password": "secret"}})
	if err != nil {
		t.Fatal(err)
	}
	in := SourceInput{ID: a.ID, Kind: "cardigann", Name: "Edited", Enabled: true, Values: map[string]string{"username": "new"}}
	if _, err = s.SaveSource(ctx, "1:a", in); err != nil {
		t.Fatal(err)
	}
	s.withState(ctx, "1:a", func(st *state) error {
		v := st.Sources[a.ID]
		if v.Values["password"] != "secret" || v.Values["username"] != "new" || v.YAML == "" {
			t.Fatalf("%+v", v)
		}
		return nil
	})
	in.ClearValues = true
	in.Values = map[string]string{"cookie": "new-cookie"}
	if _, err = s.SaveSource(ctx, "1:a", in); err != nil {
		t.Fatal(err)
	}
	s.withState(ctx, "1:a", func(st *state) error {
		v := st.Sources[a.ID]
		if len(v.Values) != 1 || v.Values["cookie"] != "new-cookie" {
			t.Fatal(v.Values)
		}
		return nil
	})
	yaml, err := s.ExportSource(ctx, "1:a", a.ID)
	if err != nil || strings.Contains(yaml, "secret") {
		t.Fatalf("%s %v", yaml, err)
	}
}

func TestBuiltinSelectionUsesDefinitionIDWhenFilenameDiffers(t *testing.T) {
	s := fixtureService(t)
	raw, err := s.definition(context.Background(), Source{SourceInput: SourceInput{Kind: "builtin", DefinitionID: "bluebirdhd"}})
	if err != nil || !strings.Contains(raw, "bluebirdhd") {
		t.Fatalf("ID-to-file lookup failed: %v", err)
	}
}

func TestEditedSourceCreatesNewImmutableTicketForSameContent(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	src, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	page, _ := s.Search(ctx, "1:a", Query{SourceID: src.ID, Page: 1})
	first, err := s.Pin(ctx, "1:a", page.Results[0].ResultID, "same-content")
	if err != nil {
		t.Fatal(err)
	}
	s.SetEnabled(ctx, "1:a", src.ID, true)
	page, _ = s.Search(ctx, "1:a", Query{SourceID: src.ID, Page: 1})
	second, err := s.Pin(ctx, "1:a", page.Results[0].ResultID, "same-content")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("source edit retargeted an existing task ticket")
	}
	if _, err = s.ResolveTicket(ctx, "1:a", first); Code(err) != "search_source_disabled" {
		t.Fatal(err)
	}
	if _, err = s.ResolveTicket(ctx, "1:a", second); err != nil {
		t.Fatal(err)
	}
}
