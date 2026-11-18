package search

import (
	"context"
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

//go:embed baseline/bundle.json
var baseline []byte

type catalogState struct {
	Current  string `json:"current"`
	Previous string `json:"previous,omitempty"`
}
type Catalog struct {
	Commit            string `json:"commit"`
	Generation        string `json:"generation"`
	Schema            int    `json:"schema"`
	Total             int    `json:"total"`
	Supported         int    `json:"supported"`
	Sites             []Site `json:"sites"`
	RollbackAvailable bool   `json:"rollbackAvailable"`
	// When the current generation was fetched on this NAS; empty for the baseline.
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func (s *Service) bundle(ctx context.Context) (Bundle, error) {
	unlock, err := lock(ctx, filepath.Join(s.Data, "catalog.lock"))
	if err != nil {
		return Bundle{}, err
	}
	defer unlock()
	var st catalogState
	err = readJSON(filepath.Join(s.Data, "catalog.json"), &st, 4096)
	if os.IsNotExist(err) {
		var b Bundle
		err = json.Unmarshal(baseline, &b)
		return b, err
	}
	if err != nil {
		return Bundle{}, fail("search_catalog_invalid")
	}
	if !generationPattern.MatchString(st.Current) {
		return Bundle{}, fail("search_catalog_invalid")
	}
	var stored struct {
		Bundle Bundle `json:"bundle"`
		Hash   string `json:"hash"`
	}
	if err = readJSON(filepath.Join(s.Data, "generations", st.Current+".json"), &stored, 16<<20); err != nil {
		return Bundle{}, fail("search_catalog_invalid")
	}
	encoded, _ := json.Marshal(stored.Bundle)
	if digest(encoded) != st.Current || stored.Hash != st.Current {
		return Bundle{}, fail("search_catalog_invalid")
	}
	return stored.Bundle, nil
}
func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*second)
	defer cancel()
	b, err := s.bundle(ctx)
	if err != nil {
		return Catalog{}, err
	}
	rep, err := s.Run(ctx, Request{Op: "catalog", Definitions: b.Definitions})
	if err != nil {
		return Catalog{}, err
	}
	if rep.Code != "" || len(rep.Sites) != len(b.Definitions) {
		return Catalog{}, fail("search_catalog_invalid")
	}
	raw, _ := json.Marshal(b)
	out := Catalog{Commit: b.Commit, Generation: digest(raw), Schema: 11, Total: len(rep.Sites), Sites: rep.Sites}
	for _, site := range rep.Sites {
		if site.Supported {
			out.Supported++
		}
	}
	var st catalogState
	if readJSON(filepath.Join(s.Data, "catalog.json"), &st, 4096) == nil {
		out.RollbackAvailable = st.Previous != ""
		// Generation records are written once, so their time is the fetch time.
		if info, err := os.Stat(filepath.Join(s.Data, "generations", st.Current+".json")); err == nil {
			out.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339)
		}
	}
	return out, nil
}
