package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"sort"
	"strings"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// Task commands project a stable, credential-free view instead of exposing the
// engine proxy, raw requests, trackers, staging paths or preparation URLs.
type ctlTask struct {
	ID               string `json:"id"`
	Status           string `json:"status"`
	Published        bool   `json:"published"`
	PublicationState string `json:"publicationState"`
	Downloaded       *int64 `json:"downloaded"`
	Total            *int64 `json:"total"`
	Speed            *int64 `json:"speed"`
}

type ctlSearchSource struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Enabled    bool   `json:"enabled"`
	Configured bool   `json:"configured"`
	Supported  bool   `json:"supported"`
}

func (s *Service) ctlRoutes() []api.Route {
	routes := []api.Route{}
	handler := func(method string) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			switch method {
			case "search.sources":
				if err := decodeStrict(raw, &struct{}{}); err != nil {
					return nil, err
				}
				sources, err := s.searchService().Sources(ctx, owner)
				if err != nil {
					return nil, searchRefusal(err)
				}
				// UI source metadata includes display URLs/settings and user-provided
				// names. The command returns only source identity and readiness.
				items := []ctlSearchSource{}
				for _, src := range sources {
					items = append(items, ctlSearchSource{src.ID, src.Kind, src.Enabled, src.Configured, src.Supported})
				}
				return items, nil
			case "tasks.list", "tasks.get":
				var in struct {
					ID string `json:"id"`
				}
				if method == "tasks.get" {
					if err := decodeStrict(raw, &in); err != nil {
						return nil, err
					}
					if !validCtlID(in.ID) {
						return nil, refused("invalid_request")
					}
				} else if err := decodeStrict(raw, &struct{}{}); err != nil {
					return nil, err
				}
				rep, err := s.readTaskList(ctx, owner, Input{Method: "GET", Path: "/api/v1/tasks"})
				if err != nil {
					return nil, err
				}
				var rows []struct {
					ID       string `json:"id"`
					Status   string `json:"status"`
					Progress struct {
						Downloaded *int64 `json:"downloaded"`
						Speed      *int64 `json:"speed"`
					} `json:"progress"`
					Meta struct {
						Res struct {
							Size *int64 `json:"size"`
						} `json:"res"`
					} `json:"meta"`
					Publication struct {
						Published bool   `json:"published"`
						State     string `json:"state"`
					} `json:"publication"`
				}
				if rep.Code != 0 || json.Unmarshal(rep.Data, &rows) != nil {
					return nil, refused("engine_unavailable")
				}
				items := []ctlTask{}
				for _, row := range rows {
					item := ctlTask{ID: row.ID, Status: row.Status, Published: row.Publication.Published, PublicationState: row.Publication.State,
						Downloaded: row.Progress.Downloaded, Total: row.Meta.Res.Size, Speed: row.Progress.Speed}
					if method == "tasks.get" && row.ID == in.ID {
						return item, nil
					}
					items = append(items, item)
				}
				if method == "tasks.get" {
					return nil, refused("task_denied")
				}
				sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
				return map[string]any{"tasks": items}, nil
			case "tasks.delete", "tasks.removal":
				var in struct {
					ID string `json:"id"`
				}
				if err := decodeStrict(raw, &in); err != nil {
					return nil, err
				}
				if !validCtlID(in.ID) {
					return nil, refused("invalid_request")
				}
				if method == "tasks.removal" {
					return s.ctlRemoval(owner, in.ID)
				}
				work, cancel := context.WithTimeout(ctx, s.saveBudget())
				defer cancel()
				out, err := s.deleteTask(work, owner, in.ID)
				if err != nil {
					return nil, err
				}
				return out.(Reply).Data, nil
			case "tasks.pause", "tasks.resume":
				var in struct {
					ID string `json:"id"`
				}
				if err := decodeStrict(raw, &in); err != nil {
					return nil, err
				}
				if !validCtlID(in.ID) {
					return nil, refused("invalid_request")
				}
				op := "pause"
				if method == "tasks.resume" {
					op = "continue"
				}
				out, err := s.controlTask(ctx, owner, in.ID, op)
				if err != nil {
					return nil, err
				}
				if rep, ok := out.(Reply); ok && rep.Code != 0 {
					return nil, refused("engine_unavailable")
				}
				// Control is durable intent; publication cancellation may still be in flight.
				return map[string]any{"id": in.ID, "accepted": true}, nil
			case "downloads.destination":
				if err := decodeStrict(raw, &struct{}{}); err != nil {
					return nil, err
				}
				grant, err := s.currentDestination(ctx, c, r)
				if err != nil {
					return nil, err
				}
				return map[string]string{"path": func() string { st, _ := s.readState(); return path.Join(grant.Path, downloadSubfolder(&st, owner)) }(), "status": "active"}, nil
			case "downloads.add", "downloads.add_torrent":
				var in struct {
					Key string `json:"key"`
					URL string `json:"url"`
				}
				if method == "downloads.add_torrent" {
					var torrent struct {
						Key     string `json:"key"`
						Torrent string `json:"torrent"`
					}
					if err := decodeStrict(raw, &torrent); err != nil {
						return nil, err
					}
					if torrent.Torrent == "" || len(torrent.Torrent) > 4*((maxTorrentBytes+2)/3) {
						return nil, refused("invalid_request")
					}
					in.Key, in.URL = torrent.Key, torrentDataPrefix+torrent.Torrent
				} else {
					if err := decodeStrict(raw, &in); err != nil {
						return nil, err
					}
					if len(in.URL) > 8<<10 {
						return nil, refused("invalid_request")
					}
				}
				if !batchKey.MatchString(in.Key) || in.URL == "" || strings.HasPrefix(in.Key, "search-") {
					return nil, refused("invalid_request")
				}
				grant, err := s.currentDestination(ctx, c, r)
				if err != nil {
					return nil, err
				}
				out, err := s.createMany(ctx, c, r, owner, mustMarshal(map[string]any{"path": grant.Path, "items": []map[string]string{{"key": in.Key, "url": in.URL}}}))
				if err != nil {
					return nil, err
				}
				results := out.(map[string]any)["results"].([]batchResult)
				if len(results) != 1 {
					return nil, refused("engine_unavailable")
				}
				if results[0].Status == "failed" {
					return nil, refused(results[0].Code)
				}
				return map[string]any{"job_id": in.Key, "accepted": true, "completion": "not_checked"}, nil
			default:
				var in struct {
					JobID string `json:"job_id"`
				}
				if err := decodeStrict(raw, &in); err != nil {
					return nil, err
				}
				if !batchKey.MatchString(in.JobID) {
					return nil, refused("invalid_request")
				}
				return s.downloadStatus(owner, in.JobID)
			}
		}
	}
	return append(routes,
		api.HandleRequest("tasks.list", handler("tasks.list")),
		api.HandleRequest("tasks.get", handler("tasks.get")),
		api.HandleRequest("tasks.delete", handler("tasks.delete")),
		api.HandleRequest("tasks.removal", handler("tasks.removal")),
		api.HandleRequest("tasks.pause", handler("tasks.pause")),
		api.HandleRequest("tasks.resume", handler("tasks.resume")),
		api.HandleRequest("downloads.add", handler("downloads.add")),
		api.HandleRequest("downloads.add_torrent", handler("downloads.add_torrent")),
		api.HandleRequest("downloads.status", handler("downloads.status")),
		api.HandleRequest("downloads.destination", handler("downloads.destination")),
		api.HandleRequest("search.sources", handler("search.sources")),
	)
}

