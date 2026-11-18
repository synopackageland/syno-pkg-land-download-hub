package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
)

// Versioned private SDK input, never a user-supplied operation/owner selector.
type searchJobInput struct {
	Version   int          `json:"version"`
	Operation string       `json:"operation"`
	Batch     search.Batch `json:"batch"`
	SourceID  string       `json:"sourceId,omitempty"`
}
type searchJobView struct {
	ID              string               `json:"id"`
	Key             string               `json:"key"`
	Operation       string               `json:"operation"`
	SourceID        string               `json:"sourceId,omitempty"`
	State           background.State     `json:"state"`
	Progress        *background.Progress `json:"progress,omitempty"`
	ErrorCode       string               `json:"errorCode,omitempty"`
	CancelRequested bool                 `json:"cancelRequested"`
	StartedAt       time.Time            `json:"startedAt,omitzero"`
}

func viewSearchJob(j background.Job) searchJobView {
	subject := ""
	if strings.HasPrefix(j.Singleton, "source-") {
		subject = strings.TrimPrefix(j.Singleton, "source-")
	}
	return searchJobView{j.ID, j.Key, j.Name, subject, j.State, j.Progress, j.ErrorCode, j.CancelRequested, j.StartedAt}
}
func (s *Service) searchJobIdentity(name string) (background.Submitter, error) {
	if s.SearchIdentity != nil {
		return s.SearchIdentity(name)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return background.Submitter{}, refused("identity_unavailable")
	}
	o := background.Submitter{User: name, UID: u.Uid}
	if !o.Valid() {
		return o, refused("identity_unavailable")
	}
	return o, nil
}
func (s *Service) searchJobStore() *background.Store {
	store := background.NewRequestStore(filepath.Join(s.Data, "search-jobs"))
	// SDK-managed migration: obsolete cookie jobs may retain private input even
	// after success. Purge the affected owner's transient search history through
	// the SDK, including pending tombstones; download jobs live elsewhere.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if jobs, err := store.List(ctx); err == nil {
		seen := map[background.Submitter]bool{}
		for _, j := range jobs {
			if (j.Name == "cookie-set" || j.Name == "cookie-test") && !seen[j.Submitter] {
				seen[j.Submitter] = true
				_, _ = store.PurgeOwner(ctx, j.Submitter, "browser-search-migration")
			}
		}
	}
	return store
}
func searchJobError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, background.ErrNotFound):
		return refused("search_job_missing")
	case errors.Is(err, background.ErrResultUnavailable):
		return refused("search_result_unavailable")
	case errors.Is(err, background.ErrKeyConflict):
		return refused("operation-conflict")
	case errors.Is(err, background.ErrBusy):
		return refused("jobs_busy")
	case errors.Is(err, background.ErrInvalid):
		return refused("invalid_request")
	case errors.Is(err, background.ErrNotCancelable):
		return refused("search_job_finished")
	default:
		return refused("jobs_unavailable")
	}
}
func (s *Service) searchJobRoutes() []api.Route {
	var routes []api.Route
	handler := func(method string) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			identity, err := s.searchJobIdentity(c.User())
			if err != nil || !identity.Valid() {
				return nil, refused("identity_unavailable")
			}
			// App product policy is owner-only, including for DSM administrators.
			access := background.Access{Owner: identity}
			store := s.searchJobStore()
			if strings.HasSuffix(method, ".start") || method == "search.jobs.start" {
				owner, err := s.Owner(c.User())
				if err != nil {
					return nil, refused("identity_unavailable")
				}
				return s.startSearchJob(ctx, owner, identity, method, raw)
			}
			if method == "search.jobs.list" {
				if err := decodeStrict(raw, &struct{}{}); err != nil {
					return nil, err
				}
				jobs, err := store.ListFor(ctx, access)
				if err != nil {
					return nil, searchJobError(err)
				}
				out := []searchJobView{}
				for _, j := range jobs {
					out = append(out, viewSearchJob(j))
				}
				return map[string]any{"jobs": out}, nil
			}
			var in struct {
				ID     string `json:"id"`
				Offset int    `json:"offset"`
				Limit  int    `json:"limit"`
			}
			if method == "search.jobs.pages" {
				if err := decodeStrict(raw, &in); err != nil {
					return nil, err
				}
			} else {
				var id struct {
					ID string `json:"id"`
				}
				if err := decodeStrict(raw, &id); err != nil {
					return nil, err
				}
				in.ID = id.ID
			}
			if !validCtlID(in.ID) {
				return nil, refused("invalid_request")
			}
			switch method {
			case "search.jobs.status":
				j, err := store.GetFor(ctx, in.ID, access)
				return viewSearchJob(j), searchJobError(err)
			case "search.jobs.cancel":
				j, err := store.CancelFor(ctx, in.ID, access)
				return viewSearchJob(j), searchJobError(err)
			case "search.jobs.result":
				out, err := store.ResultFor(ctx, in.ID, access)
				return out, searchJobError(err)
			default:
				return s.searchJobPages(ctx, store, access, in.ID, in.Offset, in.Limit)
			}
		}
	}
	return append(routes,
		api.HandleRequest("search.jobs.start", handler("search.jobs.start")),
		api.HandleRequest("search.sources.test.start", handler("search.sources.test.start")),
		api.HandleRequest("search.jobs.status", handler("search.jobs.status")),
		api.HandleRequest("search.jobs.result", handler("search.jobs.result")),
		api.HandleRequest("search.jobs.pages", handler("search.jobs.pages")),
		api.HandleRequest("search.jobs.list", handler("search.jobs.list")),
		api.HandleRequest("search.jobs.cancel", handler("search.jobs.cancel")),
	)
}
func (s *Service) startSearchJob(ctx context.Context, owner string, identity background.Submitter, method string, raw json.RawMessage) (any, error) {
	in := searchJobInput{Version: 1}
	key := ""
	scope := "search"
	switch method {
	case "search.jobs.start":
		var p struct {
			Key       string   `json:"key"`
			SourceIDs []string `json:"sourceIds"`
			Query     string   `json:"query"`
			Page      int      `json:"page"`
			Category  string   `json:"category"`
		}
		if err := decodeStrict(raw, &p); err != nil {
			return nil, err
		}
		if _, known := search.CategoryIDs(p.Category); !known || p.Page < 1 || p.Page > 100 || len(p.Query) > 256 {
			return nil, refused("invalid_request")
		}
		key = p.Key
		in.Operation = "search"
		in.Batch = search.Batch{SourceIDs: p.SourceIDs, Text: p.Query, Page: p.Page, Category: p.Category}
	case "search.sources.test.start":
		var p struct {
			Key string `json:"key"`
			ID  string `json:"id"`
		}
		if err := decodeStrict(raw, &p); err != nil {
			return nil, err
		}
		key, in.SourceID, in.Operation = p.Key, p.ID, "source-test"
	default:
		return nil, refused("invalid_request")
	}
	if !validCtlID(key) || len(key) > 64 {
		return nil, refused("invalid_request")
	}
	ids := in.Batch.SourceIDs
	if in.SourceID != "" {
		ids = []string{in.SourceID}
		scope = "source-" + in.SourceID
	}
	_, err := s.searchService().JobSources(ctx, owner, ids)
	if err != nil {
		return nil, searchRefusal(err)
	}
	data, err := json.Marshal(in)
	if err != nil {
		return nil, refused("invalid_request")
	}
	admission, err := s.searchJobStore().SubmitSingleton(ctx, scope, background.SubmitRequest{Key: key, Name: in.Operation, Data: data, Submitter: identity})
	if err != nil {
		return nil, searchJobError(err)
	}
	return map[string]any{"outcome": admission.Outcome, "job": viewSearchJob(admission.Job), "duplicate": admission.Duplicate}, nil
}

