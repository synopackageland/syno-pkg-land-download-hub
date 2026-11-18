// Download Hub's short-lived Cardigann/RSS adapter. Secrets arrive on stdin only.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/goccy/go-yaml"
	"github.com/mediactl/clustarr/pkg/cardigann"
	"github.com/mediactl/clustarr/pkg/newznab"
	"github.com/mediactl/clustarr/pkg/torznab"
	"github.com/mmcdole/gofeed"
)

type request struct {
	HTML        string            `json:"html"`
	SolverURL   string            `json:"solverUrl"`
	Op          string            `json:"op"`
	YAML        string            `json:"yaml"`
	Definitions map[string]string `json:"definitions"`
	URL         string            `json:"url"`
	Kind        string            `json:"kind"`
	Values      map[string]string `json:"values"`
	Query       string            `json:"query"`
	Page        int               `json:"page"`
	Categories  []int             `json:"categories"`
	Link        string            `json:"link"`
	Searches    []request         `json:"searches"`
}
type setting struct {
	Optional bool              `json:"optional,omitempty"`
	Name     string            `json:"name"`
	Label    string            `json:"label"`
	Type     string            `json:"type"`
	Options  map[string]string `json:"options,omitempty"`
}
type site struct {
	BrowserReason string    `json:"browserReason,omitempty"`
	Cloudflare    bool      `json:"cloudflare,omitempty"`
	File          string    `json:"file,omitempty"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Supported     bool      `json:"supported"`
	Reasons       []string  `json:"reasons"`
	Settings      []setting `json:"settings"`
}
type result struct {
	Title    string `json:"title"`
	Link     string `json:"link"`
	Hash     string `json:"hash,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Seeders  *int32 `json:"seeders,omitempty"`
	Leechers *int32 `json:"leechers,omitempty"`
	Category string `json:"category,omitempty"`
	Date     string `json:"date,omitempty"`
}
type reply struct {
	BrowserPlan *browserPlan `json:"browserPlan,omitempty"`
	SiteURL     string       `json:"siteURL,omitempty"`
	Code        string       `json:"code,omitempty"`
	Sites       []site       `json:"sites,omitempty"`
	Results     []result     `json:"results,omitempty"`
	Link        string       `json:"link,omitempty"`
	Torrent     string       `json:"torrent,omitempty"`
	Batch       []reply      `json:"batch,omitempty"`
}

// A closed feature gate sits above the schema, which allows free-form filter names.
var filters = map[string]bool{}

func init() {
	for _, s := range strings.Fields("querystring regexp re_replace split replace trim validfilename prepend append urlencode urldecode htmldecode htmlencode diacritics jsonjoinarray hexdump strdump validate timeago reltime fuzzytime dateparse timeparse tolower toupper") {
		filters[s] = true
	}
}
func inspect(raw string) (*cardigann.Definition, site) {
	meta := site{Reasons: []string{}, Settings: []setting{}}
	if len(raw) > 256<<10 || !boundedYAML(raw) {
		meta.Reasons = append(meta.Reasons, "definition_too_large")
		return nil, meta
	}
	var doc map[string]any
	_ = yaml.Unmarshal([]byte(raw), &doc) // engine Load handles quoted tabs and BOMs
	meta.ID, _ = doc["id"].(string)
	meta.Name, _ = doc["name"].(string)
	meta.Type, _ = doc["type"].(string)
	def, err := cardigann.Load([]byte(raw))
	if err != nil {
		meta.Reasons = append(meta.Reasons, "schema_or_engine_unsupported")
		return nil, meta
	}
	// KAT labels newly published rows "just now". The pinned Cardigann
	// timeago filter accepts numeric durations; normalize this source's date
	// through its existing filter API without changing the bundled definition.
	if def.ID == "kickasstorrents-ws" {
		for i := range def.Search.Fields {
			field := &def.Search.Fields[i]
			if field.Name == "date" {
				field.Block.Filters = append([]cardigann.FilterBlock{{Name: "re_replace", Args: cardigann.ScalarList{"^just now$", "0 seconds ago"}}}, field.Block.Filters...)
			}
		}
	}
	meta.ID, meta.Name, meta.Type = def.ID, def.Name, string(def.Type)
	// Reject unsupported filter names throughout login/search/download, never passthrough.
	var walk func(reflect.Value)
	ft := reflect.TypeOf(cardigann.FilterBlock{})
	walk = func(v reflect.Value) {
		if v.Kind() == reflect.Pointer {
			if !v.IsNil() {
				walk(v.Elem())
			}
			return
		}
		if v.Type() == ft {
			name := v.FieldByName("Name").String()
			args, _ := v.FieldByName("Args").Interface().(cardigann.ScalarList)
			// replace with an empty needle inserts its text between every character.
			// andmatch is run by the helper (see andMatch), and only as a row filter.
			if name == "andmatch" && rowFilter(def, v) {
				return
			}
			if !filters[name] || (name == "replace" && (len(args) == 0 || args[0] == "")) {
				meta.Reasons = append(meta.Reasons, "filter:"+name)
			}
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				walk(v.Field(i))
			}
		case reflect.Slice, reflect.Array:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i))
			}
		case reflect.Map:
			it := v.MapRange()
			for it.Next() {
				walk(it.Value())
			}
		case reflect.Interface:
			if !v.IsNil() {
				walk(v.Elem())
			}
		}
	}
	walk(reflect.ValueOf(def))
	if len(def.Certificates) > 0 {
		meta.Reasons = append(meta.Reasons, "custom_tls_certificates")
	}
	for _, f := range def.Settings {
		if f.Type == "info_flaresolverr" {
			meta.Cloudflare = true
		}
		if strings.HasPrefix(f.Type, "info") {
			continue
		}
		options := map[string]string{}
		for k, v := range f.Options {
			options[k] = string(v)
		}
		// Nothing asks for text a login does not use; a default stands in otherwise.
		optional := string(f.Default) != "" || def.Login == nil
		meta.Settings = append(meta.Settings, setting{Optional: optional, Name: f.Name, Label: f.Label, Type: f.Type, Options: options})
	}
	if !cardigann.RuntimeTemplatesSupported(def) {
		meta.Reasons = append(meta.Reasons, "template_unsupported")
	}
	if credentialsInURLs(def) {
		meta.Reasons = append(meta.Reasons, "credentials_in_url")
	}
	meta.BrowserReason = browserUnsupported(def)
	meta.Supported = len(meta.Reasons) == 0
	return def, meta
}

