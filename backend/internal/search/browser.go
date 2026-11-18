package search

import (
	"context"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
	"time"
)

const BrowserLifetime = 5 * time.Minute
const BrowserHTMLLimit = 256 << 10

type BrowserTicket struct {
	Origins                 []string    `json:"origins"`
	Selector                string      `json:"selector,omitempty"`
	MaxBytes                int         `json:"maxBytes,omitempty"`
	AllowSameOriginRedirect bool        `json:"allowSameOriginRedirect,omitempty"`
	JobID                   string      `json:"jobId"`
	StartedAt               time.Time   `json:"startedAt"`
	SourceID                string      `json:"sourceId"`
	Revision                string      `json:"revision"`
	Query                   string      `json:"query"`
	Page                    int         `json:"page"`
	URL                     string      `json:"url"`
	ResultID                string      `json:"resultId,omitempty"`
	Created                 time.Time   `json:"created"`
	Expires                 time.Time   `json:"expires"`
	TokenHash               string      `json:"tokenHash,omitempty"`
	Canceled                bool        `json:"canceled,omitempty"`
	Result                  *SourcePage `json:"result,omitempty"`
}
type BrowserRequest struct {
	ID                      string    `json:"id"`
	Token                   string    `json:"token"`
	URL                     string    `json:"url"`
	Operation               string    `json:"operation"`
	Expires                 time.Time `json:"expires"`
	Hosts                   []string  `json:"hosts"`
	Selector                string    `json:"selector,omitempty"`
	MaxBytes                int       `json:"maxBytes,omitempty"`
	AllowSameOriginRedirect bool      `json:"allowSameOriginRedirect,omitempty"`
}
type BrowserDelivery struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	URL     string `json:"url"`
	HTML    string `json:"html,omitempty"`
	Magnet  string `json:"magnet,omitempty"`
	Torrent string `json:"torrent,omitempty"`
}

func browserURL(origins []string, raw string) bool {
	if !validEndpoint(raw) {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" {
		return false
	}
	for _, origin := range origins {
		if strings.EqualFold(u.Scheme+"://"+u.Host, origin) {
			return true
		}
	}
	return false
}
func (s *Service) browserPlan(ctx context.Context, src Source, query string) (*BrowserPlan, error) {
	if src.Kind != "builtin" && src.Kind != "cardigann" {
		return nil, fail("search_browser_unsupported")
	}
	raw, err := s.definition(ctx, src)
	if err != nil {
		return nil, err
	}
	rep, err := s.Run(ctx, Request{Op: "browser.plan", YAML: raw, Kind: src.Kind, URL: src.URL, Values: src.Values, Query: query})
	if err != nil {
		return nil, err
	}
	if rep.BrowserPlan == nil || rep.BrowserPlan.Reason != "" || !browserURL(rep.BrowserPlan.Origins, rep.SiteURL) {
		return nil, fail("search_browser_unsupported")
	}
	rep.BrowserPlan.URL = rep.SiteURL
	return rep.BrowserPlan, nil
}
func browserKey(job, source string) string { return digest([]byte(job + "\x00" + source))[:32] }
func (s *Service) OfferBrowser(ctx context.Context, owner, job string, started time.Time, p SourcePage, query string) error {
	return s.OfferBrowsers(ctx, owner, job, started, []SourcePage{p}, query)[0]
}

