package search

import (
	"context"
	"net/url"
	"path/filepath"
	"strings"
	"time"
)

const solverDeadline = 90 * time.Second

// Solver settings belong to the same verified owner as the source store.
func (s *Service) Solver(ctx context.Context, owner string) (string, error) {
	var value string
	err := s.withState(ctx, owner, func(st *state) error { value = st.SolverURL; return nil })
	return value, err
}
func (s *Service) SetSolver(ctx context.Context, owner, raw string) error {
	if raw != "" {
		u, err := url.Parse(raw)
		if err != nil || !validEndpoint(raw) || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" || len(raw) > 2048 || (u.Path != "" && u.Path != "/" && u.Path != "/v1") {
			return fail("invalid_request")
		}
		u.Path = "/v1"
		raw = u.String()
	}
	return s.withState(ctx, owner, func(st *state) error { st.SolverURL = raw; return nil })
}
func (s *Service) networkContext(ctx context.Context, owner string) (context.Context, context.CancelFunc, string, error) {
	solver, err := s.Solver(ctx, owner)
	limit := 25 * time.Second
	if solver != "" {
		limit = 100 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	return ctx, cancel, solver, err
}

// Normal helpers retain their short deadline. Only a proven challenge is retried
// in a separate bounded solver helper. A failed solve never replays a third time.
func (s *Service) runNetwork(ctx context.Context, in Request, solver string) (Reply, error) {
	rep, err := s.Run(ctx, in)
	if err == nil && rep.Code == "search_cloudflare" && solver != "" {
		return s.retrySolver(ctx, in, solver)
	}
	return rep, err
}
func (s *Service) retrySolver(ctx context.Context, in Request, solver string) (Reply, error) {
	ctx, cancel := context.WithTimeout(ctx, solverDeadline)
	defer cancel()
	release, err := lock(ctx, filepath.Join(s.Data, "solver.lock"))
	if err != nil {
		return Reply{}, err
	}
	defer release()
	in.SolverURL = solver
	return s.Run(ctx, in)
}

// Config exports omit YAML (it can embed literals) and mask every field value.
func (s *Service) SourceConfig(ctx context.Context, owner, id string) (map[string]any, error) {
	var src Source
	err := s.withState(ctx, owner, func(st *state) error {
		var ok bool
		src, ok = st.Sources[id]
		if !ok {
			return fail("search_source_missing")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	fields := map[string]string{}
	for k := range src.Values {
		fields[k] = "***"
	}
	m, err := s.meta(ctx, src)
	if err != nil {
		return nil, err
	}
	settings := []map[string]string{}
	for _, f := range m.Settings {
		settings = append(settings, map[string]string{"name": f.Name, "type": f.Type})
	}
	return map[string]any{"id": src.ID, "kind": src.Kind, "enabled": src.Enabled, "definitionId": src.DefinitionID, "url": sourceURL(src), "values": fields, "settings": settings}, nil
}
func (s *Service) SetSourceField(ctx context.Context, owner, id, field, value string) (SourceView, error) {
	if strings.TrimSpace(field) == "" {
		return SourceView{}, fail("invalid_request")
	}
	return s.PatchSource(ctx, owner, SourcePatch{ID: id, Values: map[string]string{field: value}})
}

type SourcePatch struct {
	ID      string            `json:"id"`
	Name    *string           `json:"name,omitempty"`
	URL     *string           `json:"url,omitempty"`
	Enabled *bool             `json:"enabled,omitempty"`
	Values  map[string]string `json:"values,omitempty"`
}

func (s *Service) PatchSource(ctx context.Context, owner string, p SourcePatch) (SourceView, error) {
	var in SourceInput
	revision := ""
	err := s.withState(ctx, owner, func(st *state) error {
		src, ok := st.Sources[p.ID]
		if !ok {
			return fail("search_source_missing")
		}
		in = src.SourceInput
		revision = src.Revision
		return nil
	})
	if err != nil {
		return SourceView{}, err
	}
	if p.Name != nil {
		in.Name = *p.Name
	}
	if p.URL != nil {
		in.URL = *p.URL
	}
	if p.Enabled != nil {
		in.Enabled = *p.Enabled
	}
	in.Values = p.Values
	return s.saveSource(ctx, owner, in, revision)
}
