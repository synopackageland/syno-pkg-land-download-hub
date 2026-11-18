package search

import (
	"context"
	"sync"
)

// SearchEach stores and emits one source at a time, independent of siblings.
// Only one callback runs at once; a failed transport cancels outstanding work.
func (s *Service) SearchEach(ctx context.Context, owner string, b Batch, emit func(SourcePage) error) error {
	return s.SearchEachBatch(ctx, owner, b, func(pages []SourcePage) error {
		for _, p := range pages {
			if err := emit(p); err != nil {
				return err
			}
		}
		return nil
	})
}

// SearchEachBatch emits sources as they finish. Sources that answer while an
// earlier group is still being stored are stored and emitted together, in one
// state write: on a NAS whose disk takes seconds per write, a write per source
// made each answer wait behind every source that answered before it.
// Only one callback runs at once; a failed transport cancels outstanding work.
func (s *Service) SearchEachBatch(ctx context.Context, owner string, b Batch, emit func([]SourcePage) error) error {
	cats, known := CategoryIDs(b.Category)
	if len(b.SourceIDs) < 1 || len(b.SourceIDs) > 64 || b.Page < 1 || b.Page > 100 || len(b.Text) > 256 || !known {
		return fail("invalid_request")
	}
	seen := map[string]bool{}
	for _, id := range b.SourceIDs {
		if !validID(id) || seen[id] {
			return fail("invalid_request")
		}
		seen[id] = true
	}
	resultCtx := ctx
	ctx, cancel, solver, settingErr := s.networkContext(ctx, owner)
	defer cancel()
	if settingErr != nil {
		return settingErr
	}
	srcs, err := s.searchable(ctx, owner, b.SourceIDs)
	if err != nil {
		return err
	}
	type done struct {
		index int
		reply Reply
		code  string
	}
	var mu sync.Mutex
	var transportErr error
	finish := func(group []done) error {
		mu.Lock()
		defer mu.Unlock()
		if transportErr != nil {
			return transportErr
		}
		items, at := []keepItem{}, []int{}
		pages := make([]SourcePage, len(group))
		for n, d := range group {
			pages[n] = SourcePage{Page: Page{SiteURL: d.reply.SiteURL, SourceID: b.SourceIDs[d.index], Page: b.Page, Results: []ResultView{}}}
			if d.code == "" {
				items, at = append(items, keepItem{srcs[d.index].Source, b.Page, d.reply}), append(at, n)
			} else {
				pages[n].Error = &SourceError{Code: d.code}
			}
		}
		if len(items) > 0 {
			for k, r := range s.keepMany(resultCtx, owner, items) {
				pages[at[k]].Page = r.Page
				if code := errCode(r.err); code != "" {
					pages[at[k]].Error = &SourceError{Code: code}
				}
			}
		}
		if err := emit(pages); err != nil {
			transportErr = err
			cancel()
			return err
		}
		return nil
	}
	one := func(i int, rep Reply, code string) error { return finish([]done{{i, rep, code}}) }
	runnable := []int{}
	refused := []done{}
	for i, c := range srcs {
		if c.code != "" {
			refused = append(refused, done{i, Reply{}, c.code})
		} else {
			runnable = append(runnable, i)
		}
	}
	if len(refused) > 0 {
		if err := finish(refused); err != nil {
			return err
		}
	}
	held, err := s.slots(ctx, len(runnable))
	if err != nil {
		return err
	}
	defer func() {
		for _, release := range held {
			release()
		}
	}()
	busy := []done{}
	for _, i := range runnable[len(held):] {
		busy = append(busy, done{i, Reply{}, "search_busy"})
	}
	if len(busy) > 0 {
		if err := finish(busy); err != nil {
			return err
		}
	}
	runnable = runnable[:len(held)]
	var wg sync.WaitGroup
	network, requests := []int{}, []Request{}
	var bundle *Bundle
	for _, i := range runnable {
		src := srcs[i].Source
		if video(src) {
			wg.Add(1)
			go func(i int, src Source) {
				defer wg.Done()
				if s.Video == nil {
					one(i, Reply{}, "search_tool_missing")
					return
				}
				rep, err := s.Video(ctx, searchRequest(src, "", Query{Text: b.Text, Page: b.Page, Categories: cats}))
				one(i, rep, errCode(err))
			}(i, src)
			continue
		}
		raw, err := s.batchDefinition(ctx, &bundle, src)
		if err != nil {
			one(i, Reply{}, Code(err))
			continue
		}
		network = append(network, i)
		requests = append(requests, searchRequest(src, raw, Query{Text: b.Text, Page: b.Page, Categories: cats}))
	}
	if len(network) > 0 {
		answered := map[int]bool{}
		type completion struct {
			index int
			reply Reply
		}
		// Drain the bounded helper stream independently of local persistence,
		// browser planning and UI publication; those must not spend network time.
		pending := make(chan completion, len(network))
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for row := range pending {
				group := []completion{row}
			more:
				for {
					select {
					case r, ok := <-pending:
						if !ok {
							break more
						}
						group = append(group, r)
					default:
						break more
					}
				}
				stored := []done{}
				for _, c := range group {
					if c.reply.Code == "search_cloudflare" && solver != "" {
						wg.Add(1)
						go func(n int) {
							defer wg.Done()
							solved, e := s.retrySolver(ctx, requests[n], solver)
							one(network[n], solved, errCode(e))
						}(c.index)
						continue
					}
					stored = append(stored, done{network[c.index], c.reply, ""})
				}
				if len(stored) > 0 && finish(stored) != nil {
					cancel()
				}
			}
		}()
		accept := func(n int, rep Reply) error {
			if n < 0 || n >= len(network) || answered[n] {
				return fail("search_helper")
			}
			answered[n] = true
			select {
			case pending <- completion{n, rep}:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		if s.Stream != nil {
			err = s.Stream(ctx, Request{Op: "batch.stream", Searches: requests}, accept)
		} else {
			err = fail("search_helper")
		}
		close(pending)
		<-drained
		if err == nil && len(answered) != len(network) {
			err = fail("search_helper")
		}
		if err != nil {
			missing := []done{}
			for n, i := range network {
				if !answered[n] {
					missing = append(missing, done{i, Reply{}, Code(err)})
				}
			}
			if len(missing) > 0 {
				finish(missing)
			}
		}
	}
	wg.Wait()
	return transportErr
}
