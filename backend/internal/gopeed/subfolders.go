package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// Leave room for the bounded leaf filename in the executor's 1024-byte name.
func validSubfolder(value string) bool {
	if !utf8.ValidString(value) || len(value) > 768 {
		return false
	}
	if value == "" {
		return true
	}
	parts := strings.Split(value, "/")
	if len(parts) > 16 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || len(part) > 255 || strings.TrimSpace(part) != part || strings.HasSuffix(part, ".") || strings.ContainsAny(part, `\:*?"<>|`) {
			return false
		}
		for _, r := range part {
			if unicode.IsControl(r) {
				return false
			}
		}
		switch strings.ToLower(part) {
		case "@eadir", "#recycle", "#snapshot":
			return false
		}
	}
	return true
}

func (j Job) destinationPath() string { return path.Join(j.Destination, j.Subfolder) }
func (j Job) destinationName(name string) string {
	if j.Subfolder == "" {
		return name
	}
	return j.Subfolder + "/" + name
}
func (a sourceAccess) subfolder() string {
	if a.admission != nil {
		return a.admission.Subfolder
	}
	return ""
}
func sourceSubfolder(a sourceAccess, st *State, owner string) string {
	if a.admission != nil {
		return a.subfolder()
	}
	return downloadSubfolder(st, owner)
}
func downloadSubfolder(st *State, owner string) string {
	p := preferences(st, owner)
	if p.DownloadSubfolder != nil {
		return *p.DownloadSubfolder
	}
	return "" // Existing destinations migrate to a root with an empty subfolder.
}

// Old settings stored absolute paths and their own grant. Translate only
// paths proven to be inside the current SDK root; never reinterpret an escape.
// Already admitted jobs and operation references are deliberately untouched.
func migrateMediaSubfolders(p MediaPreferences, root string) MediaPreferences {
	relative := func(value string) string {
		if value == root {
			return ""
		}
		if strings.HasPrefix(value, root+"/") {
			return strings.TrimPrefix(value, root+"/")
		}
		return value
	}
	p.Rules = append([]MediaRule{}, p.Rules...)
	for i := range p.Rules {
		r := &p.Rules[i]
		if r.Grant != "" && strings.HasPrefix(r.Path, "/") {
			r.Path = relative(r.Path)
			if validSubfolder(r.Path) {
				r.Grant = ""
			}
		}
	}
	p.Follows = append([]MediaFollow{}, p.Follows...)
	for i := range p.Follows {
		f := &p.Follows[i]
		if f.Grant != "" && strings.HasPrefix(f.Path, "/") {
			f.Path = relative(f.Path)
			if validSubfolder(f.Path) {
				f.Grant = ""
			}
		}
	}
	return p
}

type downloadLocation struct {
	delegate.DestinationState
	Subfolder string `json:"subfolder"`
	Reason    string `json:"reason,omitempty"`
}

func (s *Service) location(ctx context.Context, c api.Caller, r *http.Request, owner string) (downloadLocation, error) {
	out := downloadLocation{DestinationState: delegate.DestinationState{Key: "save", Status: "missing"}}
	if s.DestinationDelegate == nil {
		return out, refused("save_unavailable")
	}
	state, err := s.DestinationDelegate.Destination(ctx, c, r, "save")
	if err != nil {
		return out, err
	}
	out.DestinationState = state
	st, err := s.readState()
	if err != nil {
		return out, err
	}
	out.Subfolder = downloadSubfolder(&st, owner)
	if state.Status == "active" && state.Destination != nil {
		root, grant := state.Destination.Path, state.Destination.ID
		legacy := false
		p := preferences(&st, owner)
		for _, r := range p.Rules {
			legacy = legacy || r.Grant != "" && strings.HasPrefix(r.Path, "/") && (r.Path == root || strings.HasPrefix(r.Path, root+"/"))
		}
		for _, f := range p.Follows {
			legacy = legacy || f.Grant != "" && strings.HasPrefix(f.Path, "/") && (f.Path == root || strings.HasPrefix(f.Path, root+"/"))
		}
		if st.Folders[owner] != root || st.Grants[owner] != grant || legacy {
			_, err = s.withState(ctx, func(current *State) (any, error) {
				current.Folders[owner], current.Grants[owner] = root, grant
				setPreferences(current, owner, migrateMediaSubfolders(preferences(current, owner), root))
				return nil, nil
			})
			if err != nil {
				return out, err
			}
		}
	}
	if state.Status == "missing" {
		if preferences(&st, owner).DownloadSubfolder == nil {
			out.Subfolder = "Downloads"
		}
		if state.Selection == nil || state.Selection.Scope != "home" || state.Selection.Root != "/home" {
			out.Reason = "home_scope_required"
		}
	}
	return out, nil
}

func (s *Service) folderRoutes() []api.Route {
	handler := func(set bool) api.RequestHandler {
		return func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			if !set {
				if err = decodeStrict(raw, &struct{}{}); err != nil {
					return nil, err
				}
				return s.location(ctx, c, r, owner)
			}
			var in struct {
				Subfolder string `json:"subfolder"`
			}
			if err = decodeStrict(raw, &in); err != nil {
				return nil, err
			}
			if !validSubfolder(in.Subfolder) {
				return nil, refused("invalid_subfolder")
			}
			current, err := s.location(ctx, c, r, owner)
			if err != nil {
				return nil, err
			}
			if current.Status == "missing" {
				if current.Reason != "" {
					return nil, refused(current.Reason)
				}
				// Persist the chosen relative folder before grant creation. If its
				// reply is lost, a later lookup must not silently save at the root.
				_, err = s.withState(ctx, func(st *State) (any, error) {
					p := preferences(st, owner)
					p.DownloadSubfolder = &in.Subfolder
					setPreferences(st, owner, p)
					return nil, nil
				})
				if err != nil {
					return nil, err
				}
				// A fresh session and protected scope are verified by the SDK. No
				// mkdir here: the executor creates Name's ancestors at publication.
				selected, e := s.DestinationDelegate.SelectDestination(ctx, c, r, delegate.SelectDestinationParams{Key: "save", Path: "/home"})
				if e != nil {
					return nil, e
				}
				grant, e := s.currentDestination(ctx, c, r)
				if e != nil {
					return nil, e
				}
				if grant.ID != selected.ID || grant.Path != "/home" {
					return nil, refused("commit_uncertain")
				}
			} else if current.Status != "active" {
				return nil, refused("background_destination_required")
			}
			grant, err := s.currentDestination(ctx, c, r)
			if err != nil {
				return nil, err
			}
			_, err = s.withState(ctx, func(st *State) (any, error) {
				st.Folders[owner], st.Grants[owner] = grant.Path, grant.ID
				p := preferences(st, owner)
				p.DownloadSubfolder = &in.Subfolder
				setPreferences(st, owner, p)
				return nil, nil
			})
			if err != nil {
				return nil, err
			}
			return s.location(ctx, c, r, owner)
		}
	}
	return []api.Route{api.HandleRequest("downloads.location", handler(false)), api.HandleRequest("downloads.folder.set", handler(true))}
}