func validCtlID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return false
		}
	}
	return true
}

// This poll is observational. It never reconciles/replays submissions, changes
// grants or claims that source preparation implies a completed download/save.
func (s *Service) downloadStatus(owner, key string) (any, error) {
	st, err := s.readState()
	if err != nil {
		return nil, err
	}
	found, pending, failed := false, false, false
	ids := []string{}
	for id, j := range st.Jobs {
		if j.Owner != owner || j.BatchKey != key {
			continue
		}
		found = true
		ids = append(ids, id)
		if j.Preparation != nil {
			pending = true
			failed = failed || j.Preparation.Status == "error"
		}
		failed = failed || j.DeleteRequested
	}
	for _, j := range st.Resolved {
		if j.Owner == owner && j.BatchKey == key && j.SubmissionRID != "" {
			found, pending = true, true
		}
	}
	if !found {
		return nil, refused("task_denied")
	}
	state := "prepared"
	if pending {
		state = "pending"
	}
	if failed {
		state = "failed"
	}
	sort.Strings(ids)
	return map[string]any{"job_id": key, "state": state, "ids": ids, "completion": "not_checked"}, nil
}

// Removal observations use the same bounded owner-bound notices as the UI.
// Missing history cannot prove removal; no source names or paths are returned.
func (s *Service) ctlRemoval(owner, id string) (any, error) {
	st, err := s.readState()
	if err != nil {
		return nil, err
	}
	if j, ok := st.Jobs[id]; ok && j.Owner == owner {
		state := "present"
		if j.DeleteRequested {
			state = "deleting"
		}
		return map[string]any{"id": id, "state": state, "fileKept": j.DeleteKept}, nil
	}
	for i := len(st.Removals) - 1; i >= 0; i-- {
		r := st.Removals[i]
		if r.ID == id && r.Owner == owner {
			return map[string]any{"id": id, "state": "removed", "fileKept": r.FileKept}, nil
		}
	}
	return nil, refused("task_denied")
}