// OfferBrowsers offers a browser ticket to each page in one state write, after
// one read of the sources and their plans in parallel (each plan runs a
// helper). The answer has one error per page, nil where a ticket waits.
func (s *Service) OfferBrowsers(ctx context.Context, owner, job string, started time.Time, pages []SourcePage, query string) []error {
	errs := make([]error, len(pages))
	srcs := make([]Source, len(pages))
	if err := s.withState(ctx, owner, func(st *state) error {
		for i, p := range pages {
			src, ok := st.Sources[p.SourceID]
			if !ok {
				errs[i] = fail("search_source_missing")
			}
			srcs[i] = src
		}
		return nil
	}); err != nil {
		for i := range errs {
			errs[i] = err
		}
		return errs
	}
	plans := make([]*BrowserPlan, len(pages))
	urls := make([]string, len(pages))
	var wg sync.WaitGroup
	for i, p := range pages {
		if errs[i] != nil {
			continue
		}
		wg.Add(1)
		go func(i int, p SourcePage) {
			defer wg.Done()
			plan, err := s.browserPlan(ctx, srcs[i], query)
			if err != nil {
				errs[i] = err
				return
			}
			urls[i] = p.SiteURL
			if urls[i] == "" {
				urls[i] = plan.URL
			}
			if !browserURL(plan.Origins, urls[i]) || urls[i] != plan.URL {
				errs[i] = fail("search_browser_unsupported")
				return
			}
			plans[i] = plan
		}(i, p)
	}
	wg.Wait()
	offered := false
	for _, e := range errs {
		offered = offered || e == nil
	}
	if !offered {
		return errs
	}
	err := s.withState(ctx, owner, func(st *state) error {
		for i, p := range pages {
			if errs[i] != nil {
				continue
			}
			key := browserKey(job, p.SourceID)
			if old, ok := st.Browser[key]; ok && old.StartedAt.Equal(started) {
				continue
			}
			if len(st.Browser) >= 64 {
				errs[i] = fail("search_limit")
				continue
			}
			plan, src := plans[i], srcs[i]
			st.Browser[key] = BrowserTicket{AllowSameOriginRedirect: plan.AllowSameOriginRedirect, MaxBytes: plan.MaxBytes, Selector: plan.Selector, Origins: plan.Origins, JobID: job, StartedAt: started, SourceID: src.ID, Revision: src.Revision, Query: query, Page: p.Page.Page, URL: urls[i], Created: time.Now(), Expires: time.Now().Add(BrowserLifetime)}
		}
		return nil
	})
	if err != nil {
		for i := range errs {
			if errs[i] == nil {
				errs[i] = err
			}
		}
	}
	return errs
}
func (s *Service) StartBrowser(ctx context.Context, owner, job, source string, started time.Time) (BrowserRequest, error) {
	return s.issueBrowser(ctx, owner, browserKey(job, source), started)
}
func (s *Service) issueBrowser(ctx context.Context, owner, key string, started time.Time) (out BrowserRequest, err error) {
	token := id() + id()
	err = s.withState(ctx, owner, func(st *state) error {
		t, ok := st.Browser[key]
		src := st.Sources[t.SourceID]
		if !ok || t.Canceled || !t.StartedAt.Equal(started) || time.Since(t.Created) > time.Hour || !src.Enabled || src.Revision != t.Revision || !browserURL(t.Origins, t.URL) {
			return fail("search_browser_expired")
		}
		t.TokenHash = digest([]byte(token))
		t.Expires = time.Now().Add(BrowserLifetime)
		t.Result = nil
		st.Browser[key] = t
		op := "search"
		if t.ResultID != "" {
			op = "download"
		}
		out = BrowserRequest{AllowSameOriginRedirect: t.AllowSameOriginRedirect, ID: key, Token: token, URL: t.URL, Operation: op, Expires: t.Expires, Hosts: t.Origins, Selector: t.Selector, MaxBytes: t.MaxBytes}
		return nil
	})
	return
}
func (s *Service) StartBrowserDownload(ctx context.Context, owner, rid string) (BrowserRequest, error) {
	r, err := s.Result(ctx, owner, rid)
	if err != nil {
		return BrowserRequest{}, err
	}
	if !r.Browser || !browserURL(r.BrowserOrigins, r.Hit.Link) {
		return BrowserRequest{}, fail("search_browser_unsupported")
	}
	key := browserKey("download", rid)
	err = s.withState(ctx, owner, func(st *state) error {
		if len(st.Browser) >= 64 {
			if _, ok := st.Browser[key]; !ok {
				return fail("search_limit")
			}
		}
		st.Browser[key] = BrowserTicket{Origins: r.BrowserOrigins, SourceID: r.Source.ID, Revision: r.Source.Revision, URL: r.Hit.Link, ResultID: rid, Created: time.Now()}
		return nil
	})
	if err != nil {
		return BrowserRequest{}, err
	}
	return s.issueBrowser(ctx, owner, key, time.Time{})
}

