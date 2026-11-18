package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// One helper searches a batch of sources together: one process and two
// shared HTTP clients instead of a process per source. At most batchLimit
// sources connect at once; each has its own deadline from when it starts,
// so a slow source answers search_timeout without holding up the others.
const (
	batchLimit     = 64
	batchMax       = 64
	sourceDeadline = 20 * time.Second
)

func batch(ctx context.Context, items []request, client func(lan bool) *http.Client, perSource time.Duration) reply {
	out := reply{Batch: make([]reply, len(items))}
	batchEach(ctx, items, client, perSource, func(i int, r reply) { out.Batch[i] = r })
	return out
}

// emit must permit concurrent calls, one for each completed source.
func batchEach(ctx context.Context, items []request, client func(bool) *http.Client, perSource time.Duration, emit func(int, reply)) {
	slots := make(chan struct{}, batchLimit)
	var wg sync.WaitGroup
	for i, in := range items {
		if in.Op != "search" {
			emit(i, reply{Code: "invalid_request"})
			continue
		}
		wg.Add(1)
		go func(i int, in request) {
			defer wg.Done()
			select {
			case slots <- struct{}{}:
				defer func() { <-slots }()
			case <-ctx.Done():
				emit(i, reply{Code: "search_timeout"})
				return
			}
			sctx, cancel := context.WithTimeout(ctx, perSource)
			defer cancel()
			r := run(sctx, in, client(in.Kind == "torznab"))
			if sctx.Err() != nil {
				r = reply{Code: "search_timeout"}
			}
			emit(i, r)
		}(i, in)
	}
	wg.Wait()
}

// The clients are shared by every source of a batch; Torznab may reach the LAN.
func batchClients() func(lan bool) *http.Client {
	lan, wan := newClient(true), newClient(false)
	return func(l bool) *http.Client {
		if l {
			return lan
		}
		return wan
	}
}
