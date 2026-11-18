package search

import (
	"context"
	"testing"
	"time"
)

// A slow source stays active for fifteen seconds. Its sibling's result must
// be stored and emitted in <1s, and keep its opaque result usable immediately.
func TestStreamFastSourceIsUsableBeforeSlowSibling(t *testing.T) {
	s := fixtureService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	a, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "Slow", URL: "https://example.org/slow", Enabled: true})
	b, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "Fast", URL: "https://example.org/fast", Enabled: true})
	s.Stream = func(ctx context.Context, in Request, emit func(int, Reply) error) error {
		if in.Op != "batch.stream" || len(in.Searches) != 2 {
			t.Errorf("%+v", in)
		}
		time.Sleep(200 * time.Millisecond)
		if err := emit(1, Reply{Results: []Hit{hit("Fast")}}); err != nil {
			return err
		}
		select {
		case <-time.After(15 * time.Second):
			return emit(0, Reply{Results: []Hit{hit("Slow")}})
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	start := time.Now()
	seen := make(chan SourcePage, 2)
	done := make(chan error, 1)
	go func() {
		done <- s.SearchEach(ctx, "1:a", Batch{SourceIDs: []string{a.ID, b.ID}, Page: 1}, func(p SourcePage) error { seen <- p; return nil })
	}()
	select {
	case p := <-seen:
		if time.Since(start) >= time.Second || p.SourceID != b.ID || len(p.Results) != 1 {
			t.Fatalf("late/wrong page: %+v", p)
		}
		if _, err := s.Result(ctx, "1:a", p.Results[0].ResultID); err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("fast source blocked by slow sibling")
	}
	cancel()
	<-done
}

func TestStreamDrainsRepliesWhileResultConsumerIsBusy(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	a, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "A", URL: "https://example.org/a", Enabled: true})
	b, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: "B", URL: "https://example.org/b", Enabled: true})
	drained := make(chan struct{})
	release := make(chan struct{})
	s.Stream = func(ctx context.Context, in Request, emit func(int, Reply) error) error {
		if err := emit(0, Reply{Results: []Hit{hit("A")}}); err != nil {
			return err
		}
		if err := emit(1, Reply{Results: []Hit{hit("B")}}); err != nil {
			return err
		}
		close(drained)
		return nil
	}
	done := make(chan error, 1)
	count := 0
	go func() {
		done <- s.SearchEach(ctx, "1:a", Batch{SourceIDs: []string{a.ID, b.ID}, Page: 1}, func(p SourcePage) error {
			count++
			if count == 1 {
				<-release
			}
			return nil
		})
	}()
	select {
	case <-drained:
	case <-time.After(300 * time.Millisecond):
		t.Error("result persistence/browser planning blocked helper reply draining")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatal(count)
	}
}

// Sources that answer while an earlier group is still being handled are
// stored and emitted together, each with its own usable results.
func TestStreamGroupsAnswersThatArriveWhileBusy(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	ids := []string{}
	for _, n := range []string{"a", "b", "c"} {
		src, _ := s.SaveSource(ctx, "1:a", SourceInput{Kind: "rss", Name: n, URL: "https://example.org/" + n, Enabled: true})
		ids = append(ids, src.ID)
	}
	release := make(chan struct{})
	s.Stream = func(ctx context.Context, in Request, emit func(int, Reply) error) error {
		for i, n := range []string{"a", "b", "c"} {
			if err := emit(i, Reply{Results: []Hit{hit(n)}}); err != nil {
				return err
			}
		}
		close(release)
		return nil
	}
	groups := [][]SourcePage{}
	err := s.SearchEachBatch(ctx, "1:a", Batch{SourceIDs: ids, Page: 1}, func(g []SourcePage) error {
		if len(groups) == 0 {
			<-release
		}
		groups = append(groups, g)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, g := range groups {
		for _, p := range g {
			if p.Error != nil || len(p.Results) != 1 {
				t.Fatalf("%+v", p)
			}
			if _, err := s.Result(ctx, "1:a", p.Results[0].ResultID); err != nil {
				t.Fatal(err)
			}
			total++
		}
	}
	if total != 3 || len(groups) > 2 {
		t.Fatalf("%d pages in %d groups", total, len(groups))
	}
}
