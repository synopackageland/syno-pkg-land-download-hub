package gopeed

import (
	"context"
	"dsmapp/internal/search"
	"encoding/json"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"net/http"
	"time"
)

func (s *Service) browserSearchRoutes() []api.Route {
	routes := []api.Route{}
	handler := func(method string) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			identity, err := s.searchJobIdentity(c.User())
			if err != nil {
				return nil, err
			}
			svc := s.searchService()
			store := s.searchJobStore()
			access := background.Access{Owner: identity}
			authorize := func(id string) (background.Job, error) {
				j, e := store.GetFor(ctx, id, access)
				if e != nil {
					return j, searchJobError(e)
				}
				if j.Name != "search" || j.CancelRequested || (j.State != background.StateRunning && j.State != background.StateSucceeded) {
					return j, refused("search_result_unavailable")
				}
				return j, nil
			}
			if method == "search.browser.deliver" {
				var in search.BrowserDelivery
				if len(raw) > 1<<20 || decodeStrict(raw, &in) != nil {
					return nil, refused("invalid_request")
				}
				job, started, e := svc.BrowserBinding(ctx, owner, in.ID)
				if e != nil {
					return nil, searchRefusal(e)
				}
				if job != "" {
					j, e := authorize(job)
					if e != nil {
						return nil, e
					}
					if !j.StartedAt.Equal(started) {
						return nil, refused("search_browser_expired")
					}
				}
				out, e := svc.DeliverBrowser(ctx, owner, in)
				return out, searchRefusal(e)
			}
			if method == "search.browser.download" {
				var in struct {
					ResultID string `json:"resultId"`
				}
				if decodeStrict(raw, &in) != nil {
					return nil, refused("invalid_request")
				}
				out, e := svc.StartBrowserDownload(ctx, owner, in.ResultID)
				return out, searchRefusal(e)
			}
			var in struct {
				ID       string `json:"id"`
				SourceID string `json:"sourceId,omitempty"`
				Timeout  int    `json:"timeout,omitempty"`
				Browser  bool   `json:"browser,omitempty"`
			}
			if decodeStrict(raw, &in) != nil || !validCtlID(in.ID) {
				return nil, refused("invalid_request")
			}
			if method == "search.jobs.wait" {
				if in.Timeout < 1 || in.Timeout > 20 || in.SourceID != "" {
					return nil, refused("invalid_request")
				}
				until := time.Now().Add(time.Duration(in.Timeout) * time.Second)
				for {
					j, e := store.GetFor(ctx, in.ID, access)
					if e != nil {
						return nil, searchJobError(e)
					}
					if j.State == background.StateSucceeded && !j.CancelRequested {
						out, e := s.searchJobPages(ctx, store, access, in.ID, 0, 100)
						if e != nil {
							return nil, e
						}
						pending := false
						for _, p := range out.(map[string]any)["items"].([]search.SourcePage) {
							if p.Error != nil && p.Error.Code == "search_cloudflare" {
								pending = true
							}
						}
						if !in.Browser || !pending || !time.Now().Before(until) {
							return out, nil
						}
					} else if j.CancelRequested || j.State == background.StateCanceled || j.State == background.StateFailed || j.State == background.StateInterrupted {
						return nil, refused("search_job_finished")
					}
					if !time.Now().Before(until) {
						return map[string]any{"pending": true, "id": in.ID}, nil
					}
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(250 * time.Millisecond):
					}
				}
			}
			if in.Timeout != 0 || in.Browser {
				return nil, refused("invalid_request")
			}
			j, e := authorize(in.ID)
			if e != nil {
				return nil, e
			}
			if method == "search.browser.cancel" {
				if in.SourceID != "" {
					return nil, refused("invalid_request")
				}
				e = svc.CancelBrowser(ctx, owner, in.ID)
				return map[string]bool{"canceled": e == nil}, searchRefusal(e)
			}
			out, e := svc.StartBrowser(ctx, owner, j.ID, in.SourceID, j.StartedAt)
			return out, searchRefusal(e)
		}
	}
	return append(routes,
		api.HandleRequest("search.browser.start", handler("search.browser.start")),
		api.HandleRequest("search.browser.deliver", handler("search.browser.deliver")),
		api.HandleRequest("search.browser.cancel", handler("search.browser.cancel")),
		api.HandleRequest("search.browser.download", handler("search.browser.download")),
		api.HandleRequest("search.jobs.wait", handler("search.jobs.wait")),
	)
}
