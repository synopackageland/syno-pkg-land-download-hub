package search

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSolverOwnerSettingsValidationAndRetry(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	for _, bad := range []string{"file:///tmp/a", "http://u:p@host/", "http://host/?secret=x", "http://host/#f", "http://host/path"} {
		if err := s.SetSolver(ctx, "alice", bad); Code(err) != "invalid_request" {
			t.Fatalf("accepted %s", bad)
		}
	}
	if err := s.SetSolver(ctx, "alice", "http://solver.lan:8191"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Solver(ctx, "bob"); v != "" {
		t.Fatal("foreign setting")
	}
	src, _ := s.SaveSource(ctx, "alice", SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	attempts := 0
	s.Run = func(c context.Context, in Request) (Reply, error) {
		attempts++
		if attempts == 1 {
			if in.SolverURL != "" {
				t.Fatal("not challenge only")
			}
			return Reply{Code: "search_cloudflare"}, nil
		}
		if in.SolverURL != "http://solver.lan:8191/v1" {
			t.Fatal(in)
		}
		deadline, _ := c.Deadline()
		if time.Until(deadline) < 50*time.Second {
			t.Fatal("solver got short deadline")
		}
		return Reply{Results: []Hit{{Title: "Ubuntu", Link: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}}}, nil
	}
	if _, err := s.Search(ctx, "alice", Query{SourceID: src.ID, Page: 1}); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatal(attempts)
	}
	raw, _ := json.Marshal(src)
	if strings.Contains(string(raw), "solver.lan") {
		t.Fatal("setting leak")
	}
}

func TestSolverRetryDoesNotBlockFastStreamAndRespectsCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := fixtureService(t)
	s.SetSolver(ctx, "alice", "http://solver.lan:8191")
	a, _ := s.SaveSource(ctx, "alice", SourceInput{Kind: "rss", Name: "Blocked", URL: "https://example.org/blocked", Enabled: true})
	b, _ := s.SaveSource(ctx, "alice", SourceInput{Kind: "rss", Name: "Fast", URL: "https://example.org/fast", Enabled: true})
	started := make(chan struct{})
	s.Run = func(c context.Context, in Request) (Reply, error) {
		if in.SolverURL == "" {
			t.Error("expected only solver runner")
		}
		close(started)
		<-c.Done()
		return Reply{}, c.Err()
	}
	s.Stream = func(c context.Context, in Request, emit func(int, Reply) error) error {
		emit(0, Reply{Code: "search_cloudflare"})
		<-started
		return emit(1, Reply{Results: []Hit{{Title: "Fast", Link: "magnet:?xt=urn:btih:" + strings.Repeat("a", 40)}}})
	}
	gotFast := false
	err := s.SearchEach(ctx, "alice", Batch{SourceIDs: []string{a.ID, b.ID}, Page: 1}, func(p SourcePage) error {
		if p.SourceID == b.ID {
			gotFast = true
			cancel()
		}
		return nil
	})
	if err != nil || !gotFast {
		t.Fatal("solver held fast result", err)
	}
}
