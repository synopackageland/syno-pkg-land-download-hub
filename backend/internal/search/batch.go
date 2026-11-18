package search

import (
	"context"
	"sync"
)

// Batch searches several of the owner's sources for one query and page.
type Batch struct {
	SourceIDs []string `json:"sourceIds"`
	Text      string   `json:"query"`
	Page      int      `json:"page"`
	Category  string   `json:"category,omitempty"`
}
type SourceError struct {
	Code string `json:"code"`
}

// SourcePage is one source's page, or the code that ended its search.
type SourcePage struct {
	Status string `json:"status,omitempty"`
	Page
	Error *SourceError `json:"error,omitempty"`
}

const batchMax = 16

// SearchBatch searches every listed source at once. Cardigann, Torznab and
// RSS sources share one helper process, which runs each in its own goroutine
// under its own deadline; each yt-dlp source runs its own process beside it.
// A source that gets no package slot answers search_busy; the others run.
func (s *Service) SearchBatch(ctx context.Context, owner string, b Batch) ([]SourcePage, error) {
	ctx, cancel, solver, settingErr := s.networkContext(ctx, owner)
	defer cancel()
	if settingErr != nil {
		return nil, settingErr
	}
	cats, known := CategoryIDs(b.Category)
	if len(b.SourceIDs) < 1 || len(b.SourceIDs) > batchMax || b.Page < 1 || b.Page > 100 || len(b.Text) > 256 || !known {
		return nil, fail("invalid_request")
	}
	seen := map[string]bool{}
	for _, id := range b.SourceIDs {
		if !validID(id) || seen[id] {
			return nil, fail("invalid_request")
		}
		seen[id] = true
	}
	srcs, err := s.searchable(ctx, owner, b.SourceIDs)
	if err != nil {
		return nil, err
	}
	q := Query{Text: b.Text, Page: b.Page, Categories: cats}
	out := make([]SourcePage, len(srcs))
	codes := make([]string, len(srcs))
	replies := make([]Reply, len(srcs))
	runnable := []int{}
	for i, c := range srcs {
		out[i] = SourcePage{Page: Page{SourceID: c.ID, Page: b.Page, Results: []ResultView{}}}
		codes[i] = c.code
		if c.code == "" && video(c.Source) && s.Video == nil {
			codes[i] = "search_tool_missing"
		}
		if codes[i] == "" {
			runnable = append(runnable, i)
		}
	}
	held, err := s.slots(ctx, len(runnable))
	if err != nil {
		return nil, err
	}
	defer func() {
		for _, release := range held {
			release()
		}
	}()
	for _, i := range runnable[len(held):] {
		codes[i] = "search_busy"
	}
	runnable = runnable[:len(held)]
	var bundle *Bundle
	network, requests := []int{}, []Request{}
	var wg sync.WaitGroup
	for _, i := range runnable {
		src := srcs[i].Source
		if video(src) {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				rep, err := s.Video(ctx, searchRequest(src, "", q))
				replies[i], codes[i] = rep, errCode(err)
			}(i)
			continue
		}
		raw, err := s.batchDefinition(ctx, &bundle, src)
		if err != nil {
			codes[i] = Code(err)
			continue
		}
		network, requests = append(network, i), append(requests, searchRequest(src, raw, q))
	}
	if len(network) > 0 {
		rep, err := s.Run(ctx, Request{Op: "batch", Searches: requests})
		if err == nil && rep.Code == "" && len(rep.Batch) != len(network) {
			err = fail("search_helper")
		}
		for n, i := range network {
			switch {
			case err != nil:
				codes[i] = Code(err)
			case rep.Code != "":
				codes[i] = rep.Code
			default:
				replies[i] = rep.Batch[n]
				if rep.Batch[n].Code == "search_cloudflare" && solver != "" {
					wg.Add(1)
					go func(i, n int) {
						defer wg.Done()
						var e error
						replies[i], e = s.retrySolver(ctx, requests[n], solver)
						if e != nil {
							codes[i] = Code(e)
						}
					}(i, n)
				}
			}
		}
	}
	wg.Wait()
	for i := range out {
		if codes[i] == "" {
			out[i].Page, err = s.keep(ctx, owner, srcs[i].Source, b.Page, replies[i])
			codes[i] = errCode(err)
		}
		if codes[i] != "" {
			out[i].Error = &SourceError{codes[i]}
		}
	}
	return out, nil
}
func errCode(err error) string {
	if err == nil {
		return ""
	}
	return Code(err)
}

// A batch reads the definition bundle at most once.
func (s *Service) batchDefinition(ctx context.Context, bundle **Bundle, src Source) (string, error) {
	if src.Kind != "builtin" {
		return s.definition(ctx, src)
	}
	if *bundle == nil {
		b, err := s.bundle(ctx)
		if err != nil {
			return "", err
		}
		*bundle = &b
	}
	return definitionIn(**bundle, src)
}