// One existing package worker runs this SDK runner beside its delegate loop.
// No CGI goroutine runs work, and no App queue or job-state journal exists.
func (s *Service) SearchRunner(info background.WorkerInfo) *background.Runner {
	r := background.NewRunner(filepath.Join(s.Data, "search-jobs"), info)
	r.Timeout = 2 * time.Minute
	r.Work = s.SearchWork
	r.ProgressEvery = 100 * time.Millisecond
	return r
}
func (s *Service) SearchWork(ctx context.Context, t *background.Task) (background.Result, error) {
	var in searchJobInput
	raw, err := io.ReadAll(io.LimitReader(t.Input, 64<<10))
	if err != nil {
		return background.Result{}, refused("invalid_request")
	}
	if decodeStrict(raw, &in) != nil || in.Version != 1 || in.Operation != t.Job.Name || !t.Job.Submitter.Valid() {
		return background.Result{}, refused("invalid_request")
	}
	current, err := s.searchJobIdentity(t.Job.Submitter.User)
	if err != nil || current != t.Job.Submitter {
		return background.Result{}, refused("identity_unavailable")
	}
	owner, err := s.Owner(current.User)
	if err != nil {
		return background.Result{}, refused("identity_unavailable")
	}
	ids := in.Batch.SourceIDs
	if in.SourceID != "" {
		ids = []string{in.SourceID}
	}
	_, err = s.searchService().JobSources(ctx, owner, ids)
	if err != nil {
		return background.Result{}, searchRefusal(err)
	}
	t.Report(background.Progress{Phase: in.Operation, Total: int64(len(ids))})
	var result any
	switch in.Operation {
	case "search":
		// No empty file first: readers wait for progress, and on a busy disk
		// each write costs seconds before the first source is asked.
		pages := []search.SourcePage{}
		publish := func(more []search.SourcePage) error {
			if len(more) == 0 {
				return nil
			}
			pages = append(pages, more...)
			if err := s.writeSearchPages(ctx, t.Job.ID, t.Job.StartedAt, pages); err != nil {
				return err
			}
			t.Report(background.Progress{Phase: "search", Done: int64(len(pages)), Total: int64(len(ids))})
			return nil
		}
		// Sources answered together are published together. Results go out
		// first; pages handed to the browser follow once their tickets exist,
		// so planning them never delays results already in hand.
		err = s.searchService().SearchEachBatch(ctx, owner, in.Batch, func(group []search.SourcePage) error {
			ready, browser := []search.SourcePage{}, []search.SourcePage{}
			for _, p := range group {
				if p.Error != nil && search.BrowserFallback(p.Error.Code) {
					browser = append(browser, p)
				} else {
					ready = append(ready, p)
				}
			}
			if err := publish(ready); err != nil {
				return err
			}
			if len(browser) == 0 {
				return nil
			}
			for i, e := range s.searchService().OfferBrowsers(ctx, owner, t.Job.ID, t.Job.StartedAt, browser, in.Batch.Text) {
				if e == nil {
					browser[i].Status = "needs_browser"
				}
			}
			return publish(browser)
		})
		result = map[string]any{"kind": "search", "pages": len(pages)}
	case "source-test":
		var p search.Probe
		p, err = s.searchService().TestSource(ctx, owner, in.SourceID)
		result = map[string]any{"kind": "probe", "id": in.SourceID, "status": p.Status, "code": p.Code}
	default:
		return background.Result{}, refused("invalid_request")
	}
	if err != nil {
		return background.Result{}, searchRefusal(err)
	}
	if ctx.Err() != nil {
		return background.Result{}, ctx.Err()
	}
	t.Report(background.Progress{Phase: "finished", Done: int64(len(ids)), Total: int64(len(ids))})
	out, err := json.Marshal(result)
	return background.Result{JSON: out}, err
}
