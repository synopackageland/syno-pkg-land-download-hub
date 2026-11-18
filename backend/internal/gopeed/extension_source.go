package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// maxExtensionRequestBytes bounds one stored extension request (URL, headers
// and labels).
const maxExtensionRequestBytes = 16 << 10

// extensionFile is one file of an extension's resolve result: the request the
// engine downloads it with and its name in the destination.
type extensionFile struct {
	Req  json.RawMessage `json:"req"`
	Name string          `json:"name"`
}

type extensionResource struct {
	Name  string `json:"name"`
	Files []struct {
		Name string          `json:"name"`
		Path string          `json:"path"`
		Size int64           `json:"size"`
		Req  json.RawMessage `json:"req"`
	} `json:"files"`
}

// previewExtension keeps an extension's resolve result, which has no engine
// resolve ID, as a preview of the owner's. Each file is downloaded later by
// its own engine task, so each request is checked like a pasted address.
// The browser receives the file list without the requests.
func (s *Service) previewExtension(st *State, preview Job, res extensionResource, name string) (any, error) {
	if len(res.Files) == 0 {
		return nil, refused("source_unsupported")
	}
	sources := make([]multisave.Source, len(res.Files))
	files := make([]extensionFile, len(res.Files))
	shown := make([]map[string]any, len(res.Files))
	for i, f := range res.Files {
		req, err := s.extensionRequest(f.Req)
		if err != nil {
			return nil, err
		}
		p := f.Name
		if f.Path != "" {
			p = f.Path + "/" + f.Name
		}
		sources[i] = multisave.Source{Path: p, Size: f.Size}
		files[i].Req = req
		shown[i] = map[string]any{"name": f.Name, "path": f.Path, "size": f.Size}
	}
	title := res.Name
	if title == "" {
		title = res.Files[0].Name
	}
	items, err := multisave.Plan(title, "", sources)
	if err != nil {
		return nil, refused("invalid_files")
	}
	for i := range items {
		files[i].Name = items[i].Name
	}
	if len(files) == 1 && name != "" {
		if !validName(name) {
			return nil, refused("invalid_filename")
		}
		files[0].Name = name
	}
	for rid, other := range st.Resolved {
		if other.Owner == preview.Owner && other.Kind == string(SourceExtension) && other.SubmissionRID == "" {
			delete(st.Resolved, rid)
		}
	}
	rid := "x" + randomID()
	preview.Kind, preview.Stage, preview.FileCount, preview.Filename, preview.Requests = string(SourceExtension), "", len(files), title, files
	st.Resolved[rid] = preview
	data, _ := json.Marshal(map[string]any{"id": rid, "res": map[string]any{"name": res.Name, "files": shown}})
	return Reply{Data: data}, nil
}

// extensionRequest admits one file request of an extension result under the
// same floor as a pasted address; BT and local addresses are refused.
func (s *Service) extensionRequest(raw json.RawMessage) (json.RawMessage, error) {
	var req map[string]any
	if len(raw) > maxExtensionRequestBytes || json.Unmarshal(raw, &req) != nil || req == nil {
		return nil, refused("source_unsupported")
	}
	u, _ := req["url"].(string)
	if kind, err := ClassifySource(u, nil); err != nil || kind != SourceEngine || !s.allowedURL(u) {
		return nil, refused("url_denied")
	}
	var err error
	if req["url"], err = engineAddress(u); err != nil {
		return nil, err
	}
	req["skipVerifyCert"] = false
	delete(req, "proxy")
	return json.Marshal(req)
}

// createExtension submits each file of an extension preview as one
// single-file job. Every job's intent is fsynced before the first create
// POST and is reconciled by its own stage and name, like any other job.
func (s *Service) createExtension(ctx context.Context, r *http.Request, caller api.Caller, st *State, rid string, preview Job) (any, error) {
	return s.createExtensionSource(ctx, sourceAccess{caller: caller, request: r, owner: preview.Owner}, st, rid, preview)
}

func (s *Service) createExtensionSource(ctx context.Context, a sourceAccess, st *State, rid string, preview Job) (any, error) {
	if len(st.Jobs)+len(st.Resolved)-1+preview.FileCount > 128 {
		return nil, refused("queue_limit")
	}
	err := a.recheck(ctx, s, preview)
	if err != nil {
		return nil, err
	}
	children := make([]string, preview.FileCount)
	for i := range children {
		stage := filepath.Join(s.StagingDir, randomID())
		if err = os.MkdirAll(stage, 0700); err != nil {
			return nil, err
		}
		key := rid + "." + strconv.Itoa(i)
		st.Resolved[key] = Job{Subfolder: preview.Subfolder, Owner: preview.Owner, Stage: stage, Destination: preview.Destination, Filename: preview.Requests[i].Name, DelegationID: preview.DelegationID, SubmissionRID: key, BatchKey: preview.BatchKey, BatchKind: preview.BatchKind}
		children[i] = key
	}
	requests := preview.Requests
	delete(st.Resolved, rid)
	intent, err := json.Marshal(st)
	if err != nil {
		return nil, err
	}
	if err = s.writeState(intent); err != nil {
		return nil, err
	}
	for n, key := range children {
		job := st.Resolved[key]
		rep, err := s.engine(ctx, "POST", "/api/v1/tasks", nil, map[string]any{"req": requests[n].Req, "opts": map[string]any{"path": job.Stage, "name": job.Filename}})
		var id string
		if err != nil || rep.Code != 0 || json.Unmarshal(rep.Data, &id) != nil || !engineTaskID.MatchString(id) {
			continue
		}
		st.Jobs[id] = job
		delete(st.Resolved, key)
	}
	lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	reply, _, err := s.extensionReceipt(lookup, st, rid, preview.Owner)
	return reply, err
}

// extensionReceipt answers a repeated create of an extension preview with
// the tasks its files became; a marked file is never posted again.
func (s *Service) extensionReceipt(ctx context.Context, st *State, rid, owner string) (any, bool, error) {
	prefix := rid + "."
	pending, found := false, false
	for key, job := range st.Resolved {
		if strings.HasPrefix(key, prefix) && job.SubmissionRID == key {
			if job.Owner != owner {
				return nil, true, refused("task_denied")
			}
			pending, found = true, true
		}
	}
	for _, job := range st.Jobs {
		if strings.HasPrefix(job.SubmissionRID, prefix) {
			if job.Owner != owner {
				return nil, true, refused("task_denied")
			}
			found = true
		}
	}
	if !found {
		return nil, false, nil
	}
	if pending {
		if reply, err := s.engine(ctx, "GET", "/api/v1/tasks", nil, nil); err == nil && reply.Code == 0 {
			if err = reconcileTaskList(st, reply.Data); err != nil {
				return nil, true, err
			}
		}
	}
	ids := []string{}
	for id, job := range st.Jobs {
		if strings.HasPrefix(job.SubmissionRID, prefix) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil, true, refused("engine_unavailable")
	}
	sort.Strings(ids)
	data, _ := json.Marshal(ids)
	return Reply{Data: data}, true, nil
}
