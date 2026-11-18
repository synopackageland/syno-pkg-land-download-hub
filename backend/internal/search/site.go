package search

import (
	"context"
)

func (s *Service) source(ctx context.Context, owner, id string) (Source, error) {
	var src Source
	err := s.withState(ctx, owner, func(st *state) error {
		var ok bool
		src, ok = st.Sources[id]
		if !ok {
			return fail("search_source_missing")
		}
		return nil
	})
	return src, err
}
func (s *Service) SiteURL(ctx context.Context, owner, id, query string) (string, error) {
	if len(query) > 256 {
		return "", fail("invalid_request")
	}
	src, err := s.source(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if src.Kind != "builtin" && src.Kind != "cardigann" {
		return "", fail("search_unsupported")
	}
	raw, err := s.definition(ctx, src)
	if err != nil {
		return "", err
	}
	rep, err := s.Run(ctx, Request{Op: "site", YAML: raw, Kind: src.Kind, URL: src.URL, Values: src.Values, Query: query})
	if err != nil {
		return "", err
	}
	if rep.Code != "" {
		return "", fail(rep.Code)
	}
	return rep.SiteURL, nil
}
