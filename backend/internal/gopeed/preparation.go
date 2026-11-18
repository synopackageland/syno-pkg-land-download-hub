package gopeed

import (
	"context"
	"dsmapp/internal/media"
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"
)

// Preparation is a durable, owner-bound source request, never a DSM session.
type Preparation struct {
	MediaRules    []MediaRule    `json:"mediaRules,omitempty"`
	MediaOverride bool           `json:"mediaOverride,omitempty"`
	MediaOptions  *media.Options `json:"mediaOptions,omitempty"`
	FollowID      string         `json:"followId,omitempty"`
	Children      []string       `json:"children,omitempty"`
	URL           string         `json:"url"`
	Status        string         `json:"status"`
	Error         string         `json:"error,omitempty"`
	Started       time.Time      `json:"started"`
	// Waiting: the last attempt ended while peers had not sent the torrent's
	// information yet. The request keeps waiting until its owner pauses or
	// clears it; Attempted orders the waiting requests so none starves others.
	Waiting   bool      `json:"waiting,omitempty"`
	Attempted time.Time `json:"attempted,omitempty"`
}

func (s *Service) presentPreparation(id string, j Job) map[string]any {
	status := "preparing"
	if j.ManualPaused {
		status = "pause"
	} else if j.Preparation.Status == "error" {
		status = "error"
	}
	t := map[string]any{"id": id, "status": status, "preparation": true, "waiting": status == "preparing" && j.Preparation.Waiting, "meta": map[string]any{"opts": map[string]any{}, "res": map[string]any{"name": j.Filename}}, "progress": map[string]any{}}
	s.present(t, j)
	t["publication"].(map[string]any)["error"] = j.Preparation.Error
	return t
}
func (s *Service) preparationRequest(st *State, id string, j Job, in Input, op string) (any, error) {
	if in.Method == "GET" && op == "" {
		b, _ := json.Marshal(s.presentPreparation(id, j))
		return Reply{Data: b}, nil
	}
	if in.Method != "PUT" || (op != "/pause" && op != "/continue") {
		return nil, refused("endpoint_denied")
	}
	p := *j.Preparation
	j.Preparation = &p
	j.ManualPaused = op == "/pause"
	if !j.ManualPaused {
		p.Status, p.Error, p.Started = "queued", "", time.Now()
		p.Waiting, p.Attempted = false, time.Time{}
	}
	st.Jobs[id] = j
	return Reply{Data: json.RawMessage(`true`)}, nil
}

// PreparationRunner shares the existing daemon tick. One source is resolved at
// a time; it never holds jobs.lock during a probe or metadata wait.
type PreparationRunner struct {
	Service          *Service
	follows          FollowRunner
	nextMediaCleanup time.Time
	mu               sync.Mutex
	id               string
	cancel           context.CancelFunc
	done             chan struct{}
	lastOwner        string
}

func (r *PreparationRunner) Tick(ctx context.Context) error {
	st, err := r.Service.readState()
	if err != nil {
		return err
	}
	if len(st.MediaPreferences) > 0 && time.Now().After(r.nextMediaCleanup) {
		_ = r.Service.ReapDeletedMediaUsers(ctx)
		r.nextMediaCleanup = time.Now().Add(time.Hour)
	}
	if r.follows.Service == nil {
		r.follows.Service = r.Service
	}
	r.follows.Tick(ctx, st)
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done != nil {
		j, ok := st.Jobs[r.id]
		if !ok || j.ManualPaused || j.DeleteRequested {
			r.cancel()
		}
		select {
		case <-r.done:
			r.done = nil
			r.cancel()
		default:
			return nil
		}
	}
	candidates := map[string]Job{}
	for id, j := range st.Jobs {
		if j.Preparation != nil && !j.ManualPaused && !j.DeleteRequested && j.Preparation.Status != "error" {
			candidates[id] = j
		}
		if j.Preparation != nil && j.DeleteRequested {
			r.Service.clearPreparation(ctx, id, j)
		}
	}
	for _, id := range r.order(candidates) {
		j := st.Jobs[id]
		work, cancel := context.WithTimeout(ctx, 3*time.Minute)
		r.id, r.cancel, r.done = id, cancel, make(chan struct{})
		r.lastOwner = j.Owner
		done := r.done
		go func() { defer close(done); defer cancel(); r.prepare(work, id, j) }()
		break
	}
	return nil
}

