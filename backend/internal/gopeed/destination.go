package gopeed

import (
	"context"
	"encoding/json"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"net/http"
)

// This adapter records a business binding; scope selection and grant authority
// belong entirely to the SDK's protected selection and caller-bound lookup.
func (s *Service) currentDestination(ctx context.Context, c api.Caller, r *http.Request) (delegate.DestinationGrant, error) {
	if s.DestinationDelegate == nil {
		return delegate.DestinationGrant{}, refused("save_unavailable")
	}
	current, err := s.DestinationDelegate.Destination(ctx, c, r, "save")
	if err != nil {
		return delegate.DestinationGrant{}, err
	}
	if current.Status != "active" || current.Destination == nil || current.Selection == nil {
		return delegate.DestinationGrant{}, refused("background_destination_required")
	}
	grant := *current.Destination
	if grant.ID == "" || grant.Username != c.User() || grant.Key != "save" || grant.Lifetime != "until-revoked" || !validFolder(grant.Path) {
		return delegate.DestinationGrant{}, refused("commit_uncertain")
	}
	return grant, nil
}

// pickDestination asks the SDK's protected picker for a destination grant.
func (s *Service) pickDestination(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (delegate.SelectDestinationParams, delegate.DestinationGrant, error) {
	var in delegate.SelectDestinationParams
	if e := decodeStrict(raw, &in); e != nil {
		return in, delegate.DestinationGrant{}, e
	}
	if s.DestinationDelegate == nil || in.Key != "save" {
		return in, delegate.DestinationGrant{}, refused("save_unavailable")
	}
	selected, e := s.DestinationDelegate.SelectDestination(ctx, c, r, in)
	return in, selected, e
}

// bindDestination records the owner's folder once the SDK lists the picked
// grant as the active binding.
func (s *Service) bindDestination(ctx context.Context, c api.Caller, r *http.Request, in delegate.SelectDestinationParams, selected delegate.DestinationGrant, st *State, owner string) (any, error) {
	// A successful picker admission alone does not establish the active binding.
	grant, e := s.currentDestination(ctx, c, r)
	if e != nil {
		return nil, e
	}
	if grant.ID != selected.ID || grant.Path != in.Path {
		return nil, refused("commit_uncertain")
	}
	p := preferences(st, owner)
	empty := ""
	if p.DownloadSubfolder == nil {
		p.DownloadSubfolder = &empty
	}
	setPreferences(st, owner, p)
	st.Folders[owner] = grant.Path
	st.Grants[owner] = grant.ID
	// Preserve published history and uncertain attempts. Only the original
	// caller can explicitly bind an unfinished legacy job to the same folder.
	for id, job := range st.Jobs {
		if job.Owner == owner && job.Destination == grant.Path && job.DelegationID == "" && !job.Published && !job.Uncertain && !job.SaveStarted {
			job.DelegationID = grant.ID
			job.SaveError = ""
			st.Jobs[id] = job
		}
	}
	return grant, nil
}
