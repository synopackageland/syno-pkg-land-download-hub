package main

import (
	"context"
	"errors"
	"github.com/mediactl/clustarr/pkg/cardigann"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

type browserPlan struct {
	AllowSameOriginRedirect bool     `json:"allowSameOriginRedirect,omitempty"`
	Selector                string   `json:"selector,omitempty"`
	MaxBytes                int      `json:"maxBytes,omitempty"`
	Origins                 []string `json:"origins"`
	Rows                    string   `json:"rows"`
	Reason                  string   `json:"reason,omitempty"`
}

func browserUnsupported(def *cardigann.Definition) string {
	if def == nil {
		return "definition"
	}
	if def.Login != nil {
		return "login"
	}
	for _, p := range def.Search.Paths {
		if p.Response != nil && p.Response.Type != "" && p.Response.Type != "html" {
			return "api"
		}
		if p.Method != "" && !strings.EqualFold(p.Method, "GET") {
			return "post"
		}
	}
	if def.Search.Rows.Selector == "" {
		return "selector"
	}
	return ""
}
func makeBrowserPlan(def *cardigann.Definition, cfg cardigann.Config) *browserPlan {
	p := &browserPlan{Selector: browserContainer(def.Search.Rows.Selector), Rows: def.Search.Rows.Selector, Reason: browserUnsupported(def), Origins: []string{}}
	// This source's result table alone can exceed 256 KiB before removing
	// inert attributes. Its transport budget is bounded; delivery stays 256 KiB.
	if def.ID == "kickasstorrents-ws" {
		p.MaxBytes = 512 << 10
		p.AllowSameOriginRedirect = true
	}
	for _, raw := range append(append([]string{cfg.BaseURL}, def.Links...), def.LegacyLinks...) {
		u, e := url.Parse(raw)
		if e == nil && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			origin := u.Scheme + "://" + u.Host
			found := false
			for _, v := range p.Origins {
				if v == origin {
					found = true
				}
			}
			if !found {
				p.Origins = append(p.Origins, origin)
			}
		}
	}
	return p
}

type browserTransport struct{ url, html string }

func (b browserTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != "GET" || r.URL.String() != b.url {
		return nil, errors.New("browser parsing cannot fetch")
	}
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(b.html)), Request: r}, nil
}

// Reuse each configured definition's selectors and filters with no network.
func browserParse(ctx context.Context, in request, def *cardigann.Definition, cfg cardigann.Config, siteURL string) reply {
	if len(in.HTML) == 0 || len(in.HTML) > 256<<10 || browserUnsupported(def) != "" || siteURL == "" {
		return reply{Code: "search_browser_unsupported"}
	}
	// Parse only the displayed search page. A definition may list several
	// independent result pages (1337x does); never replay one DOM for all paths.
	copyDef := *def
	if len(def.Search.Paths) > 0 {
		matched := false
		for _, path := range def.Search.Paths {
			candidate := *def
			candidate.Search.Paths = []cardigann.SearchPathBlock{path}
			candidateURL, e := (cardigann.Engine{}).SearchPageURL(ctx, &candidate, cfg, cardigannQuery(in))
			if e == nil && candidateURL == siteURL {
				copyDef = candidate
				matched = true
				break
			}
		}
		if !matched {
			return reply{Code: "search_browser_unsupported"}
		}
	}
	eng := cardigann.Engine{HTTP: &http.Client{Transport: browserTransport{siteURL, in.HTML}}}
	rels, err := eng.Search(ctx, &copyDef, cfg, cardigannQuery(in))
	if err != nil {
		return reply{Code: classify(err)}
	}
	out := mapReleases(rowFilters(&copyDef, rels, in.Query))
	out.SiteURL = siteURL
	return out
}

// Preserve the table/container context expected by the original row selector,
// while excluding navigation, advertisements and inline page state. Complex
// selectors without a safe explicit container retain the bounded body fallback.
var browserContainerPattern = regexp.MustCompile(`^((?:table|div|main|section|ul|ol)(?:[.#][A-Za-z_][A-Za-z0-9_-]*)*)(?:\s|>)`)

func browserContainer(rows string) string {
	if match := browserContainerPattern.FindStringSubmatch(rows); len(match) > 1 {
		return match[1]
	}
	return "body"
}
