package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBatchEmitsFastBeforeFifteenSecondSibling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		delay := 200 * time.Millisecond
		if r.URL.Path == "/slow" {
			delay = 15 * time.Second
		}
		select {
		case <-time.After(delay):
			w.Write([]byte(`<rss version="2.0"><channel><item><title>X</title><link>magnet:?xt=x</link></item></channel></rss>`))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	seen := make(chan int, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		batchEach(ctx, []request{{Op: "search", Kind: "rss", URL: server.URL + "/slow"}, {Op: "search", Kind: "rss", URL: server.URL + "/fast"}}, func(bool) *http.Client { return server.Client() }, 20*time.Second, func(i int, r reply) {
			if r.Code == "" {
				seen <- i
			}
		})
	}()
	select {
	case i := <-seen:
		if i != 1 {
			t.Fatal("wrong source")
		}
	case <-time.After(time.Second):
		t.Fatal("fast source blocked")
	}
	cancel()
	<-done
}
