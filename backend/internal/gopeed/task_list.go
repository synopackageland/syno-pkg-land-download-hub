package gopeed

import (
	"context"
	"encoding/json"
	"net/url"
)

func requestQuery(in Input) (url.Values, error) {
	if len(in.Query) > 4 {
		return nil, refused("invalid_request")
	}
	q := url.Values{}
	for k, v := range in.Query {
		if k != "status" && k != "force" {
			return nil, refused("endpoint_denied")
		}
		var single string
		var values []string
		if json.Unmarshal(v, &single) == nil {
			q.Add(k, single)
		} else if json.Unmarshal(v, &values) == nil && len(values) <= 6 {
			for _, v := range values {
				q.Add(k, v)
			}
		} else if k == "force" && (string(v) == "true" || string(v) == "false") {
			q.Set(k, string(v))
		} else {
			return nil, refused("invalid_request")
		}
	}
	return q, nil
}

func (s *Service) filterTaskList(st *State, owner string, rep Reply) (Reply, error) {
	var tasks []map[string]any
	if json.Unmarshal(rep.Data, &tasks) != nil {
		return Reply{}, refused("engine_unavailable")
	}
	if err := reconcileTaskList(st, rep.Data); err != nil {
		return Reply{}, err
	}
	hidden := map[string]bool{}
	for _, j := range st.Jobs {
		if j.Preparation != nil {
			for _, id := range j.Preparation.Children {
				hidden[id] = true
			}
		}
	}
	out := []map[string]any{}
	for _, t := range tasks {
		id, _ := t["id"].(string)
		if job, ok := st.Jobs[id]; ok && job.Owner == owner && !hidden[id] {
			s.present(t, job)
			out = append(out, t)
		}
	}
	for id, job := range st.Jobs {
		if job.Owner == owner && job.Preparation != nil {
			out = append(out, s.presentPreparation(id, job))
		}
		if job.Owner == owner && job.Media != nil && !hidden[id] {
			out = append(out, s.presentMedia(id, job))
		}
	}
	rep.Data, _ = json.Marshal(out)
	return rep, nil
}

// Lists are observations, not engine mutations. Upstream serves each HTTP GET
// as its own snapshot; an update may make that read unavailable but does not
// justify waiting for a product write lock or treating Pending as engine_busy.
func (s *Service) readTaskList(ctx context.Context, owner string, in Input) (Reply, error) {
	q, err := requestQuery(in)
	if err != nil {
		return Reply{}, err
	}
	rep, err := s.engine(ctx, "GET", in.Path, q, nil)
	if err != nil || rep.Code != 0 {
		return rep, err
	}
	// Read after HTTP: an owner deletion committed while the engine response
	// was in flight must be reflected, rather than resurrected by stale rows.
	st, err := s.readState()
	if err != nil {
		return Reply{}, err
	}
	// Exact durable C2 intents can identify an accepted task in this private
	// view. Persisting reconciliation belongs to the writer/daemon, not a GET;
	// no POST, grant change, fsync or state lock is introduced by this read.
	return s.filterTaskList(&st, owner, rep)
}