// Only xt+dn survive: a page cannot smuggle a tracker/webseed on a private host
// into the download engine. The existing task admission still parses BitTorrent.
func BrowserMagnet(raw string) (string, error) {
	if len(raw) > 8192 {
		return "", fail("search_browser_invalid")
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "magnet" || u.Host != "" || u.Opaque != "" {
		return "", fail("search_browser_invalid")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil || len(q["xt"]) != 1 {
		return "", fail("search_browser_invalid")
	}
	hash := strings.TrimPrefix(q.Get("xt"), "urn:btih:")
	if hash == q.Get("xt") {
		return "", fail("search_browser_invalid")
	}
	var b []byte
	if len(hash) == 40 {
		b, e = hex.DecodeString(hash)
	} else if len(hash) == 32 {
		b, e = base32.StdEncoding.DecodeString(strings.ToUpper(hash))
	} else {
		return "", fail("search_browser_invalid")
	}
	if e != nil || len(b) != 20 {
		return "", fail("search_browser_invalid")
	}
	out := url.Values{"xt": {"urn:btih:" + hex.EncodeToString(b)}}
	if dn := q.Get("dn"); len(dn) <= 1024 && dn != "" {
		out.Set("dn", dn)
	}
	return "magnet:?" + out.Encode(), nil
}
func (s *Service) DeliverBrowser(ctx context.Context, owner string, in BrowserDelivery) (SourcePage, error) {
	if !validID(in.ID) || len(in.Token) != 64 || len(in.HTML) > BrowserHTMLLimit || len(in.Torrent) > 4*((512<<10)+2)/3 {
		return SourcePage{}, fail("search_browser_invalid")
	}
	var t BrowserTicket
	var src Source
	// Spend the token durably before parsing. A lost reply is observed through
	// pages, never silently replayed. Retry explicitly issues a new token.
	err := s.withState(ctx, owner, func(st *state) error {
		var ok bool
		t, ok = st.Browser[in.ID]
		src = st.Sources[t.SourceID]
		if !ok || t.Canceled || t.TokenHash == "" || t.TokenHash != digest([]byte(in.Token)) || time.Now().After(t.Expires) || !src.Enabled || src.Revision != t.Revision || in.URL != t.URL || !browserURL(t.Origins, in.URL) {
			return fail("search_browser_expired")
		}
		t.TokenHash = ""
		st.Browser[in.ID] = t
		return nil
	})
	if err != nil {
		return SourcePage{}, err
	}
	if t.ResultID != "" {
		link := ""
		if in.Magnet != "" && in.Torrent == "" {
			link, err = BrowserMagnet(in.Magnet)
		} else if in.Torrent != "" && in.Magnet == "" {
			var b []byte
			b, err = base64.StdEncoding.DecodeString(in.Torrent)
			if err == nil && (len(b) == 0 || len(b) > 512<<10 || b[0] != 'd') {
				err = fail("search_browser_invalid")
			}
			link = "data:application/x-bittorrent;base64," + in.Torrent
		} else {
			err = fail("search_browser_invalid")
		}
		if err != nil {
			return SourcePage{}, fail("search_browser_invalid")
		}
		err = s.withState(ctx, owner, func(st *state) error {
			current := st.Browser[in.ID]
			r, ok := st.Results[t.ResultID]
			if !ok || current.Canceled || st.Sources[t.SourceID].Revision != t.Revision {
				return fail("search_browser_expired")
			}
			r.Hit.Link = link
			r.Hit.Hash = ""
			st.Results[t.ResultID] = r
			return nil
		})
		return SourcePage{}, err
	}
	if in.HTML == "" || in.Magnet != "" || in.Torrent != "" {
		return SourcePage{}, fail("search_browser_invalid")
	}
	raw, err := s.definition(ctx, src)
	if err != nil {
		return SourcePage{}, err
	}
	// Parsing gets what the request has left, less room to keep the result.
	// A slow NAS is no failure of the page: a parse cut short hands the token
	// back, so the browser delivers the same page again (search_browser_busy).
	parse, cancel := browserParseContext(ctx)
	defer cancel()
	rep, err := s.Run(parse, Request{Op: "browser", YAML: raw, URL: src.URL, Values: src.Values, Query: t.Query, Page: t.Page, HTML: in.HTML})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return SourcePage{}, s.returnBrowserToken(ctx, owner, in, t)
		}
		return SourcePage{}, err
	}
	if rep.Code != "" && rep.Code != "search_empty" && rep.Code != "search_no_data" {
		return SourcePage{}, fail(rep.Code)
	}
	if len(rep.Results) > 100 {
		return SourcePage{}, fail("search_browser_invalid")
	}
	for i, h := range rep.Results {
		if strings.HasPrefix(h.Link, "magnet:") {
			rep.Results[i].Link, err = BrowserMagnet(h.Link)
		} else if !browserURL(t.Origins, h.Link) {
			err = fail("search_browser_invalid")
		}
		// An infohash supplied by a page is never a task identity authority.
		rep.Results[i].Hash = ""
		if err != nil || len(h.Title) > 1024 || h.Size < 0 {
			return SourcePage{}, fail("search_browser_invalid")
		}
	}
	rep.Browser = true
	rep.BrowserOrigins = t.Origins
	p, err := s.keep(ctx, owner, src, t.Page, rep)
	out := SourcePage{Page: p}
	if rep.Code == "search_no_data" {
		out.Error = &SourceError{Code: rep.Code}
	}
	if err != nil {
		if Code(err) != "search_empty" && Code(err) != "search_no_data" {
			return out, err
		}
		if out.Error == nil {
			out.Error = &SourceError{Code: "search_empty"}
		}
	}
	err = s.withState(ctx, owner, func(st *state) error {
		current := st.Browser[in.ID]
		if current.Canceled || st.Sources[t.SourceID].Revision != t.Revision {
			return fail("search_browser_expired")
		}
		current.Result = &out
		st.Browser[in.ID] = current
		return nil
	})
	return out, err
}