// rowFilter reports whether v is one of the definition's rows.filters blocks.
func rowFilter(def *cardigann.Definition, v reflect.Value) bool {
	if !v.CanAddr() {
		return false
	}
	for i := range def.Search.Rows.Filters {
		if v.Addr().Interface() == any(&def.Search.Rows.Filters[i]) {
			return true
		}
	}
	return false
}

// andMatch is Jackett's rows filter of that name, which the engine ignores: keep
// a release only if every word of the query (split on non-letters/digits) is in
// its lower-cased title, of which only the first limit characters count.
func andMatch(rels []torznab.Release, query string, limit int) []torznab.Release {
	words := strings.FieldsFunc(strings.ToLower(query), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) == 0 {
		return rels
	}
	out := rels[:0:0]
next:
	for _, r := range rels {
		title := []rune(strings.ToLower(r.Title))
		if limit > 0 && len(title) > limit {
			title = title[:limit]
		}
		for _, w := range words {
			if !strings.Contains(string(title), w) {
				continue next
			}
		}
		out = append(out, r)
	}
	return out
}
func classify(err error) string {
	var site *cardigann.SearchError
	if errors.As(err, &site) && site.Message == "search_no_data" {
		return "search_no_data"
	}
	var cf *cardigann.CloudflareChallengeError
	var login *cardigann.LoginError
	var cap *cardigann.CaptchaRequiredError
	var status *cardigann.StatusError
	var dns *net.DNSError
	var network net.Error
	var certificate *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.EPIPE):
		return "search_connection_reset"
	case errors.As(err, &dns):
		return "search_dns"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return "search_timeout"
	case errors.As(err, &network) && network.Timeout():
		return "search_timeout"
	case errors.As(err, &certificate), errors.As(err, &authority), errors.As(err, &hostname), errors.As(err, &invalid):
		return "search_tls"
	case errors.Is(err, errDestinationDenied):
		return "search_destination_denied"
	case errors.As(err, &cf):
		return "search_cloudflare"
	case errors.As(err, &login), errors.As(err, &cap), errors.Is(err, cardigann.ErrSessionRequired):
		return "search_login"
	case errors.As(err, &status):
		if status.StatusCode == 401 {
			return "search_login"
		}
		return "search_http_" + fmtInt(status.StatusCode)
	}
	var op *net.OpError
	if errors.As(err, &op) {
		return "search_connection"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		return "search_network"
	}
	if strings.Contains(err.Error(), "request ") {
		return "search_network"
	}
	if strings.Contains(err.Error(), "credentials_in_url") {
		return "search_auth_unsupported"
	}
	return "search_parse"
}

