package gopeed

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"dsmapp/internal/binaries"
)

type sourcePost struct {
	key     string
	job     Job
	payload any
}

// submitPreparedSource holds the engine mutation fence across create, but owns
// jobs.lock only while recording the intent/result. Slow create IPC cannot hold
// a new caller's admission hostage. A persisted intent is never POSTed again.
func (s *Service) submitPreparedSource(ctx context.Context, a sourceAccess, rid, key, link string) (string, []string, error) {
	unlock, err := (binaries.Manager{Data: s.Data}).Lock(ctx)
	if err != nil {
		return "", nil, refused("engine_busy")
	}
	defer unlock()
	if (binaries.Manager{Data: s.Data}).Pending() {
		return "", nil, refused("engine_busy")
	}
	var posts []sourcePost
	kind := ""
	_, err = s.withState(ctx, func(st *State) (any, error) {
		parent, ok := st.Jobs[a.admissionID]
		if !ok || parent.Owner != a.owner || parent.ManualPaused || parent.DeleteRequested {
			return nil, context.Canceled
		}
		j, ok := st.Resolved[rid]
		if !ok || j.Owner != a.owner {
			return nil, refused("task_denied")
		}
		if err := a.recheck(ctx, s, j); err != nil {
			return nil, err
		}
		kind = jobKind(j, link)
		j.BatchKey, j.BatchKind = key, kind
		if j.SubmissionRID != "" {
			return nil, refused("engine_unavailable")
		}
		if j.Hash != "" && trackedFiles(st)+j.FileCount > maxTrackedFiles {
			return nil, refused("queue_limit")
		}
		if j.Hash != "" && btActive(st, j.Hash) {
			return nil, refused("torrent_in_use")
		}
		if j.Kind != string(SourceExtension) {
			j.SubmissionRID = rid
			st.Resolved[rid] = j
			posts = append(posts, sourcePost{key: rid, job: j, payload: map[string]string{"rid": rid}})
			return nil, nil
		}
		if j.FileCount < 1 || len(j.Requests) != j.FileCount {
			return nil, refused("state_invalid")
		}
		if len(st.Jobs)+len(st.Resolved)-1+j.FileCount > 128 {
			return nil, refused("queue_limit")
		}
		for i, request := range j.Requests {
			stage := filepath.Join(s.StagingDir, randomID())
			if err := os.MkdirAll(stage, 0700); err != nil {
				return nil, err
			}
			childKey := rid + "." + strconv.Itoa(i)
			child := Job{Owner: j.Owner, Stage: stage, Destination: j.Destination, Filename: request.Name, DelegationID: j.DelegationID, SubmissionRID: childKey, BatchKey: key, BatchKind: kind}
			st.Resolved[childKey] = child
			posts = append(posts, sourcePost{key: childKey, job: child, payload: map[string]any{"req": request.Req, "opts": map[string]any{"path": stage, "name": request.Name}}})
		}
		delete(st.Resolved, rid)
		return nil, nil
	})
	if err != nil {
		return kind, nil, err
	}
	// withState has fsynced every owner/grant/stage before the first side effect.
	for _, post := range posts {
		rep, e := s.engine(ctx, "POST", "/api/v1/tasks", nil, post.payload)
		var id string
		if e != nil || rep.Code != 0 || json.Unmarshal(rep.Data, &id) != nil || !engineTaskID.MatchString(id) {
			continue
		}
		persist, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.saveBudget())
		_, e = s.withState(persist, func(st *State) (any, error) {
			intent, ok := st.Resolved[post.key]
			if !ok || intent.Owner != post.job.Owner || intent.Stage != post.job.Stage {
				return nil, refused("state_invalid")
			}
			st.Jobs[id] = intent
			delete(st.Resolved, post.key)
			return nil, nil
		})
		cancel()
		if e != nil {
			break
		}
	}
	lookup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	// Recovery queries only; even a canceled request cannot send another create.
	_ = s.ReconcileSubmissions(lookup)
	st, err := s.readState()
	if err != nil {
		return kind, nil, err
	}
	ids := []string{}
	for id, j := range st.Jobs {
		if j.Owner == a.owner && j.BatchKey == key && j.Preparation == nil {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return kind, nil, refused("submission_pending")
	}
	return kind, ids, nil
}
