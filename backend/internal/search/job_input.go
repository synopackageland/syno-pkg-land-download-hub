package search

import (
	"context"
)

// JobSources verifies source ownership without a helper or network request.
// Recheck in the worker; queued input never grants authority over a source.
func (s *Service) JobSources(ctx context.Context, owner string, ids []string) (map[string]string, error) {
	if len(ids) < 1 || len(ids) > 64 {
		return nil, fail("invalid_request")
	}
	revisions := map[string]string{}
	err := s.withState(ctx, owner, func(st *state) error {
		for _, id := range ids {
			if !validID(id) {
				return fail("invalid_request")
			}
			if _, ok := revisions[id]; ok {
				return fail("invalid_request")
			}
			src, ok := st.Sources[id]
			if !ok {
				return fail("search_source_missing")
			}
			revisions[id] = src.Revision
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return revisions, nil
}