// maxMirrors bounds how many of a definition's links one search may try.
const maxMirrors = 3

// mirrorFailure is a reply that says this address is down or blocked, not that
// the site has nothing: the next link of the definition may still answer.
func mirrorFailure(code string) bool {
	switch code {
	case "search_connection", "search_connection_reset", "search_dns", "search_timeout", "search_tls", "search_network", "search_cloudflare", "search_http_403":
		return true
	}
	return strings.HasPrefix(code, "search_http_5")
}

// run searches the definition's first link and, while the answer is a
// mirrorFailure, the next links in order (not legacylinks), within the caller's
// deadline: each try but the last gets half of the time left. A URL the user
// pinned is never replaced.
func run(ctx context.Context, in request, client *http.Client) reply {
	if (in.Op != "search" && in.Op != "test") || in.URL != "" || in.Kind == "rss" || in.Kind == "torznab" {
		return runOnce(ctx, in, client)
	}
	def, meta := inspect(in.YAML)
	if !meta.Supported || len(def.Links) < 2 {
		return runOnce(ctx, in, client)
	}
	links := def.Links
	if len(links) > maxMirrors {
		links = links[:maxMirrors]
	}
	var best reply
	for i, link := range links {
		attempt := ctx
		if deadline, ok := ctx.Deadline(); ok && i < len(links)-1 {
			var cancel context.CancelFunc
			attempt, cancel = context.WithTimeout(ctx, time.Until(deadline)/2)
			defer cancel()
		}
		in.URL = link
		got := runOnce(attempt, in, client)
		if !mirrorFailure(got.Code) {
			return got
		}
		// The browser can only solve a Cloudflare challenge, so report that one.
		if i == 0 || (got.Code == "search_cloudflare" && best.Code != "search_cloudflare") {
			best = got
		}
		if ctx.Err() != nil {
			break
		}
	}
	return best
}
func runOnce(ctx context.Context, in request, client *http.Client) (out reply) {
	if in.Op == "catalog" {
		names := make([]string, 0, len(in.Definitions))
		for n := range in.Definitions {
			names = append(names, n)
		}
		sort.Strings(names)
		out := reply{}
		for _, n := range names {
			_, m := inspect(in.Definitions[n])
			m.File = n
			out.Sites = append(out.Sites, m)
		}
		return out
	}
	if in.Op == "validate" {
		_, m := inspect(in.YAML)
		return reply{Sites: []site{m}}
	}
	if in.Kind == "rss" || in.Kind == "torznab" {
		return feed(ctx, in, withSolver(client, in.SolverURL))
	}
	def, meta := inspect(in.YAML)
	if !meta.Supported {
		return reply{Code: "search_unsupported"}
	}
	cfg, err := cardigann.NewConfig(def, in.URL, in.Values)
	if err != nil {
		return reply{Code: "search_login"}
	}
	// Browser links use only defaults and non-secret settings; never session
	// cookies, passkeys or private request headers. No network is performed.
	safeValues := map[string]string{}
	for k, v := range in.Values {
		if !secretName(k) && k != "useragent" {
			safeValues[k] = v
		}
	}
	for k := range sensitive(def) {
		safeValues[k] = ""
	}
	safeValues["cookie"] = ""
	safeValues["useragent"] = ""
	publicCfg, publicErr := cardigann.NewConfig(def, in.URL, safeValues)
	siteURL := ""
	if publicErr == nil && !credentialsInURLs(def) {
		siteURL, _ = (cardigann.Engine{}).SearchPageURL(ctx, def, publicCfg, cardigannQuery(in))
	}
	if u, e := url.Parse(siteURL); e != nil || urlCredentials(u) || (u.Scheme != "https" && u.Scheme != "http") {
		siteURL = ""
	}
	defer func() {
		out.SiteURL = siteURL
	}()
	if in.Op == "site" {
		return reply{}
	}
	if in.Op == "browser.plan" {
		return reply{BrowserPlan: makeBrowserPlan(def, publicCfg)}
	}
	if in.Op == "browser" {
		return browserParse(ctx, in, def, publicCfg, siteURL)
	}
	eng := cardigann.Engine{HTTP: withPrivacy(withSolver(client, in.SolverURL), def, in.Values)}
	if in.SolverURL != "" {
		eng.Proxy = eng.HTTP.Transport
	}
	cfg.Session, err = eng.Login(ctx, def, cfg)
	if err != nil {
		return reply{Code: classify(err)}
	}
	if in.Op == "test" && def.Login != nil && def.Login.Test != nil {
		return reply{}
	}
	if in.Op == "download" {
		r, err := eng.Download(ctx, def, cfg, in.Link)
		if err != nil {
			return reply{Code: classify(err)}
		}
		defer r.Close()
		b, err := io.ReadAll(io.LimitReader(r, (512<<10)+1))
		if err != nil || len(b) > 512<<10 {
			return reply{Code: "search_parse"}
		}
		if strings.HasPrefix(string(b), "magnet:") {
			return reply{Link: string(b)}
		}
		if len(b) == 0 || b[0] != 'd' {
			return reply{Code: "search_parse"}
		}
		return reply{Torrent: base64.StdEncoding.EncodeToString(b)}
	}
	// Cardigann v11 has no generic page contract. Refuse a second page instead of replaying page one.
	if in.Page > 1 {
		return reply{Code: "search_page_unsupported"}
	}
	releases, err := eng.Search(ctx, def, cfg, cardigannQuery(in))
	if err != nil {
		return reply{Code: classify(err)}
	}
	if in.Op == "test" {
		return reply{}
	}
	return mapReleases(rowFilters(def, releases, in.Query))
}

