package search

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1" // Git object identity, checked against pinned-host tree metadata.
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
)

var generationPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var commitPattern = regexp.MustCompile(`^[a-f0-9]{40}$`)

const second = time.Second

func (s *Service) fetch(ctx context.Context, address string, max int64) ([]byte, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || (u.Host != "api.github.com" && u.Host != "codeload.github.com") {
		return nil, fail("search_update_source")
	}
	client := s.HTTP
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	clone := *client
	clone.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Download-Hub-definition-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	githubtoken.Authorize(req, s.GitHub.Load())
	res, err := clone.Do(req)
	if err != nil {
		return nil, fail("search_update_network")
	}
	defer res.Body.Close()
	if res.StatusCode == 401 || res.StatusCode == 403 || res.StatusCode == 429 {
		err = binaries.SourceError(res)
		var rate *binaries.RateLimitError
		switch {
		case errors.As(err, &rate):
			return nil, rateLimited(rate.Reset)
		case errors.Is(err, githubtoken.ErrInvalid):
			return nil, fmt.Errorf("%w: %w", fail("github_token_invalid"), err)
		case res.StatusCode == 403:
			return nil, fail("search_update_forbidden")
		}
	}
	if res.StatusCode != 200 {
		return nil, fail("search_update_network")
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, fail("search_update_invalid")
	}
	return b, nil
}
func (s *Service) Update(ctx context.Context) (Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	unlock, err := lock(ctx, filepath.Join(s.Data, "update.lock"))
	if err != nil {
		return Catalog{}, err
	}
	defer unlock()
	// Manual update checks share a NAS-wide cache across CGI processes and
	// users. Opening Settings only reads the installed catalog, never GitHub.
	cachePath := filepath.Join(s.Data, "update-check.json")
	// A limit GitHub set for anonymous calls does not hold back a new token.
	token := s.GitHub.Load() != ""
	var cached struct {
		Until   time.Time
		Limited bool
		Token   bool
	}
	if readJSON(cachePath, &cached, 4096) == nil && time.Now().Before(cached.Until) && cached.Token == token {
		if cached.Limited {
			return Catalog{}, rateLimited(cached.Until)
		}
		return s.Catalog(ctx)
	}
	out, err := s.update(ctx)
	var rate *binaries.RateLimitError
	if errors.As(err, &rate) {
		cached.Until, cached.Limited, cached.Token = rate.Reset, true, token
		if b, e := json.Marshal(cached); e == nil {
			_ = atomic(cachePath, b)
		}
		return Catalog{}, err
	}
	if err == nil {
		cached.Until, cached.Limited, cached.Token = time.Now().Add(time.Hour), false, token
		if b, e := json.Marshal(cached); e == nil {
			_ = atomic(cachePath, b)
		}
	}
	return out, err
}

// rateLimited is search_update_rate_limited that still says when GitHub
// resets the limit.
func rateLimited(reset time.Time) error {
	return fmt.Errorf("%w: %w", fail("search_update_rate_limited"), &binaries.RateLimitError{Reset: reset})
}

