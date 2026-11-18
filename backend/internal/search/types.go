// Package search owns per-user sources and opaque, expiring search results.
// Network parsing runs in a separate, short-lived helper, never under jobs.lock.
package search

import (
	"context"
	"errors"
	"time"
)

type Error struct{ Value string }

func (e *Error) Error() string { return "search: " + e.Value }
func fail(code string) error   { return &Error{code} }
func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Value
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "search_timeout"
	}
	return "search_storage"
}

type Setting struct {
	Optional bool              `json:"optional,omitempty"`
	Name     string            `json:"name"`
	Label    string            `json:"label"`
	Type     string            `json:"type"`
	Options  map[string]string `json:"options,omitempty"`
}
type Site struct {
	BrowserReason string    `json:"browserReason,omitempty"`
	Cloudflare    bool      `json:"cloudflare,omitempty"`
	File          string    `json:"file,omitempty"`
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"`
	Supported     bool      `json:"supported"`
	Reasons       []string  `json:"reasons"`
	Settings      []Setting `json:"settings"`
}
type Request struct {
	HTML        string            `json:"html,omitempty"`
	SolverURL   string            `json:"solverUrl,omitempty"`
	Op          string            `json:"op"`
	YAML        string            `json:"yaml,omitempty"`
	Definitions map[string]string `json:"definitions,omitempty"`
	URL         string            `json:"url,omitempty"`
	Kind        string            `json:"kind,omitempty"`
	Values      map[string]string `json:"values,omitempty"`
	Query       string            `json:"query,omitempty"`
	Page        int               `json:"page,omitempty"`
	Categories  []int             `json:"categories,omitempty"`
	Link        string            `json:"link,omitempty"`
	Searches    []Request         `json:"searches,omitempty"`
}
type Hit struct {
	Title    string `json:"title"`
	Link     string `json:"link"`
	Hash     string `json:"hash,omitempty"`
	Size     int64  `json:"size,omitempty"`
	Seeders  *int32 `json:"seeders,omitempty"`
	Leechers *int32 `json:"leechers,omitempty"`
	Category string `json:"category,omitempty"`
	Date     string `json:"date,omitempty"`
}
type BrowserPlan struct {
	AllowSameOriginRedirect bool     `json:"allowSameOriginRedirect,omitempty"`
	Selector                string   `json:"selector,omitempty"`
	MaxBytes                int      `json:"maxBytes,omitempty"`
	URL                     string   `json:"url,omitempty"`
	Origins                 []string `json:"origins"`
	Rows                    string   `json:"rows"`
	Reason                  string   `json:"reason,omitempty"`
}
type Reply struct {
	BrowserPlan    *BrowserPlan `json:"browserPlan,omitempty"`
	BrowserOrigins []string     `json:"-"`
	Browser        bool         `json:"-"`
	SiteURL        string       `json:"siteURL,omitempty"`
	Code           string       `json:"code,omitempty"`
	Sites          []Site       `json:"sites,omitempty"`
	Results        []Hit        `json:"results,omitempty"`
	Link           string       `json:"link,omitempty"`
	Torrent        string       `json:"torrent,omitempty"`
	Batch          []Reply      `json:"batch,omitempty"`
}
type StreamRunner func(context.Context, Request, func(int, Reply) error) error
type Runner func(context.Context, Request) (Reply, error)
type SourceInput struct {
	ID           string            `json:"id,omitempty"`
	Kind         string            `json:"kind"`
	Name         string            `json:"name"`
	Enabled      bool              `json:"enabled"`
	DefinitionID string            `json:"definitionId,omitempty"`
	YAML         string            `json:"yaml,omitempty"`
	URL          string            `json:"url,omitempty"`
	Values       map[string]string `json:"values,omitempty"`
	ClearValues  bool              `json:"clearValues,omitempty"`
}
type Source struct {
	Automatic bool `json:"automatic,omitempty"`
	SourceInput
	Revision string `json:"revision"`
}

// SourceView exposes a redacted display URL, never the stored URL, YAML or values.
type SourceView struct {
	BrowserReason string `json:"browserReason,omitempty"`
	revision      string
	Cloudflare    bool      `json:"cloudflare,omitempty"`
	Explicit      bool      `json:"explicit"`
	URL           string    `json:"url,omitempty"`
	ID            string    `json:"id"`
	Kind          string    `json:"kind"`
	Name          string    `json:"name"`
	Enabled       bool      `json:"enabled"`
	DefinitionID  string    `json:"definitionId,omitempty"`
	Configured    bool      `json:"configured"`
	Supported     bool      `json:"supported"`
	Reasons       []string  `json:"reasons"`
	Settings      []Setting `json:"settings"`
}
type Query struct {
	SourceID string `json:"sourceId"`
	Text     string `json:"query"`
	Page     int    `json:"page"`
	// Categories are Newznab IDs; sites that map none of them search no path.
	Categories []int `json:"categories,omitempty"`
}
type ResultView struct {
	ResultID string `json:"resultId"`
	SourceID string `json:"sourceId"`
	Title    string `json:"title"`
	Size     int64  `json:"size,omitempty"`
	Seeders  *int32 `json:"seeders,omitempty"`
	Leechers *int32 `json:"leechers,omitempty"`
	Category string `json:"category,omitempty"`
	Date     string `json:"date,omitempty"`
	// Hash: the torrent's info-hash in lower-case hex, so the page can show
	// one row for the same torrent from several sources. Display only.
	Hash string `json:"hash,omitempty"`
}
type Page struct {
	SiteURL      string       `json:"siteURL,omitempty"`
	SourceID     string       `json:"sourceId"`
	Page         int          `json:"page"`
	Results      []ResultView `json:"results"`
	MorePossible bool         `json:"morePossible"`
}
type Cached struct {
	BrowserOrigins []string  `json:"browserOrigins,omitempty"`
	Browser        bool      `json:"browser,omitempty"`
	Hit            Hit       `json:"hit"`
	Source         Source    `json:"source"`
	Created        time.Time `json:"created"`
}
type state struct {
	Browser          map[string]BrowserTicket `json:"browser,omitempty"`
	BrowserMigration int                      `json:"browserMigration,omitempty"`
	SolverURL        string                   `json:"solverUrl,omitempty"`
	Sources          map[string]Source        `json:"sources"`
	Results          map[string]Cached        `json:"results"`
	// Starter: the user has had the starter sites (or sources of their own).
	Starter bool `json:"starter,omitempty"`
	// StarterVersion: the revision of the starter set the user was given;
	// 0 for users seeded before it existed.
	StarterVersion int `json:"starterVersion,omitempty"`
}
type Bundle struct {
	DefinitionFiles map[string]string `json:"definitionFiles,omitempty"`
	Commit          string            `json:"commit"`
	Schema          int               `json:"schema"`
	Definitions     map[string]string `json:"definitions"`
}