// order puts requests never tried yet first, fairly across owners; then the
// requests still waiting for torrent information, least recently tried first.
func (r *PreparationRunner) order(candidates map[string]Job) []string {
	fresh, waiting := map[string]Job{}, []string{}
	for id, j := range candidates {
		if j.Preparation.Attempted.IsZero() {
			fresh[id] = j
		} else {
			waiting = append(waiting, id)
		}
	}
	slices.SortFunc(waiting, func(a, b string) int {
		if c := candidates[a].Preparation.Attempted.Compare(candidates[b].Preparation.Attempted); c != 0 {
			return c
		}
		return strings.Compare(a, b)
	})
	return append(fairJobIDs(fresh, nil, r.lastOwner), waiting...)
}
func (r *PreparationRunner) Close() {
	r.follows.Close()
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}
func (r *PreparationRunner) prepare(ctx context.Context, id string, j Job) {
	s := r.Service
	if len(j.Preparation.Children) > 0 {
		for _, child := range j.Preparation.Children {
			if _, err := s.controlTask(ctx, j.Owner, child, "continue"); err != nil {
				// A child that is gone or being deleted cannot resume; fail
				// the parent so the runner moves on to other jobs.
				if code := errorCodeOf(err); code == "task_denied" || code == "task_deleting" {
					s.withState(ctx, func(st *State) (any, error) {
						cur, ok := st.Jobs[id]
						if ok && cur.Preparation != nil && !cur.ManualPaused && !cur.DeleteRequested {
							p := *cur.Preparation
							p.Status, p.Error = "error", code
							cur.Preparation = &p
							st.Jobs[id] = cur
						}
						return nil, nil
					})
				}
				return
			}
		}
		s.withState(ctx, func(st *State) (any, error) {
			cur, ok := st.Jobs[id]
			if ok && !cur.ManualPaused && !cur.DeleteRequested {
				delete(st.Jobs, id)
			}
			return nil, nil
		})
		return
	}
	a := sourceAccess{owner: j.Owner, admission: &j, admissionID: id}
	_, ids, err := s.createLinkSource(ctx, a, j.Owner, j.Destination, j.BatchKey, j.Preparation.URL)
	persist, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s.withState(persist, func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if !ok || cur.Preparation == nil {
			return nil, nil
		}
		if len(ids) > 0 {
			// Pausing just as create completed must also pause the accepted children.
			// The pending parent stays until these mutations have acknowledged.
			if cur.ManualPaused || cur.DeleteRequested {
				p := *cur.Preparation
				p.Children = ids
				cur.Preparation = &p
				st.Jobs[id] = cur
				return nil, nil
			}
			delete(st.Jobs, id)
			return nil, nil
		}
		if cur.ManualPaused || cur.DeleteRequested || ctx.Err() == context.Canceled {
			return nil, nil
		}
		p := *cur.Preparation
		cur.Preparation = &p
		code := errorCodeOf(err)
		// No wall-clock limit on peers: a slow magnet keeps waiting, shown as
		// such, until its owner pauses or clears it. The attempt deadline only
		// returns the worker to other requests between waits.
		if code == "metadata_pending" || ctx.Err() == context.DeadlineExceeded {
			p.Waiting, p.Attempted = true, time.Now()
			st.Jobs[id] = cur
			return nil, nil
		}
		if code == "submission_pending" {
			return nil, nil
		}
		p.Status, p.Error = "error", code
		st.Jobs[id] = cur
		return nil, nil
	})
	// A pause/delete racing the create is applied outside jobs.lock.
	st, e := s.readState()
	if e != nil {
		return
	}
	cur, ok := st.Jobs[id]
	if !ok {
		return
	}
	if len(ids) > 0 && (cur.ManualPaused || cur.DeleteRequested) {
		for _, child := range ids {
			if cur.DeleteRequested {
				s.deleteTask(persist, cur.Owner, child)
			} else {
				s.controlTask(persist, cur.Owner, child, "pause")
			}
		}
	}
}

// Clearing a source request first waits for its worker to stop. Accepted engine
// intents are reconciled, never re-posted or silently forgotten.
func (s *Service) clearPreparation(ctx context.Context, id string, j Job) error {
	_, ids, _, err := s.batchReceipt(ctx, j.Owner, j.BatchKey)
	if err != nil {
		return err
	}
	for _, child := range ids {
		if _, err = s.deleteTask(ctx, j.Owner, child); err != nil {
			return err
		}
	}
	_, err = s.withState(ctx, func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if !ok || !cur.DeleteRequested {
			return nil, nil
		}
		for _, x := range st.Resolved {
			if x.Owner == j.Owner && x.BatchKey == j.BatchKey && x.SubmissionRID != "" {
				return nil, nil
			}
		}
		for child, x := range st.Jobs {
			if child != id && x.Owner == j.Owner && x.BatchKey == j.BatchKey {
				return nil, nil
			}
		}
		delete(st.Jobs, id)
		return nil, nil
	})
	return err
}