func (s *Service) update(ctx context.Context) (Catalog, error) {
	b, err := s.fetch(ctx, "https://api.github.com/repos/Prowlarr/Indexers/commits/master", 1<<20)
	if err != nil {
		return Catalog{}, err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if json.Unmarshal(b, &commit) != nil || !commitPattern.MatchString(commit.SHA) {
		return Catalog{}, fail("search_update_invalid")
	}
	b, err = s.fetch(ctx, "https://api.github.com/repos/Prowlarr/Indexers/git/trees/"+commit.SHA+"?recursive=1", 8<<20)
	if err != nil {
		return Catalog{}, err
	}
	var tree struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
			Mode string `json:"mode"`
		} `json:"tree"`
	}
	if json.Unmarshal(b, &tree) != nil || tree.Truncated {
		return Catalog{}, fail("search_update_invalid")
	}
	expected := map[string]string{}
	for _, e := range tree.Tree {
		if strings.HasPrefix(e.Path, "definitions/v11/") && strings.HasSuffix(e.Path, ".yml") {
			name := strings.TrimPrefix(e.Path, "definitions/v11/")
			if path.Base(name) != name || e.Type != "blob" || e.Mode != "100644" || !commitPattern.MatchString(e.SHA) {
				return Catalog{}, fail("search_update_invalid")
			}
			expected[name] = e.SHA
		}
	}
	if len(expected) < 1 || len(expected) > 2000 {
		return Catalog{}, fail("search_update_invalid")
	}
	b, err = s.fetch(ctx, "https://codeload.github.com/Prowlarr/Indexers/tar.gz/"+commit.SHA, 16<<20)
	if err != nil {
		return Catalog{}, err
	}
	defs, err := readArchive(b, expected)
	if err != nil {
		return Catalog{}, err
	}
	if err = s.install(ctx, Bundle{Commit: commit.SHA, Schema: 11, Definitions: defs}); err != nil {
		return Catalog{}, err
	}
	return s.Catalog(ctx)
}
func readArchive(archive []byte, expected map[string]string) (map[string]string, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fail("search_update_invalid")
	}
	defer gz.Close()
	contents, err := io.ReadAll(io.LimitReader(gz, (64<<20)+1))
	if err != nil || len(contents) > 64<<20 {
		return nil, fail("search_update_invalid")
	}
	tr := tar.NewReader(bytes.NewReader(contents))
	defs := map[string]string{}
	total := int64(0)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fail("search_update_invalid")
		}
		n := strings.TrimSuffix(h.Name, "/")
		if path.IsAbs(n) || path.Clean(n) != n || strings.Contains(n, "\\") {
			return nil, fail("search_update_invalid")
		}
		_, rest, ok := strings.Cut(n, "/")
		if !ok || !strings.HasPrefix(rest, "definitions/v11/") || !strings.HasSuffix(rest, ".yml") {
			continue
		}
		name := strings.TrimPrefix(rest, "definitions/v11/")
		sha, ok := expected[name]
		if !ok || h.Typeflag != tar.TypeReg || h.Size < 1 || h.Size > 256<<10 || defs[name] != "" {
			return nil, fail("search_update_invalid")
		}
		total += h.Size
		if total > 12<<20 {
			return nil, fail("search_update_invalid")
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, fail("search_update_invalid")
		}
		actual := fmt.Sprintf("%x", sha1.Sum(append([]byte(fmt.Sprintf("blob %d\x00", len(b))), b...)))
		if actual != sha {
			return nil, fail("search_update_hash")
		}
		defs[name] = string(b)
	}
	if len(defs) != len(expected) {
		return nil, fail("search_update_invalid")
	}
	return defs, nil
}
func (s *Service) install(ctx context.Context, b Bundle) error {
	if b.Schema != 11 || !commitPattern.MatchString(b.Commit) || len(b.Definitions) == 0 {
		return fail("search_update_invalid")
	}
	rep, err := s.Run(ctx, Request{Op: "catalog", Definitions: b.Definitions})
	if err != nil {
		return err
	}
	if rep.Code != "" || len(rep.Sites) != len(b.Definitions) {
		return fail("search_update_invalid")
	}
	ids := map[string]bool{}
	b.DefinitionFiles = map[string]string{}
	for _, site := range rep.Sites {
		if site.ID == "" || ids[site.ID] {
			return fail("search_update_invalid")
		}
		ids[site.ID] = true
		file := site.File
		if file == "" {
			file = site.ID + ".yml"
		}
		if _, ok := b.Definitions[file]; !ok {
			return fail("search_update_invalid")
		}
		b.DefinitionFiles[site.ID] = file
		// Syntactically invalid/schema-incompatible snapshots never replace current.
		// Recognized but unsupported runtime features remain visible and disabled.
		for _, reason := range site.Reasons {
			if reason == "invalid_yaml" || reason == "schema_or_engine_unsupported" || reason == "definition_too_large" {
				return fail("search_update_invalid")
			}
		}
	}
	current, err := s.bundle(ctx)
	if err != nil {
		return err
	}
	dir := filepath.Join(s.Data, "generations")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	save := func(value Bundle) (string, error) {
		raw, _ := json.Marshal(value)
		hash := digest(raw)
		record, _ := json.Marshal(struct {
			Bundle Bundle `json:"bundle"`
			Hash   string `json:"hash"`
		}{value, hash})
		return hash, atomic(filepath.Join(dir, hash+".json"), record)
	}
	previous, err := save(current)
	if err != nil {
		return err
	}
	next, err := save(b)
	if err != nil {
		return err
	}
	unlock, err := lock(ctx, filepath.Join(s.Data, "catalog.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	pointer, _ := json.Marshal(catalogState{next, previous})
	if next == previous {
		return nil
	}
	if err = atomic(filepath.Join(s.Data, "catalog.json"), pointer); err != nil {
		return err
	}
	// Retain precisely the current and previous generations; no user sources touched.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") && e.Name() != next+".json" && e.Name() != previous+".json" {
			os.Remove(filepath.Join(dir, e.Name()))
		}
	}
	return nil
}
func (s *Service) Rollback(ctx context.Context) error {
	unlock, err := lock(ctx, filepath.Join(s.Data, "update.lock"))
	if err != nil {
		return err
	}
	defer unlock()
	var st catalogState
	if readJSON(filepath.Join(s.Data, "catalog.json"), &st, 4096) != nil || !generationPattern.MatchString(st.Previous) {
		return fail("search_rollback_missing")
	}
	var stored struct {
		Bundle Bundle `json:"bundle"`
		Hash   string `json:"hash"`
	}
	if readJSON(filepath.Join(s.Data, "generations", st.Previous+".json"), &stored, 16<<20) != nil {
		return fail("search_catalog_invalid")
	}
	raw, _ := json.Marshal(stored.Bundle)
	if digest(raw) != st.Previous || stored.Hash != st.Previous {
		return fail("search_catalog_invalid")
	}
	pointer, _ := json.Marshal(catalogState{st.Previous, st.Current})
	return atomic(filepath.Join(s.Data, "catalog.json"), pointer)
}
