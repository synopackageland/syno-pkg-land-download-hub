package search

import (
	"context"
)

type Probe struct {
	Status string `json:"status"`
	Code   string `json:"code,omitempty"`
}

func (s *Service) TestSource(ctx context.Context, owner, id string) (Probe, error) {
	if !validID(id) {
		return Probe{}, fail("invalid_request")
	}
	ctx, cancel, solver, settingErr := s.networkContext(ctx, owner)
	defer cancel()
	if settingErr != nil {
		return Probe{}, settingErr
	}
	srcs, err := s.searchable(ctx, owner, []string{id})
	if err != nil {
		return Probe{}, err
	}
	if srcs[0].code != "" {
		return Probe{}, fail(srcs[0].code)
	}
	release, err := s.slot(ctx)
	if err != nil {
		return Probe{}, err
	}
	defer release()
	src := srcs[0].Source
	raw, err := s.definition(ctx, src)
	if err != nil {
		return Probe{}, err
	}
	if video(src) {
		return Probe{Status: "unsupported", Code: "search_unsupported"}, nil
	}
	rep, err := s.runNetwork(ctx, Request{Op: "test", Kind: src.Kind, URL: src.URL, YAML: raw, Values: src.Values}, solver)
	code := rep.Code
	if err != nil {
		code = Code(err)
	}
	status := "parse"
	switch code {
	case "":
		status = "available"
	case "search_login", "search_auth_unsupported":
		status = "login"
	case "search_cloudflare":
		status = "cloudflare"
	case "search_network", "search_timeout":
		status = "network"
	case "search_unsupported":
		status = "unsupported"
	}
	return Probe{Status: status, Code: code}, nil
}
