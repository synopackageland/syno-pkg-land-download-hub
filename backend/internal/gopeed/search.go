package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strings"

	"dsmapp/internal/githubtoken"
	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

const searchTicketPrefix = "downloadhub-search:"

func (s *Service) searchService() *search.Service {
	if s.Search != nil {
		return s.Search
	}
	helper := "/var/packages/SynoPkgGopeed/target/daemon/payload/bin/search-helper"
	svc := search.New(filepath.Join(s.Data, "search"), helper)
	svc.Video = s.searchVideo
	svc.GitHub = githubtoken.At(s.Data)
	return svc
}
func searchRefusal(err error) error {
	if err == nil {
		return nil
	}
	if refusal := githubRefusal("search_update_rate_limited", err); refusal != nil {
		return refusal
	}
	return refused(search.Code(err))
}
func (s *Service) searchRoutes() []api.Route {
	routes := append(s.searchJobRoutes(), s.browserSearchRoutes()...)
	handler := func(method string) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			svc := s.searchService()
			var result any
			switch method {
			case "search.sources.site":
				var in struct {
					ID    string `json:"id"`
					Query string `json:"query"`
				}
				if err = decodeStrict(raw, &in); err == nil {
					var value string
					value, err = svc.SiteURL(ctx, owner, in.ID, in.Query)
					result = map[string]string{"url": value}
				}
			case "search.solver.get":
				if err = decodeStrict(raw, &struct{}{}); err == nil {
					var value string
					value, err = svc.Solver(ctx, owner)
					result = map[string]any{"url": value, "configured": value != ""}
				}
			case "search.solver.set":
				var in struct {
					URL string `json:"url"`
				}
				if err = decodeStrict(raw, &in); err == nil {
					err = svc.SetSolver(ctx, owner, in.URL)
					result = map[string]bool{"configured": in.URL != ""}
				}
			case "search.sources.config":
				var in struct {
					ID string `json:"id"`
				}
				if err = decodeStrict(raw, &in); err == nil {
					result, err = svc.SourceConfig(ctx, owner, in.ID)
				}
			case "search.sources.field":
				var in struct {
					ID    string `json:"id"`
					Field string `json:"field"`
					Value string `json:"value"`
				}
				if err = decodeStrict(raw, &in); err == nil {
					var saved search.SourceView
					saved, err = svc.SetSourceField(ctx, owner, in.ID, in.Field, in.Value)
					result = map[string]any{"id": saved.ID, "configured": saved.Configured}
				}
			case "search.sources.patch":
				var in search.SourcePatch
				if err = decodeStrict(raw, &in); err == nil {
					var saved search.SourceView
					saved, err = svc.PatchSource(ctx, owner, in)
					result = map[string]any{"id": saved.ID, "enabled": saved.Enabled, "configured": saved.Configured}
				}
			case "search.catalog":
				if err = decodeStrict(raw, &struct{}{}); err == nil {
					result, err = svc.Catalog(ctx)
				}
			case "search.sources.list":
				if err = decodeStrict(raw, &struct{}{}); err == nil {
					result, err = svc.Sources(ctx, owner)
				}
			case "search.sources.enabled":
				var in struct {
					ID      string `json:"id"`
					Enabled bool   `json:"enabled"`
				}
				if err = decodeStrict(raw, &in); err == nil {
					err = svc.SetEnabled(ctx, owner, in.ID, in.Enabled)
					result = map[string]bool{"enabled": in.Enabled}
				}
			case "search.sources.save":
				var in search.SourceInput
				if err = decodeStrict(raw, &in); err == nil {
					result, err = svc.SaveSource(ctx, owner, in)
				}
			case "search.sources.delete", "search.sources.export":
				var in struct {
					ID string `json:"id"`
				}
				if err = decodeStrict(raw, &in); err == nil {
					if method == "search.sources.delete" {
						err = svc.DeleteSource(ctx, owner, in.ID)
						result = map[string]bool{"deleted": err == nil}
					} else {
						var yaml string
						yaml, err = svc.ExportSource(ctx, owner, in.ID)
						result = map[string]string{"yaml": yaml}
					}
				}
			case "search.download":
				return s.downloadSearchResult(ctx, c, r, owner, raw)
			case "search.definitions.update", "search.definitions.rollback":
				if err = api.RequireAdmin(c); err != nil {
					return nil, err
				}
				if err = decodeStrict(raw, &struct{}{}); err == nil {
					if method == "search.definitions.update" {
						result, err = svc.Update(ctx)
					} else {
						err = svc.Rollback(ctx)
						result = map[string]bool{"rolledBack": err == nil}
					}
				}
			}
			if err != nil {
				var apiErr *api.Error
				if errors.As(err, &apiErr) {
					return nil, err
				}
				return nil, searchRefusal(err)
			}
			return result, nil
		}
	}
	return append(routes,
		api.HandleRequest("search.sources.site", handler("search.sources.site")),
		api.HandleRequest("search.solver.get", handler("search.solver.get")),
		api.HandleRequest("search.solver.set", handler("search.solver.set")),
		api.HandleRequest("search.sources.field", handler("search.sources.field")),
		api.HandleRequest("search.sources.patch", handler("search.sources.patch")),
		api.HandleRequest("search.sources.config", handler("search.sources.config")),
		api.HandleRequest("search.catalog", handler("search.catalog")),
		api.HandleRequest("search.sources.list", handler("search.sources.list")),
		api.HandleRequest("search.sources.save", handler("search.sources.save")),
		api.HandleRequest("search.sources.enabled", handler("search.sources.enabled")),
		api.HandleRequest("search.sources.delete", handler("search.sources.delete")),
		api.HandleRequest("search.sources.export", handler("search.sources.export")),
		api.HandleRequest("search.download", handler("search.download")),
		api.HandleRequest("search.definitions.update", handler("search.definitions.update")),
		api.HandleRequest("search.definitions.rollback", handler("search.definitions.rollback")),
	)
}
func (s *Service) downloadSearchResult(ctx context.Context, c api.Caller, r *http.Request, owner string, raw json.RawMessage) (any, error) {
	var in struct {
		ResultID string `json:"resultId"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	svc := s.searchService()
	hit, err := svc.Result(ctx, owner, in.ResultID)
	if err != nil {
		return nil, searchRefusal(err)
	}
	if hit.Browser && !strings.HasPrefix(hit.Hit.Link, "magnet:") && !strings.HasPrefix(hit.Hit.Link, "data:application/x-bittorrent;base64,") {
		return nil, refused("search_browser_required")
	}
	identity := ""
	if hit.Source.Kind != "ytsearch" && hit.Source.Kind != "bilisearch" {
		identity = hit.Hit.Hash
	}
	if hash, err := magnetInfoHash(hit.Hit.Link); err == nil {
		identity = "btih:" + hash
	} else if identity != "" {
		identity = "btih:" + strings.ToLower(identity)
	}
	if identity == "" {
		identity = videoIdentity(hit.Hit.Link)
	}
	if ids, err := s.existingSearchDownload(ctx, owner, identity); err != nil {
		return nil, err
	} else if len(ids) > 0 {
		return map[string]any{"results": []map[string]any{{"key": "existing", "status": "created", "ids": ids}}}, nil
	}
	ticket, err := svc.Pin(ctx, owner, in.ResultID, identity)
	if err != nil {
		return nil, searchRefusal(err)
	}
	key := "search-" + search.AdmissionKey(identity)
	grant, err := s.currentDestination(ctx, c, r)
	if err != nil {
		return nil, err
	}
	// Pass the durable opaque input through the same instant-admission path as Add.
	// The daemon resolves it in createLinkSource, outside every shared state lock.
	out, err := s.createMany(ctx, c, r, owner, mustMarshal(map[string]any{"path": grant.Path, "items": []map[string]string{{"key": key, "url": searchTicketPrefix + ticket}}}))
	if err != nil {
		return nil, err
	}
	// A newly queued row shows the result title, never an internal ticket URI.
	_, err = s.withState(ctx, func(st *State) (any, error) {
		for id, j := range st.Jobs {
			if j.Owner == owner && j.BatchKey == key && j.Preparation != nil && j.Preparation.URL == searchTicketPrefix+ticket {
				j.Filename = hit.Hit.Title
				if !validName(j.Filename) {
					j.Filename = "Download"
				}
				st.Jobs[id] = j
			}
		}
		return nil, nil
	})
	return out, err
}
func (s *Service) resolveSearchLink(ctx context.Context, owner, link string) (string, error) {
	if !strings.HasPrefix(link, searchTicketPrefix) {
		return link, nil
	}
	resolved, err := s.searchService().ResolveTicket(ctx, owner, strings.TrimPrefix(link, searchTicketPrefix))
	return resolved, searchRefusal(err)
}

// Tracker URLs, headers and uploaded metainfo can contain passkeys. Search
// tasks expose only the download's display/file/progress metadata.
func hideSearchSource(task map[string]any) {
	if meta, ok := task["meta"].(map[string]any); ok {
		delete(meta, "req")
		if res, ok := meta["res"].(map[string]any); ok {
			delete(res, "extra")
			delete(res, "reqs")
			delete(res, "url")
			delete(res, "headers")
		}
	}
}

func (s *Service) existingSearchDownload(ctx context.Context, owner, identity string) ([]string, error) {
	ids := []string{}
	_, err := s.withState(ctx, func(st *State) (any, error) {
		for task, j := range st.Jobs {
			if j.Owner != owner || j.DeleteRequested {
				continue
			}
			if (strings.HasPrefix(identity, "btih:") && j.Hash != "" && strings.EqualFold("btih:"+j.Hash, identity)) || (j.Media != nil && videoIdentity(j.Media.URL) == identity) {
				ids = append(ids, task)
			}
		}
		return nil, nil
	})
	return ids, err
}

// Torznab need not advertise an infohash. Once the private fetch has produced
// torrent bytes, reuse an owner task before touching the hash's live stage.
func (s *Service) existingSearchContent(ctx context.Context, owner, link string) ([]string, error) {
	kind, err := ClassifySource(link, nil)
	if err != nil {
		return nil, err
	}
	if kind != SourceTorrentFile && kind != SourceMagnet {
		return nil, nil
	}
	src, err := s.btSource(ctx, kind, link)
	if err != nil {
		return nil, err
	}
	return s.existingSearchDownload(ctx, owner, "btih:"+src.hash)
}
