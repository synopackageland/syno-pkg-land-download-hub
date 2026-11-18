package gopeed

import (
	"context"
	"net/http"
	"reflect"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// sourceAccess separates online source previews from daemon work admitted by
// an authenticated create_many. A durable admission permits only package-private
// downloading. It confers no NAS write authority: the SDK revalidates the pinned
// grant when moving from staging. Never retain a session in the journal.
type sourceAccess struct {
	admissionID string
	caller      api.Caller
	request     *http.Request
	owner       string
	admission   *Job
}

func (a sourceAccess) current(ctx context.Context, s *Service) (delegate.DestinationGrant, error) {
	if a.admission != nil {
		return delegate.DestinationGrant{ID: a.admission.DelegationID, Path: a.admission.Destination}, nil
	}
	return s.currentDestination(ctx, a.caller, a.request)
}
func (a sourceAccess) destination(ctx context.Context, s *Service, folder string) error {
	if a.admission != nil {
		if folder != a.admission.Destination || a.owner != a.admission.Owner || a.admission.DelegationID == "" {
			return refused("task_denied")
		}
		return nil
	}
	return s.destination(ctx, a.request, a.caller, folder)
}
func (a sourceAccess) recheck(ctx context.Context, s *Service, j Job) error {
	grant, err := a.current(ctx, s)
	if err != nil {
		return err
	}
	if grant.ID != j.DelegationID || grant.Path != j.Destination {
		return refused("background_destination_required")
	}
	return a.destination(ctx, s, j.Destination)
}
func (a sourceAccess) locked(ctx context.Context, s *Service, fn func(string, *State) (any, error)) (any, error) {
	if a.admission == nil {
		return s.locked(ctx, a.caller, fn)
	}
	return s.lockedOwner(ctx, a.owner, func(owner string, st *State) (any, error) {
		if a.admissionID != "" {
			j, ok := st.Jobs[a.admissionID]
			if !ok || j.ManualPaused || j.DeleteRequested {
				return nil, context.Canceled
			}
		}
		return fn(owner, st)
	})
}

// resolveLocked computes source previews against a snapshot, then merges them
// under the mutation lock. Slow network work never owns jobs.lock.
func (a sourceAccess) resolveLocked(ctx context.Context, s *Service, fn func(string, *State) (any, error)) (any, error) {
	if a.admission == nil {
		return a.locked(ctx, s, fn)
	}
	st, err := s.readState()
	if err != nil {
		return nil, err
	}
	if a.admissionID != "" {
		delete(st.Jobs, a.admissionID)
	}
	before := map[string]Job{}
	for k, v := range st.Resolved {
		before[k] = v
	}
	out, err := fn(a.owner, &st)
	if err != nil {
		return nil, err
	}
	_, err = a.locked(ctx, s, func(_ string, current *State) (any, error) {
		for k := range before {
			if _, ok := st.Resolved[k]; !ok && reflect.DeepEqual(current.Resolved[k], before[k]) {
				delete(current.Resolved, k)
			}
		}
		for k, v := range st.Resolved {
			if reflect.DeepEqual(v, before[k]) {
				continue
			}
			current.Resolved[k] = v
		}
		return nil, nil
	})
	return out, err
}
