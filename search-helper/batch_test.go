package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const delayedFeed = `<rss version="2.0"><channel><item><title>Ubuntu</title><link>magnet:?xt=urn:btih:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa</link></item></channel></rss>`

// Ten sources that each answer after one second finish together, near one
// second, not one after another.
func TestBatchSearchesSourcesAtOnce(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		w.Write([]byte(delayedFeed))
	}))
	defer srv.Close()
	items := make([]request, 10)
	for i := range items {
		items[i] = request{Op: "search", Kind: "torznab", URL: srv.URL, Query: "Ubuntu", Page: 1}
	}
	started := time.Now()
	rep := batch(context.Background(), items, func(bool) *http.Client { return srv.Client() }, 5*time.Second)
	if elapsed := time.Since(started); elapsed > 1800*time.Millisecond {
		t.Fatalf("ten 1 s sources took %v", elapsed)
	}
	if len(rep.Batch) != 10 {
		t.Fatalf("%+v", rep)
	}
	for _, r := range rep.Batch {
		if r.Code != "" || len(r.Results) != 1 {
			t.Fatalf("%+v", r)
		}
	}
}

// A source past its own deadline answers search_timeout; the others keep
// their results, in the order they were asked.
func TestBatchSlowSourceTimesOutAlone(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("slow") == "1" {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		w.Write([]byte(delayedFeed))
	}))
	defer srv.Close()
	items := []request{
		{Op: "search", Kind: "rss", URL: srv.URL + "/?slow=1", Page: 1},
		{Op: "search", Kind: "rss", URL: srv.URL, Page: 1},
	}
	started := time.Now()
	rep := batch(context.Background(), items, func(bool) *http.Client { return srv.Client() }, 300*time.Millisecond)
	if time.Since(started) > 2*time.Second {
		t.Fatal("the slow source held up the batch")
	}
	if rep.Batch[0].Code != "search_timeout" || rep.Batch[1].Code != "" || len(rep.Batch[1].Results) != 1 {
		t.Fatalf("%+v", rep.Batch)
	}
}

// At most batchLimit sources connect at once.
func TestBatchBoundsConcurrentSources(t *testing.T) {
	active, peak := make(chan int, 1), 0
	active <- 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := <-active + 1
		if n > peak {
			peak = n
		}
		active <- n
		time.Sleep(100 * time.Millisecond)
		active <- <-active - 1
		w.Write([]byte(delayedFeed))
	}))
	defer srv.Close()
	items := make([]request, batchLimit+4)
	for i := range items {
		items[i] = request{Op: "search", Kind: "rss", URL: srv.URL, Page: 1}
	}
	batch(context.Background(), items, func(bool) *http.Client { return srv.Client() }, 5*time.Second)
	if peak > batchLimit || peak < 2 {
		t.Fatalf("peak %d", peak)
	}
}

// A batch carries only searches; nested batches and other ops are refused.
func TestBatchRefusesOtherOps(t *testing.T) {
	rep := batch(context.Background(), []request{{Op: "download"}, {Op: "batch"}}, func(bool) *http.Client { return http.DefaultClient }, time.Second)
	if rep.Batch[0].Code != "invalid_request" || rep.Batch[1].Code != "invalid_request" {
		t.Fatalf("%+v", rep.Batch)
	}
}
