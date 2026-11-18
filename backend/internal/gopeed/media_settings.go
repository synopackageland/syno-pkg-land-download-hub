package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"dsmapp/internal/media"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

type MediaPreferences struct {
	DownloadSubfolder *string       `json:"downloadSubfolder,omitempty"`
	Defaults          media.Options `json:"defaults"`
	Rules             []MediaRule   `json:"rules"`
	Follows           []MediaFollow `json:"follows"`
}
type MediaRule struct {
	ID      string        `json:"id"`
	Host    string        `json:"host,omitempty"`
	Channel string        `json:"channel,omitempty"`
	Path    string        `json:"path"`
	Options media.Options `json:"options"`
	Grant   string        `json:"grant,omitempty"`
}
type MediaFollow struct {
	ID            string          `json:"id"`
	URL           string          `json:"url"`
	Path          string          `json:"path"`
	Grant         string          `json:"grant"`
	IntervalHours int             `json:"intervalHours"`
	NextCheck     time.Time       `json:"nextCheck"`
	LastCheck     time.Time       `json:"lastCheck,omitempty"`
	Error         string          `json:"error,omitempty"`
	Seen          map[string]bool `json:"seen,omitempty"`
}

func preferences(st *State, owner string) MediaPreferences {
	p := st.MediaPreferences[owner]
	if p.Defaults.Format == "" {
		p.Defaults.Format = "best"
	}
	if p.Rules == nil {
		p.Rules = []MediaRule{}
	}
	if p.Follows == nil {
		p.Follows = []MediaFollow{}
	}
	return p
}
func setPreferences(st *State, owner string, p MediaPreferences) {
	if st.MediaPreferences == nil {
		st.MediaPreferences = map[string]MediaPreferences{}
	}
	st.MediaPreferences[owner] = p
}
func (s *Service) cookies() media.Cookies {
	return media.Cookies{Home: filepath.Join(s.mediaHome(), "cookies")}
}
func (s *Service) mediaUserRoutes() []api.Route {
	handler := func(method string) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, e := s.Owner(c.User())
			if e != nil {
				return nil, refused("identity_unavailable")
			}
			return s.mediaUserRequest(ctx, c, r, owner, method, raw)
		}
	}
	return []api.Route{
		api.HandleRequest("media.settings.get", handler("media.settings.get")),
		api.HandleRequest("media.settings.save", handler("media.settings.save")),
		api.HandleRequest("media.rules.save", handler("media.rules.save")),
		api.HandleRequest("media.rules.delete", handler("media.rules.delete")),
		api.HandleRequest("media.follows.add", handler("media.follows.add")),
		api.HandleRequest("media.follows.delete", handler("media.follows.delete")),
		api.HandleRequest("media.cookies.status", handler("media.cookies.status")),
		api.HandleRequest("media.cookies.import", handler("media.cookies.import")),
		api.HandleRequest("media.cookies.delete", handler("media.cookies.delete")),
	}
}

