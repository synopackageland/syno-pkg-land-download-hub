package search

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"dsmapp/internal/githubtoken"
)

type Service struct {
	Data   string
	Run    Runner
	Stream StreamRunner
	HTTP   *http.Client // pinned upstream updater, injectable only in fixtures
	Video  Runner
	// How long a search waits for a free package slot; zero means four seconds.
	SlotWait time.Duration
	// GitHub holds the administrator's optional token; zero is anonymous.
	GitHub githubtoken.Store
}

func New(data, helper string) *Service {
	return &Service{Data: data, Run: ExecRunner(helper), Stream: ExecStreamRunner(helper)}
}
func (s *Service) definition(ctx context.Context, src Source) (string, error) {
	if src.Kind == "cardigann" {
		return src.YAML, nil
	}
	if src.Kind != "builtin" {
		return "", nil
	}
	b, err := s.bundle(ctx)
	if err != nil {
		return "", err
	}
	return definitionIn(b, src)
}
func definitionIn(b Bundle, src Source) (string, error) {
	file := b.DefinitionFiles[src.DefinitionID]
	if file == "" {
		file = src.DefinitionID + ".yml"
	}
	raw, ok := b.Definitions[file]
	if !ok {
		return "", fail("search_unsupported")
	}
	// A fetched generation can still contain the exact legacy LinuxTracker
	// definition shipped before the correction. Migrate only that fingerprint;
	// preserve newer upstream definitions and every imported custom definition.
	if src.DefinitionID == "linuxtracker" && digest([]byte(raw)) == "76ef5d71b6de360726231a23b018bb95bb6a4495576391d50c8b72dde179d3f0" {
		var current Bundle
		if err := json.Unmarshal(baseline, &current); err != nil {
			return "", err
		}
		raw = current.Definitions["linuxtracker.yml"]
	}
	return raw, nil
}
func (s *Service) meta(ctx context.Context, src Source) (Site, error) {
	if src.Kind != "cardigann" && src.Kind != "builtin" {
		return Site{BrowserReason: "api", Supported: true, Reasons: []string{}, Settings: []Setting{}}, nil
	}
	raw, err := s.definition(ctx, src)
	if err != nil {
		return Site{}, err
	}
	rep, err := s.Run(ctx, Request{Op: "validate", YAML: raw})
	if err != nil {
		return Site{}, err
	}
	if len(rep.Sites) != 1 {
		return Site{}, fail("search_helper")
	}
	return rep.Sites[0], nil
}
func view(src Source, m Site) SourceView {
	return SourceView{BrowserReason: m.BrowserReason, revision: src.Revision, Cloudflare: m.Cloudflare, Explicit: !src.Automatic, URL: sourceURL(src), ID: src.ID, Kind: src.Kind, Name: src.Name, Enabled: src.Enabled, DefinitionID: src.DefinitionID, Configured: len(src.Values) > 0, Supported: m.Supported, Reasons: m.Reasons, Settings: m.Settings}
}
func (s *Service) SaveSource(ctx context.Context, owner string, in SourceInput) (SourceView, error) {
	return s.saveSource(ctx, owner, in, "")
}
func (s *Service) saveSource(ctx context.Context, owner string, in SourceInput, expected string) (SourceView, error) {
	if len(in.Name) < 1 || len(in.Name) > 120 || len(in.YAML) > 256<<10 || len(in.Values) > 64 {
		return SourceView{}, fail("invalid_request")
	}
	switch in.Kind {
	case "builtin", "cardigann", "rss", "torznab", "ytsearch", "bilisearch":
	default:
		return SourceView{}, fail("invalid_request")
	}
	if in.ID != "" && !validID(in.ID) {
		return SourceView{}, fail("invalid_request")
	}
	old := Source{}
	if in.ID != "" {
		err := s.withState(ctx, owner, func(st *state) error {
			var ok bool
			old, ok = st.Sources[in.ID]
			if !ok {
				return fail("search_source_missing")
			}
			return nil
		})
		if err != nil {
			return SourceView{}, err
		}
		if expected != "" && old.Revision != expected {
			return SourceView{}, fail("search_conflict")
		}
	}
	if in.Kind == "rss" || in.Kind == "torznab" {
		if old.Kind == in.Kind && (in.URL == "" || in.URL == sourceURL(old)) {
			in.URL = old.URL
		}
		if !validEndpoint(in.URL) {
			return SourceView{}, fail("invalid_request")
		}
	}
	sourceKey(&in)
	if len(in.Values) > 64 {
		return SourceView{}, fail("invalid_request")
	}
	for k, v := range in.Values {
		if strings.EqualFold(k, "cookie") && strings.Contains(strings.ToLower(v), "cf_clearance=") {
			return SourceView{}, fail("invalid_request")
		}
		if len(k) > 80 || len(v) > 8192 || strings.ContainsAny(k+v, "\x00\r\n") {
			return SourceView{}, fail("invalid_request")
		}
	}
	if in.Kind == "cardigann" && in.YAML == "" {
		in.YAML = old.YAML
	}
	src := Source{SourceInput: in, Revision: id()}
	if src.ID == "" {
		src.ID = id()
	}
	if !in.ClearValues {
		src.Values = map[string]string{}
		for k, v := range old.Values {
			src.Values[k] = v
		}
		for k, v := range in.Values {
			if v == "" {
				delete(src.Values, k)
			} else {
				src.Values[k] = v
			}
		}
	}
	src.ClearValues = false
	m, err := s.meta(ctx, src)
	if err != nil {
		return SourceView{}, err
	}
	if src.Kind == "cardigann" && (m.ID == "" || len(m.Reasons) > 0 && m.Reasons[0] == "invalid_yaml") {
		return SourceView{}, fail("search_definition_invalid")
	}
	if err = s.withState(ctx, owner, func(st *state) error {
		if len(st.Sources) >= 64 && in.ID == "" {
			return fail("search_limit")
		}
		// Compare revision to prevent two editors silently replacing newer credentials.
		if in.ID != "" && st.Sources[in.ID].Revision != old.Revision {
			return fail("search_conflict")
		}
		st.Sources[src.ID] = src
		for k, r := range st.Results {
			if r.Source.ID == src.ID {
				delete(st.Results, k)
			}
		}
		return nil
	}); err != nil {
		return SourceView{}, err
	}
	return view(src, m), nil
}
func validID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func validEndpoint(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" && u.User == nil && u.Fragment == "" && len(raw) <= 8192 && !strings.ContainsAny(raw, "\r\n\x00")
}
func (s *Service) Sources(ctx context.Context, owner string) ([]SourceView, error) {
	if err := s.seedStarter(ctx, owner); err != nil {
		return nil, err
	}
	sources := []Source{}
	if err := s.withState(ctx, owner, func(st *state) error {
		for _, src := range st.Sources {
			sources = append(sources, src)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	out := []SourceView{}
	for _, src := range sources {
		m, err := s.meta(ctx, src)
		if err != nil {
			return nil, err
		}
		out = append(out, view(src, m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (s *Service) DeleteSource(ctx context.Context, owner, source string) error {
	return s.withState(ctx, owner, func(st *state) error {
		if _, ok := st.Sources[source]; !ok {
			return fail("search_source_missing")
		}
		delete(st.Sources, source)
		for k, r := range st.Results {
			if r.Source.ID == source {
				delete(st.Results, k)
			}
		}
		return nil
	})
}
func (s *Service) ExportSource(ctx context.Context, owner, source string) (string, error) {
	var raw string
	err := s.withState(ctx, owner, func(st *state) error {
		src, ok := st.Sources[source]
		if !ok {
			return fail("search_source_missing")
		}
		if src.Kind != "cardigann" {
			return fail("search_unsupported")
		}
		raw = src.YAML
		return nil
	})
	return raw, err
}
func (s *Service) Search(ctx context.Context, owner string, q Query) (Page, error) {
	ctx, cancel, solver, settingErr := s.networkContext(ctx, owner)
	defer cancel()
	if settingErr != nil {
		return Page{}, settingErr
	}
	out := Page{SourceID: q.SourceID, Page: q.Page, Results: []ResultView{}}
	if !validID(q.SourceID) || q.Page < 1 || q.Page > 100 || len(q.Text) > 256 {
		return out, fail("invalid_request")
	}
	release, err := s.slot(ctx)
	if err != nil {
		return out, err
	}
	defer release()
	srcs, err := s.searchable(ctx, owner, []string{q.SourceID})
	if err != nil {
		return out, err
	}
	if srcs[0].code != "" {
		return out, fail(srcs[0].code)
	}
	src := srcs[0].Source
	raw, err := s.definition(ctx, src)
	if err != nil {
		return out, err
	}
	runner := s.Run
	if video(src) {
		runner = s.Video
	}
	if runner == nil {
		return out, fail("search_tool_missing")
	}
	var rep Reply
	if video(src) {
		rep, err = runner(ctx, searchRequest(src, raw, q))
	} else {
		rep, err = s.runNetwork(ctx, searchRequest(src, raw, q), solver)
	}
	if err != nil {
		return out, fail(Code(err))
	}
	return s.keep(ctx, owner, src, q.Page, rep)
}

// A source a search may use, or the code that refuses it.
type candidate struct {
	Source
	code string
}

// searchable reads the owner's sources once; a missing or disabled source
// carries its refusal instead of failing the others.
func (s *Service) searchable(ctx context.Context, owner string, ids []string) ([]candidate, error) {
	out := make([]candidate, len(ids))
	err := s.withState(ctx, owner, func(st *state) error {
		for i, id := range ids {
			src, ok := st.Sources[id]
			switch {
			case !ok:
				out[i] = candidate{Source{SourceInput: SourceInput{ID: id}}, "search_source_missing"}
			case !src.Enabled:
				out[i] = candidate{src, "search_source_disabled"}
			default:
				out[i] = candidate{Source: src}
			}
		}
		return nil
	})
	return out, err
}
func video(src Source) bool { return src.Kind == "ytsearch" || src.Kind == "bilisearch" }
func searchRequest(src Source, raw string, q Query) Request {
	return Request{Op: "search", YAML: raw, Kind: src.Kind, URL: src.URL, Values: src.Values, Query: q.Text, Page: q.Page, Categories: q.Categories}
}

// keep stores a source's reply as opaque results and returns their page.
func (s *Service) keep(ctx context.Context, owner string, src Source, page int, rep Reply) (Page, error) {
	out := s.keepMany(ctx, owner, []keepItem{{src, page, rep}})
	return out[0].Page, out[0].err
}

type keepItem struct {
	src  Source
	page int
	rep  Reply
}
type kept struct {
	Page
	err error
}

// keepMany caches every source's results in one state write. On a NAS where
// each write waits seconds for the disk, a write per source made the first
// answers wait behind every source that answered before them.
func (s *Service) keepMany(ctx context.Context, owner string, items []keepItem) []kept {
	out := make([]kept, len(items))
	pending := false
	for i, it := range items {
		out[i].Page = Page{SiteURL: it.rep.SiteURL, SourceID: it.src.ID, Page: it.page, Results: []ResultView{}}
		switch {
		case it.rep.Code != "":
			out[i].err = fail(it.rep.Code)
		case len(it.rep.Results) == 0:
			out[i].err = fail("search_empty")
		case len(it.rep.Results) > 100:
			out[i].err = fail("search_helper")
		default:
			pending = true
		}
	}
	if !pending {
		return out
	}
	err := s.withState(ctx, owner, func(st *state) error {
		for k, r := range st.Results {
			if time.Since(r.Created) > time.Hour {
				delete(st.Results, k)
			}
		}
		for i, it := range items {
			if out[i].err != nil {
				continue
			}
			src := it.src
			if st.Sources[src.ID].Revision != src.Revision || !st.Sources[src.ID].Enabled {
				out[i].err = fail("search_conflict")
				continue
			}
			for _, hit := range it.rep.Results {
				hit = redactHit(hit, src.Values)
				if len(hit.Title) > 1024 || len(hit.Link) > 8192 {
					continue
				}
				if !strings.HasPrefix(hit.Link, "magnet:") && !validEndpoint(hit.Link) {
					continue
				}
				rid := id()
				st.Results[rid] = Cached{BrowserOrigins: it.rep.BrowserOrigins, Browser: it.rep.Browser, Hit: hit, Source: Source{SourceInput: SourceInput{ID: src.ID}, Revision: src.Revision}, Created: time.Now()}
				out[i].Results = append(out[i].Results, ResultView{rid, src.ID, hit.Title, hit.Size, hit.Seeders, hit.Leechers, hit.Category, hit.Date, infoHash(hit)})
			}
		}
		for len(st.Results) > 500 {
			var oldest string
			var when time.Time
			for k, r := range st.Results {
				if oldest == "" || r.Created.Before(when) {
					oldest, when = k, r.Created
				}
			}
			delete(st.Results, oldest)
		}
		return nil
	})
	for i, it := range items {
		if out[i].err != nil {
			continue
		}
		if err != nil {
			out[i].Results, out[i].err = []ResultView{}, err
			continue
		}
		if len(out[i].Results) == 0 {
			out[i].err = fail("search_empty")
		}
		out[i].MorePossible = (it.src.Kind == "torznab" || video(it.src)) && len(out[i].Results) == 100
	}
	return out
}
func (s *Service) Result(ctx context.Context, owner, rid string) (Cached, error) {
	var result Cached
	if !validID(rid) {
		return result, fail("search_result_expired")
	}
	err := s.withState(ctx, owner, func(st *state) error {
		r, ok := st.Results[rid]
		src, exists := st.Sources[r.Source.ID]
		if !ok || !exists || !src.Enabled || src.Revision != r.Source.Revision || time.Since(r.Created) > time.Hour {
			return fail("search_result_expired")
		}
		r.Source = src
		result = r
		return nil
	})
	return result, err
}

// Resolve is called by the existing download worker, so a click never waits for
// tracker login, detail-page scraping or torrent metadata.
func (s *Service) Resolve(ctx context.Context, owner, rid string) (string, error) {
	r, err := s.Result(ctx, owner, rid)
	if err != nil {
		return "", err
	}
	return s.resolveCached(ctx, owner, r)
}
func (s *Service) resolveCached(ctx context.Context, owner string, r Cached) (string, error) {
	if r.Browser {
		if strings.HasPrefix(r.Hit.Link, "magnet:") || strings.HasPrefix(r.Hit.Link, "data:application/x-bittorrent;base64,") {
			return r.Hit.Link, nil
		}
		return "", fail("search_browser_required")
	}
	if (r.Source.Kind == "rss" && r.Source.Values["cookie"] == "") || strings.HasPrefix(r.Hit.Link, "magnet:") || r.Source.Kind == "ytsearch" || r.Source.Kind == "bilisearch" {
		return r.Hit.Link, nil
	}
	raw, err := s.definition(ctx, r.Source)
	if err != nil {
		return "", err
	}
	kind := r.Source.Kind
	endpoint := r.Source.URL
	if kind == "rss" || kind == "torznab" {
		endpoint = r.Hit.Link
	}
	values := r.Source.Values
	if kind == "rss" || kind == "torznab" {
		origin, _ := url.Parse(r.Source.URL)
		target, _ := url.Parse(endpoint)
		if origin == nil || target == nil || !strings.EqualFold(origin.Host, target.Host) || origin.Scheme != target.Scheme {
			values = nil
		}
	}
	ctx, cancel, solver, settingErr := s.networkContext(ctx, owner)
	defer cancel()
	if settingErr != nil {
		return "", settingErr
	}
	rep, err := s.runNetwork(ctx, Request{Op: "download", YAML: raw, Kind: kind, URL: endpoint, Values: values, Link: r.Hit.Link}, solver)
	if err != nil {
		return "", err
	}
	if rep.Code != "" {
		return "", fail(rep.Code)
	}
	if rep.Link != "" && strings.HasPrefix(rep.Link, "magnet:") {
		return rep.Link, nil
	}
	if rep.Torrent != "" {
		b, err := base64.StdEncoding.DecodeString(rep.Torrent)
		if err == nil && len(b) <= 512<<10 {
			return "data:application/x-bittorrent;base64," + rep.Torrent, nil
		}
	}
	return "", fail("search_parse")
}
func (s *Service) SetEnabled(ctx context.Context, owner, source string, enabled bool) error {
	return s.withState(ctx, owner, func(st *state) error {
		src, ok := st.Sources[source]
		if !ok {
			return fail("search_source_missing")
		}
		src.Enabled = enabled
		src.Automatic = false
		src.Revision = id()
		st.Sources[source] = src
		for key, result := range st.Results {
			if result.Source.ID == source {
				delete(st.Results, key)
			}
		}
		return nil
	})
}
