package search

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTorznabKeysStayInSourceOnlyAndResultsTicketsAreRedacted(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	secret := "TORZNAB-PRIVATE-KEY"
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		return Reply{Results: []Hit{{Title: "Ubuntu", Link: "https://jackett.example/download/1?apikey=" + secret + "&file=x"}}}, nil
	}
	src, err := s.SaveSource(ctx, "1:a", SourceInput{Kind: "torznab", Name: "Jackett", URL: "https://jackett.example/api/v2.0/indexers/all/results/torznab?apikey=" + secret, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if !src.Supported {
		t.Fatal("standard Jackett rejected", src)
	}
	page, err := s.Search(ctx, "1:a", Query{SourceID: src.ID, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	r, err := s.Result(ctx, "1:a", page.Results[0].ResultID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.Hit.Link, secret) || !strings.Contains(r.Hit.Link, "apikey=***") {
		t.Fatal("result leaked key", r.Hit.Link)
	}
	key, err := s.Pin(ctx, "1:a", page.Results[0].ResultID, "same-torrent")
	if err != nil {
		t.Fatal(err)
	}
	var st state
	if readJSON(filepath.Join(s.Data, "users", digest([]byte("1:a"))+".json"), &st, 16<<20) != nil {
		t.Fatal("missing state")
	}
	raw, _ := json.Marshal(st.Results)
	if strings.Contains(string(raw), secret) {
		t.Fatal("result journal leaked key")
	}
	raw, err = os.ReadFile(filepath.Join(s.Data, "tickets", digest([]byte("1:a")), key+".json"))
	if err != nil || strings.Contains(string(raw), secret) {
		t.Fatalf("ticket leaked key: %v", err)
	}
	srcRecord := st.Sources[src.ID]
	if srcRecord.Values["apikey"] != secret || strings.Contains(srcRecord.URL, secret) {
		t.Fatal("key not isolated to source values")
	}
}
func TestTorznabDownloadUsesPrivateFetcherAndHandsOnlyTorrentBytesToIntake(t *testing.T) {
	s := fixtureService(t)
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		if in.Op != "download" || in.Values["apikey"] != "SECRET" || strings.Contains(in.URL, "SECRET") || !strings.Contains(in.URL, "apikey=***") {
			t.Fatalf("unsafe helper request %+v", in)
		}
		return Reply{Torrent: "ZDE6YWkxZWU="}, nil
	}
	link, err := s.resolveCached(context.Background(), "1:a", Cached{Source: Source{SourceInput: SourceInput{Kind: "torznab", URL: "https://jackett.example/api", Values: map[string]string{"apikey": "SECRET"}}}, Hit: Hit{Link: "https://jackett.example/download?apikey=***"}})
	if err != nil || !strings.HasPrefix(link, "data:application/x-bittorrent;base64,") || strings.Contains(link, "apikey") {
		t.Fatalf("%s %v", link, err)
	}
}

func TestRedactURLCoversQueryVariantsAndLeavesNonSecrets(t *testing.T) {
	for _, raw := range []string{"https://x.example/dl?APIKEY=SECRET&file=1", "https://x.example/dl?jackett_apikey=SECRET", "https://x.example/dl?api_key=SECRET", "https://x.example/dl?passkey=SECRET"} {
		got := RedactURL(raw)
		if strings.Contains(got, "SECRET") || !strings.Contains(got, "***") {
			t.Fatal(got)
		}
	}
}