// browserKeepMargin is the part of a delivery's request kept for saving the
// parsed page after the parse.
const browserKeepMargin = 10 * time.Second

func browserParseContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if d, ok := ctx.Deadline(); ok {
		return context.WithDeadline(ctx, d.Add(-browserKeepMargin))
	}
	return context.WithCancel(ctx)
}

// returnBrowserToken restores a spent token whose parse ran out of time, unless
// the ticket changed meanwhile, and asks the browser to deliver again.
func (s *Service) returnBrowserToken(ctx context.Context, owner string, in BrowserDelivery, spent BrowserTicket) error {
	err := s.withState(context.WithoutCancel(ctx), owner, func(st *state) error {
		current, ok := st.Browser[in.ID]
		if !ok || current.Canceled || current.TokenHash != "" || current.Revision != spent.Revision || !current.Expires.Equal(spent.Expires) {
			return fail("search_browser_expired")
		}
		current.TokenHash = digest([]byte(in.Token))
		st.Browser[in.ID] = current
		return nil
	})
	if err != nil {
		return err
	}
	return fail("search_browser_busy")
}
func (s *Service) BrowserPages(ctx context.Context, owner, job string, started time.Time) (map[string]SourcePage, error) {
	out := map[string]SourcePage{}
	err := s.withState(ctx, owner, func(st *state) error {
		for _, t := range st.Browser {
			if t.JobID == job && t.StartedAt.Equal(started) && !t.Canceled && t.Result != nil && st.Sources[t.SourceID].Revision == t.Revision && st.Sources[t.SourceID].Enabled {
				out[t.SourceID] = *t.Result
			}
		}
		return nil
	})
	return out, err
}
func (s *Service) CancelBrowser(ctx context.Context, owner, job string) error {
	return s.withState(ctx, owner, func(st *state) error {
		for k, t := range st.Browser {
			if t.JobID == job || k == job {
				t.Canceled = true
				t.TokenHash = ""
				st.Browser[k] = t
			}
		}
		return nil
	})
}

func (s *Service) BrowserBinding(ctx context.Context, owner, key string) (job string, started time.Time, err error) {
	err = s.withState(ctx, owner, func(st *state) error {
		t, ok := st.Browser[key]
		if !ok {
			return fail("search_browser_expired")
		}
		job, started = t.JobID, t.StartedAt
		return nil
	})
	return
}

// Only a proven challenge page can offer browser assistance. Opening a tab
// cannot diagnose or repair DNS, TLS or connection-reset failures.
func BrowserFallback(code string) bool { return code == "search_cloudflare" }
