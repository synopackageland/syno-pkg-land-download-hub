package gopeed

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
)

const maxSearchArtifact = 16 << 20
const maxSearchArtifacts = 64 << 20
const searchArtifactLifetime = time.Hour

// This is only bounded result data, not a queue or a job-state journal. The
// SDK owns all admission/state/retention authority. No input, credential or path
// is returned as an artifact handle. Each read reauthorizes the original job.
func (s *Service) searchPagesDir() string { return filepath.Join(s.Data, "search-job-results") }

type searchArtifact struct {
	StartedAt time.Time           `json:"startedAt"`
	Pages     []search.SourcePage `json:"pages"`
}

func (s *Service) writeSearchPages(ctx context.Context, id string, startedAt time.Time, pages []search.SourcePage) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if !validCtlID(id) {
		return refused("invalid_request")
	}
	artifact := searchArtifact{StartedAt: startedAt, Pages: pages}
	raw, err := json.Marshal(artifact)
	if err != nil || len(raw) > maxSearchArtifact {
		return refused("search_output_limit")
	}
	dir := s.searchPagesDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return refused("search_storage")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return refused("search_storage")
	}
	// A single SDK runner writes this bounded store. Eviction affects only result
	// data and never deletes SDK records or changes the outcome of an operation.
	entries, err := os.ReadDir(dir)
	if err != nil {
		return refused("search_storage")
	}
	type retained struct {
		name string
		size int64
		at   time.Time
	}
	var files []retained
	var bytes int64
	for _, entry := range entries {
		if entry.Name() == id+".json" {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return refused("search_storage")
		}
		if time.Since(info.ModTime()) > searchArtifactLifetime {
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return refused("search_storage")
			}
			continue
		}
		files = append(files, retained{entry.Name(), info.Size(), info.ModTime()})
		bytes += info.Size()
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	retainedCount := len(files)
	for _, f := range files {
		if bytes+int64(len(raw)) <= maxSearchArtifacts && retainedCount < 64 {
			break
		}
		if err := os.Remove(filepath.Join(dir, f.name)); err != nil {
			return refused("search_storage")
		}
		bytes -= f.size
		retainedCount--
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err := binaries.Atomic(filepath.Join(dir, id+".json"), artifact); err != nil {
		return refused("search_storage")
	}
	return nil
}
func (s *Service) searchJobPages(ctx context.Context, store *background.Store, access background.Access, id string, offset, limit int) (any, error) {
	j, err := store.GetFor(ctx, id, access)
	if err != nil {
		return nil, searchJobError(err)
	}
	if j.Name != "search" || offset < 0 || limit < 1 || limit > 100 {
		return nil, refused("invalid_request")
	}
	if j.State != background.StateRunning && j.State != background.StateSucceeded {
		return nil, refused("search_result_unavailable")
	}
	if j.CancelRequested {
		return nil, refused("search_result_unavailable")
	}
	if j.State == background.StateSucceeded {
		if _, err := store.ResultFor(ctx, id, access); err != nil {
			return nil, searchJobError(err)
		}
	}
	root, err := os.OpenRoot(s.searchPagesDir())
	if err != nil {
		return nil, refused("search_result_expired")
	}
	defer root.Close()
	f, err := root.Open(id + ".json")
	if err != nil {
		return nil, refused("search_result_expired")
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil || !stat.Mode().IsRegular() || stat.Size() > maxSearchArtifact || time.Since(stat.ModTime()) > searchArtifactLifetime {
		return nil, refused("search_result_expired")
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxSearchArtifact+1))
	if err != nil || len(raw) > maxSearchArtifact {
		return nil, refused("search_storage")
	}
	var artifact searchArtifact
	if json.Unmarshal(raw, &artifact) != nil {
		return nil, refused("search_storage")
	}
	if !artifact.StartedAt.Equal(j.StartedAt) {
		return nil, refused("search_result_unavailable")
	}
	pages := artifact.Pages
	owner, e := s.Owner(access.Owner.User)
	if e != nil {
		return nil, refused("identity_unavailable")
	}
	browser, e := s.searchService().BrowserPages(ctx, owner, id, j.StartedAt)
	if e != nil {
		return nil, searchRefusal(e)
	}
	for i, p := range pages {
		if b, ok := browser[p.SourceID]; ok {
			pages[i] = b
		} else if p.Error != nil && p.Error.Code == "search_cloudflare" {
			pages[i].Status = "needs_browser"
		}
	}

	if offset > len(pages) {
		return nil, refused("invalid_request")
	}
	end := offset + min(limit, len(pages)-offset)
	out := map[string]any{"items": pages[offset:end], "offset": offset, "total": len(pages)}
	if end < len(pages) {
		out["nextOffset"] = end
	}
	return out, nil
}
