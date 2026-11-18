package gopeed

import (
	"context"
	"encoding/json"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func (s *Service) create(ctx context.Context, r *http.Request, caller api.Caller, owner string, st *State, raw json.RawMessage) (any, error) {
	return s.createSource(ctx, sourceAccess{caller: caller, request: r, owner: owner}, owner, st, raw)
}

func (s *Service) createSource(ctx context.Context, a sourceAccess, owner string, st *State, raw json.RawMessage) (any, error) {
	body, e := decodeTask(raw)
	if e != nil {
		return nil, e
	}
	rid, _ := body["rid"].(string)
	if reply, ok, err := s.existingSubmission(ctx, st, rid, owner); ok || err != nil {
		return reply, err
	}
	if reply, ok, err := s.extensionReceipt(ctx, st, rid, owner); ok || err != nil {
		return reply, err
	}
	job, ok := st.Resolved[rid]
	if !ok || job.Owner != owner {
		return nil, refused("task_denied")
	}
	if job.DelegationID == "" {
		return nil, refused("background_destination_required")
	}
	if job.Kind == string(SourceExtension) {
		return s.createExtensionSource(ctx, a, st, rid, job)
	}
	if job.Hash != "" {
		if btActive(st, job.Hash) {
			return nil, refused("torrent_in_use")
		}
		if trackedFiles(st)+job.FileCount > maxTrackedFiles {
			return nil, refused("queue_limit")
		}
	}
	if e = a.recheck(ctx, s, job); e != nil {
		return nil, e
	}
	// fsync owner, grant and unique staging before the non-idempotent POST.
	// A marked RID is never sent again, even if the producer exits before send.
	job.SubmissionRID = rid
	st.Resolved[rid] = job
	intent, e := json.Marshal(st)
	if e != nil {
		return nil, e
	}
	if e = s.writeState(intent); e != nil {
		return nil, e
	}
	rep, e := s.engine(ctx, "POST", "/api/v1/tasks", nil, map[string]string{"rid": rid})
	var id string
	if e != nil || rep.Code != 0 || json.Unmarshal(rep.Data, &id) != nil || !engineTaskID.MatchString(id) {
		// Read only, with a small independent budget after a canceled request.
		lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		if reply, ok, err := s.existingSubmission(lookup, st, rid, owner); ok && err == nil {
			return reply, nil
		}
		if e != nil {
			return nil, e
		}
		if rep.Code != 0 {
			return rep, nil
		}
		return nil, refused("engine_unavailable")
	}
	st.Jobs[id] = job
	delete(st.Resolved, rid)
	return rep, nil
}

// recheckDestination requires the preview's grant to still be the caller's
// current background destination, with write permission.
func (s *Service) recheckDestination(ctx context.Context, r *http.Request, caller api.Caller, job Job) error {
	current, err := s.currentDestination(ctx, caller, r)
	if err != nil {
		return err
	}
	if current.ID != job.DelegationID || current.Path != job.Destination {
		return refused("background_destination_required")
	}
	return s.destination(ctx, r, caller, job.Destination)
}

var engineTaskID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func submissionReply(id string) Reply {
	raw, _ := json.Marshal(id)
	return Reply{Data: raw}
}

// Called under jobs.lock. Already accepted RIDs are receipts, never new POSTs.
func (s *Service) existingSubmission(ctx context.Context, st *State, rid, owner string) (any, bool, error) {
	for id, job := range st.Jobs {
		if job.SubmissionRID == rid {
			if job.Owner != owner {
				return nil, true, refused("task_denied")
			}
			return submissionReply(id), true, nil
		}
	}
	job, ok := st.Resolved[rid]
	if !ok || job.SubmissionRID == "" {
		return nil, false, nil
	}
	if job.Owner != owner {
		return nil, true, refused("task_denied")
	}
	reply, err := s.engine(ctx, "GET", "/api/v1/tasks", nil, nil)
	if err != nil {
		return nil, true, err
	}
	if reply.Code != 0 {
		return nil, true, refused("engine_unavailable")
	}
	if err = reconcileTaskList(st, reply.Data); err != nil {
		return nil, true, err
	}
	for id, job := range st.Jobs {
		if job.SubmissionRID == rid {
			return submissionReply(id), true, nil
		}
	}
	return nil, true, refused("engine_unavailable")
}

// Only exact package-private staging identifies a submission. URLs and names
// alone are not identities. Plan the whole reconciliation before changing state.
func reconcileTaskList(st *State, raw json.RawMessage) error {
	var tasks []struct {
		ID   string `json:"id"`
		Meta struct {
			Opts struct{ Path, Name string } `json:"opts"`
			Res  struct {
				Hash  string                        `json:"hash"`
				Files []struct{ Name, Path string } `json:"files"`
			} `json:"res"`
		} `json:"meta"`
	}
	if json.Unmarshal(raw, &tasks) != nil {
		return refused("engine_unavailable")
	}
	adopt := map[string]string{}
	claimed := map[string]bool{}
	for rid, job := range st.Resolved {
		if job.SubmissionRID == "" {
			continue
		}
		if job.SubmissionRID != rid || job.Owner == "" || job.Stage == "" || job.DelegationID == "" {
			return refused("state_invalid")
		}
		for _, task := range tasks {
			name := task.Meta.Opts.Name
			if name == "" && len(task.Meta.Res.Files) == 1 && task.Meta.Res.Files[0].Path == "" {
				name = task.Meta.Res.Files[0].Name
			}
			if task.Meta.Opts.Path != job.Stage || (job.Hash == "" && name != job.Filename) || (job.Hash != "" && !strings.EqualFold(task.Meta.Res.Hash, job.Hash)) {
				continue
			}
			if !engineTaskID.MatchString(task.ID) || adopt[rid] != "" || claimed[task.ID] {
				return refused("engine_unavailable")
			}
			if other, exists := st.Jobs[task.ID]; exists && (other.Owner != job.Owner || other.Stage != job.Stage || other.SubmissionRID != rid || other.Filename != job.Filename || other.DelegationID != job.DelegationID) {
				return refused("state_invalid")
			}
			adopt[rid], claimed[task.ID] = task.ID, true
		}
	}
	for rid, id := range adopt {
		if _, exists := st.Jobs[id]; !exists {
			st.Jobs[id] = st.Resolved[rid]
		}
		delete(st.Resolved, rid)
	}
	return nil
}

// Daemon recovery after response loss/crash; never sends a create request.
func (s *Service) ReconcileSubmissions(ctx context.Context) error {
	pending := false
	_, err := s.withState(ctx, func(st *State) (any, error) {
		for _, job := range st.Resolved {
			if job.SubmissionRID != "" {
				pending = true
				break
			}
		}
		return nil, nil
	})
	if err != nil || !pending {
		return err
	}
	reply, err := s.engine(ctx, "GET", "/api/v1/tasks", nil, nil)
	if err != nil {
		return err
	}
	if reply.Code != 0 {
		return refused("engine_unavailable")
	}
	_, err = s.withState(ctx, func(st *State) (any, error) { return nil, reconcileTaskList(st, reply.Data) })
	return err
}