func (s *Service) mediaUserRequest(ctx context.Context, c api.Caller, r *http.Request, owner, method string, raw json.RawMessage) (any, error) {
	switch method {
	case "media.settings.get":
		if e := decodeStrict(raw, &struct{}{}); e != nil {
			return nil, e
		}
		st, e := s.readState()
		if e != nil {
			return nil, e
		}
		p := preferences(&st, owner)
		// Admission archive is private daemon state, not settings content.
		for i := range p.Follows {
			p.Follows[i].Seen = nil
		}
		return p, nil
	case "media.settings.save":
		var in media.Options
		if e := decodeStrict(raw, &in); e != nil {
			return nil, e
		}
		o, e := in.Normalize()
		if e != nil {
			return nil, mediaRefused(e)
		}
		return s.withState(ctx, func(st *State) (any, error) {
			p := preferences(st, owner)
			p.Defaults = o
			setPreferences(st, owner, p)
			return o, nil
		})
	case "media.cookies.status", "media.cookies.import", "media.cookies.delete":
		var in struct {
			Text   string `json:"text"`
			Domain string `json:"domain"`
		}
		if method == "media.cookies.status" {
			if e := decodeStrict(raw, &struct{}{}); e != nil {
				return nil, e
			}
		} else if e := decodeStrict(raw, &in); e != nil {
			return nil, e
		}
		// The shared journal lock serializes import/delete across CGI processes.
		return s.withState(ctx, func(st *State) (any, error) {
			if method == "media.cookies.import" {
				setPreferences(st, owner, preferences(st, owner))
			}
			var out media.CookieSummary
			var e error
			switch method {
			case "media.cookies.import":
				out, e = s.cookies().Import(owner, in.Text)
			case "media.cookies.delete":
				out, e = s.cookies().Delete(owner, in.Domain)
			default:
				out, e = s.cookies().Status(owner)
			}
			return out, mediaRefused(e)
		})
	case "media.rules.save":
		var in MediaRule
		if e := decodeStrict(raw, &in); e != nil {
			return nil, e
		}
		if in.Grant != "" || in.ID != "" && !engineTaskID.MatchString(in.ID) {
			return nil, refused("invalid_request")
		}
		in.Host = strings.ToLower(strings.TrimSpace(in.Host))
		in.Channel = strings.TrimSuffix(strings.TrimSpace(in.Channel), "/")
		if (in.Host == "") == (in.Channel == "") || in.Host != "" && (!safeURL("https://"+in.Host+"/") || strings.ContainsAny(in.Host, "/:?#@")) || in.Channel != "" && (!safeURL(in.Channel) || !s.allowedURL(in.Channel)) {
			return nil, refused("invalid_request")
		}
		options, e := in.Options.Normalize()
		if e != nil {
			return nil, mediaRefused(e)
		}
		in.Options = options
		if !validSubfolder(in.Path) {
			return nil, refused("invalid_subfolder")
		}
		grant, e := s.currentDestination(ctx, c, r)
		if e != nil {
			return nil, e
		}
		in.Grant = ""
		if in.ID == "" {
			in.ID = randomID()
		}
		return s.withState(ctx, func(st *State) (any, error) {
			st.Folders[owner], st.Grants[owner] = grant.Path, grant.ID
			p := preferences(st, owner)
			for i, v := range p.Rules {
				if v.ID == in.ID {
					p.Rules[i] = in
					setPreferences(st, owner, p)
					return in, nil
				}
			}
			if len(p.Rules) >= 64 {
				return nil, refused("queue_limit")
			}
			p.Rules = append(p.Rules, in)
			setPreferences(st, owner, p)
			return in, nil
		})
	case "media.follows.add":
		var in struct {
			URL           string `json:"url"`
			Path          string `json:"path"`
			IntervalHours int    `json:"intervalHours"`
			Consent       bool   `json:"consent"`
		}
		if e := decodeStrict(raw, &in); e != nil {
			return nil, e
		}
		if !in.Consent {
			return nil, refused("consent_required")
		}
		if !safeURL(in.URL) || !s.allowedURL(in.URL) {
			return nil, refused("url_denied")
		}
		if in.IntervalHours == 0 {
			in.IntervalHours = 6
		}
		if in.IntervalHours < 2 || in.IntervalHours > 168 {
			return nil, refused("invalid_request")
		}
		if !validSubfolder(in.Path) {
			return nil, refused("invalid_subfolder")
		}
		grant, e := s.currentDestination(ctx, c, r)
		if e != nil {
			return nil, e
		}
		return s.withState(ctx, func(st *State) (any, error) {
			st.Folders[owner], st.Grants[owner] = grant.Path, grant.ID
			p := preferences(st, owner)
			for _, f := range p.Follows {
				if f.URL == in.URL {
					return f, nil
				}
			}
			if len(p.Follows) >= 32 {
				return nil, refused("queue_limit")
			}
			f := MediaFollow{ID: randomID(), URL: in.URL, Path: in.Path, IntervalHours: in.IntervalHours, NextCheck: time.Now().UTC()}
			p.Follows = append(p.Follows, f)
			setPreferences(st, owner, p)
			return f, nil
		})
	case "media.rules.delete", "media.follows.delete":
		var in struct {
			ID string `json:"id"`
		}
		if e := decodeStrict(raw, &in); e != nil {
			return nil, e
		}
		return s.withState(ctx, func(st *State) (any, error) {
			p := preferences(st, owner)
			if method == "media.rules.delete" {
				for i, v := range p.Rules {
					if v.ID == in.ID {
						p.Rules = append(p.Rules[:i], p.Rules[i+1:]...)
						break
					}
				}
			} else {
				for i, v := range p.Follows {
					if v.ID == in.ID {
						p.Follows = append(p.Follows[:i], p.Follows[i+1:]...)
						break
					}
				}
			}
			setPreferences(st, owner, p)
			return map[string]bool{"deleted": true}, nil
		})
	}
	return nil, refused("endpoint_denied")
}
func matchingRule(p MediaPreferences, source, channel string) *MediaRule {
	u, e := url.Parse(source)
	if e != nil {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	// Channel rules take precedence, then the first exact host rule.
	for _, r := range p.Rules {
		if r.Channel != "" {
			for _, v := range []string{source, channel} {
				v = strings.TrimSuffix(v, "/")
				if v == r.Channel || strings.HasPrefix(v, r.Channel+"/") {
					copy := r
					return &copy
				}
			}
		}
	}
	for _, r := range p.Rules {
		if r.Host == host {
			copy := r
			return &copy
		}
	}
	return nil
}
