package search

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func hit(title string) Hit {
	return Hit{Title: title, Link: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}
}

// Network sources share one helper; each video source runs its own yt-dlp.
// Every source answers on its own, in the order asked.
func TestBatchSharesOneHelperAndKeepsEachSourcesAnswer(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	var ids []string
	for _, kind := range []string{"rss", "torznab", "rss"} {
		src, err := s.SaveSource(ctx, "1:a", SourceInput{Kind: kind, Name: kind, URL: "https://example.org/feed", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, src.ID)
	}
	yt, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "ytsearch", Name: "YouTube", Enabled: true})
	off, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "Off", URL: "https://example.org/feed", Enabled: false})
	ids = append(ids, yt.ID, off.ID, strings.Repeat("0", 32))
	var batches, videos int
	var mu sync.Mutex
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		mu.Lock()
		batches++
		mu.Unlock()
		if in.Op != "batch" || len(in.Searches) != 3 || in.Searches[0].Query != "ubuntu" {
			t.Errorf("%+v", in)
		}
		return Reply{Batch: []Reply{{Results: []Hit{hit("One")}}, {Code: "search_login"}, {Results: []Hit{hit("Three")}}}}, nil
	}
	s.Video = func(_ context.Context, in Request) (Reply, error) {
		mu.Lock()
		videos++
		mu.Unlock()
		return Reply{Results: []Hit{hit("Video")}}, nil
	}
	pages, err := s.SearchBatch(ctx, "1:a", Batch{SourceIDs: ids, Text: "ubuntu", Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	if batches != 1 || videos != 1 || len(pages) != 6 {
		t.Fatalf("batches %d videos %d pages %d", batches, videos, len(pages))
	}
	want := []string{"", "search_login", "", "", "search_source_disabled", "search_source_missing"}
	for i, p := range pages {
		code := ""
		if p.Error != nil {
			code = p.Error.Code
		}
		if p.SourceID != ids[i] || code != want[i] || p.Results == nil {
			t.Fatalf("page %d: %+v", i, p)
		}
	}
	if pages[0].Results[0].Title != "One" || pages[3].Results[0].Title != "Video" {
		t.Fatalf("%+v", pages)
	}
	if _, err := s.Result(ctx, "1:a", pages[2].Results[0].ResultID); err != nil {
		t.Fatal(err)
	}
}

// Five video sources and the helper batch, each taking one second, finish
// together near one second.
func TestBatchRunsSourcesConcurrently(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	var ids []string
	for i := 0; i < 5; i++ {
		src, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "bilisearch", Name: "Bili", Enabled: true})
		ids = append(ids, src.ID)
	}
	rss, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "RSS", URL: "https://example.org/feed", Enabled: true})
	ids = append(ids, rss.ID)
	slow := func(_ context.Context, in Request) (Reply, error) {
		time.Sleep(time.Second)
		if in.Op == "batch" {
			return Reply{Batch: []Reply{{Results: []Hit{hit("Feed")}}}}, nil
		}
		return Reply{Results: []Hit{hit("Video")}}, nil
	}
	s.Run, s.Video = slow, slow
	started := time.Now()
	pages, err := s.SearchBatch(ctx, "1:a", Batch{SourceIDs: ids, Text: "x", Page: 1})
	if elapsed := time.Since(started); err != nil || elapsed > 1800*time.Millisecond {
		t.Fatalf("six 1 s sources took %v: %v", elapsed, err)
	}
	for _, p := range pages {
		if p.Error != nil {
			t.Fatalf("%+v", p)
		}
	}
}

// Up to slotCount sources search at once across the package; a source that finds no
// slot within the wait answers search_busy and the rest still run.
func TestBatchSharesPackageSlots(t *testing.T) {
	s := fixtureService(t)
	s.SlotWait = 50 * time.Millisecond
	ctx := context.Background()
	held, err := s.slots(ctx, slotCount-2)
	if err != nil || len(held) != slotCount-2 {
		t.Fatalf("%d %v", len(held), err)
	}
	defer func() {
		for _, r := range held {
			r()
		}
	}()
	var ids []string
	for i := 0; i < 4; i++ {
		src, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "RSS", URL: "https://example.org/feed", Enabled: true})
		ids = append(ids, src.ID)
	}
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		out := Reply{}
		for range in.Searches {
			out.Batch = append(out.Batch, Reply{Results: []Hit{hit("x")}})
		}
		if len(in.Searches) != 2 {
			t.Errorf("ran %d searches with two free slots", len(in.Searches))
		}
		return out, nil
	}
	pages, err := s.SearchBatch(ctx, "1:a", Batch{SourceIDs: ids, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	busy := 0
	for _, p := range pages {
		if p.Error != nil && p.Error.Code == "search_busy" {
			busy++
		}
	}
	if busy != 2 {
		t.Fatalf("%+v", pages)
	}
}

func TestBatchRefusesAnInvalidSourceList(t *testing.T) {
	s := fixtureService(t)
	id := strings.Repeat("a", 32)
	many := make([]string, 17)
	for i := range many {
		many[i] = strings.Repeat(string(rune('a'+i%6)), 31) + string(rune('a'+i/6))
	}
	for _, b := range []Batch{{Page: 1}, {SourceIDs: []string{id, id}, Page: 1}, {SourceIDs: []string{"x"}, Page: 1}, {SourceIDs: many, Page: 1}, {SourceIDs: []string{id}, Page: 0}} {
		if _, err := s.SearchBatch(context.Background(), "1:a", b); Code(err) != "invalid_request" {
			t.Fatalf("%+v: %v", b, err)
		}
	}
}

// A category reaches the helper as Newznab IDs; no category sends none.
func TestBatchSendsTheCategoryAsNewznabIDs(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	src, err := s.SaveSource(ctx, "1:a", SourceInput{Kind: "torznab", Name: "T", URL: "https://example.org/api", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var got [][]int
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		for _, x := range in.Searches {
			got = append(got, x.Categories)
		}
		return Reply{Batch: []Reply{{}}}, nil
	}
	for _, c := range []string{"series", ""} {
		if _, err := s.SearchBatch(ctx, "1:a", Batch{SourceIDs: []string{src.ID}, Text: "x", Page: 1, Category: c}); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 2 || len(got[0]) != 1 || got[0][0] != 5000 || got[1] != nil {
		t.Fatalf("%v", got)
	}
	if _, err := s.SearchBatch(ctx, "1:a", Batch{SourceIDs: []string{src.ID}, Text: "x", Page: 1, Category: "xxx"}); err == nil {
		t.Fatal("unknown category accepted")
	}
}

func TestCategoryIDs(t *testing.T) {
	for name, want := range map[string]int{"movie": 2000, "music": 3000, "software": 4000, "series": 5000, "anime": 5070, "book": 7000, "other": 8000} {
		ids, ok := CategoryIDs(name)
		if !ok || len(ids) != 1 || ids[0] != want {
			t.Errorf("%s: %v %v", name, ids, ok)
		}
	}
	if ids, ok := CategoryIDs(""); !ok || ids != nil {
		t.Errorf("empty: %v %v", ids, ok)
	}
}