// rowFilters runs the definition's rows filters the engine ignores (andmatch).
func rowFilters(def *cardigann.Definition, releases []torznab.Release, query string) []torznab.Release {
	for _, f := range def.Search.Rows.Filters {
		if f.Name == "andmatch" {
			limit := 0
			if len(f.Args) > 0 {
				limit, _ = strconv.Atoi(strings.TrimSpace(f.Args[0]))
			}
			releases = andMatch(releases, query, limit)
		}
	}
	return releases
}
func mapReleases(rels []torznab.Release) reply {
	out := reply{}
	for _, r := range rels {
		if r.Group != "" || strings.HasSuffix(strings.ToLower(r.Link), ".nzb") {
			continue
		}
		link := r.MagnetURL
		if link == "" {
			link = r.Link
		}
		if link == "" {
			// Jackett builds a magnet for an infohash-only row (TPB's API has no link).
			link = infoHashMagnet(r.InfoHash, r.Title)
		}
		if link == "" {
			continue
		}
		out.Results = append(out.Results, result{Title: r.Title, Link: redactLink(link), Hash: r.InfoHash, Size: r.Size, Seeders: r.Seeders, Leechers: leechers(r), Category: category(r), Date: pubDate(r.PubDate)})
		if len(out.Results) == 100 {
			break
		}
	}
	if len(out.Results) == 0 {
		out.Code = "search_empty"
	}
	return out
}
func infoHashMagnet(hash, title string) string {
	if b, err := hex.DecodeString(hash); err != nil || len(b) != 20 {
		return ""
	}
	return "magnet:?xt=urn:btih:" + strings.ToLower(hash) + "&dn=" + url.QueryEscape(title)
}
func feedRaw(ctx context.Context, in request, client *http.Client) reply {
	u, err := url.Parse(in.URL)
	if err != nil {
		return reply{Code: "invalid_request"}
	}
	if in.Kind == "torznab" && in.Op != "download" {
		q := u.Query()
		if in.Op == "test" {
			q.Set("t", "caps")
			q.Del("q")
			q.Del("limit")
			q.Del("offset")
		} else {
			q.Set("t", "search")
			q.Set("q", in.Query)
			q.Set("limit", "100")
			q.Set("offset", fmtInt((in.Page-1)*100))
			if len(in.Categories) > 0 {
				cats := make([]string, len(in.Categories))
				for i, c := range in.Categories {
					cats[i] = fmtInt(c)
				}
				q.Set("cat", strings.Join(cats, ","))
			}
		}
		u.RawQuery = q.Encode()
	} else if in.Page > 1 {
		return reply{Code: "search_page_unsupported"}
	}
	if in.Kind == "torznab" {
		authenticateURL(u, in.Values)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return reply{Code: "invalid_request"}
	}
	if key := in.Values["apikey"]; key != "" {
		req.Header.Set("X-Api-Key", key)
	}
	if cookie := in.Values["cookie"]; cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := feedClient(client, in).Do(req)
	if err != nil {
		return reply{Code: classify(err)}
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	if err != nil || len(b) > 8<<20 {
		return reply{Code: "search_parse"}
	}
	if cardigann.IsCloudflareChallenge(res.Header, b) {
		return reply{Code: "search_cloudflare"}
	}
	if res.StatusCode == 401 {
		return reply{Code: "search_login"}
	}
	if res.StatusCode != http.StatusOK {
		return reply{Code: "search_http_" + fmtInt(res.StatusCode)}
	}
	if in.Op == "test" {
		if in.Kind == "torznab" {
			var root struct{ XMLName xml.Name }
			if xml.Unmarshal(b, &root) != nil || root.XMLName.Local != "caps" {
				apiErr, _ := torznab.ParseError(strings.NewReader(string(b)))
				if apiErr != nil && apiErr.Code >= 100 && apiErr.Code <= 104 {
					return reply{Code: "search_login"}
				}
				return reply{Code: "search_parse"}
			}
			if _, err := torznab.ParseCaps(strings.NewReader(string(b))); err != nil {
				return reply{Code: "search_parse"}
			}
		} else if _, err := gofeed.NewParser().ParseString(string(b)); err != nil {
			return reply{Code: "search_parse"}
		}
		return reply{}
	}
	if in.Op == "download" {
		if len(b) == 0 || b[0] != 'd' || len(b) > 512<<10 {
			return reply{Code: "search_parse"}
		}
		return reply{Torrent: base64.StdEncoding.EncodeToString(b)}
	}
	if in.Kind == "torznab" {
		apiError, parseErr := torznab.ParseError(strings.NewReader(string(b)))
		if parseErr != nil {
			return reply{Code: "search_parse"}
		}
		if apiError != nil {
			code := "search_network"
			if apiError.Code >= 100 && apiError.Code <= 104 {
				code = "search_login"
			}
			return reply{Code: code}
		}
		rels, err := torznab.ParseResults(strings.NewReader(string(b)))
		if err != nil {
			return reply{Code: "search_parse"}
		}
		return mapReleases(rels)
	}
	f, err := gofeed.NewParser().ParseString(string(b))
	if err != nil {
		return reply{Code: "search_parse"}
	}
	out := reply{}
	for _, it := range f.Items {
		if in.Query != "" && !strings.Contains(strings.ToLower(it.Title), strings.ToLower(in.Query)) {
			continue
		}
		link := it.Link
		for _, e := range it.Enclosures {
			if e.Type == "application/x-bittorrent" || strings.HasPrefix(e.URL, "magnet:") {
				link = e.URL
				break
			}
		}
		if link == "" {
			continue
		}
		r := result{Title: it.Title, Link: redactLink(link)}
		if it.PublishedParsed != nil {
			r.Date = it.PublishedParsed.Format(time.RFC3339)
		}
		out.Results = append(out.Results, r)
		if len(out.Results) == 100 {
			break
		}
	}
	if len(out.Results) == 0 {
		out.Code = "search_empty"
	}
	return out
}
func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if os.Geteuid() == 0 {
		json.NewEncoder(os.Stdout).Encode(reply{Code: "search_helper_refuses_root"})
		return
	}
	var in request
	d := json.NewDecoder(io.LimitReader(os.Stdin, 12<<20))
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		json.NewEncoder(os.Stdout).Encode(reply{Code: "invalid_request"})
		return
	}
	limit := 25 * time.Second
	if in.SolverURL != "" {
		limit = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	var out reply
	switch {
	case in.Op == "batch.stream" && len(in.Searches) > 0 && len(in.Searches) <= batchMax:
		enc := json.NewEncoder(os.Stdout)
		var mu sync.Mutex
		batchEach(ctx, in.Searches, batchClients(), sourceDeadline, func(index int, r reply) {
			mu.Lock()
			defer mu.Unlock()
			if enc.Encode(struct {
				Index int   `json:"index"`
				Reply reply `json:"reply"`
			}{index, r}) != nil {
				cancel()
			}
		})
		return
	case in.Op == "batch" && len(in.Searches) <= batchMax:
		out = batch(ctx, in.Searches, batchClients(), sourceDeadline)
	case in.Op == "batch":
		out = reply{Code: "invalid_request"}
	default:
		out = run(ctx, in, newClient(in.Kind == "torznab"))
	}
	if ctx.Err() != nil && in.Op != "batch" {
		out = reply{Code: "search_timeout"}
	}
	json.NewEncoder(os.Stdout).Encode(out)
}

// pubDate leaves out a date the definition could not parse. Its zero time
// would otherwise read as uploaded two thousand years ago.
func pubDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// cardigannQuery is the definition search for the request's text and categories.
func cardigannQuery(in request) cardigann.Query {
	q := cardigann.Query{Type: "search", Q: in.Query}
	for _, c := range in.Categories {
		q.Categories = append(q.Categories, newznab.CategoryID(c))
	}
	return q
}
