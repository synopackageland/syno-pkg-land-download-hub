package gopeed

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"dsmapp/internal/media"
	"github.com/synopackageland/syno-pkg-land-sdk/outbound"
)

// FollowRunner borrows the daemon's existing preparation tick and state read.
// Empty subscriptions create no goroutine, timer, process or extra disk read.
type FollowRunner struct {
	Service *Service
	mu      sync.Mutex
	done    chan struct{}
	cancel  context.CancelFunc
	id      string
}

func (r *FollowRunner) Tick(ctx context.Context, st State) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done != nil {
		exists := false
		for _, p := range st.MediaPreferences {
			for _, f := range p.Follows {
				if f.ID == r.id {
					exists = true
				}
			}
		}
		if !exists {
			r.cancel()
		}
		select {
		case <-r.done:
			r.done = nil
			r.cancel()
		default:
			return
		}
	}
	for owner, p := range st.MediaPreferences {
		for _, f := range p.Follows {
			if time.Now().Before(f.NextCheck) {
				continue
			}
			work, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			r.done, r.cancel, r.id = done, cancel, f.ID
			go func() { defer close(done); defer cancel(); r.check(work, owner, f) }()
			return
		}
	}
}
func (r *FollowRunner) Close() {
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
func (r *FollowRunner) check(ctx context.Context, owner string, f MediaFollow) {
	s := r.Service
	// Never use a replacement account's cookies or queue work for an absent owner.
	_, name, ok := strings.Cut(owner, ":")
	current, e := s.Owner(name)
	if !ok || e != nil || current != owner {
		r.complete(ctx, owner, f, nil, "identity_unavailable")
		return
	}
	env := s.mediaEnv()
	if env.YtDlp == "" {
		r.complete(ctx, owner, f, nil, "media_tool_missing")
		return
	}
	if e = media.PrepareHome(env.Home); e != nil {
		r.complete(ctx, owner, f, nil, "media_failed")
		return
	}
	cookies, clean, e := s.cookies().Copy(owner, f.URL, filepath.Join(env.Home, "tmp"))
	if e != nil {
		r.complete(ctx, owner, f, nil, "media_cookies_invalid")
		return
	}
	defer clean()
	proxy, e := outbound.StartProbeProxy(ctx, outbound.ProbeProxyOptions{AllowURL: s.allowedURL})
	if e != nil {
		r.complete(ctx, owner, f, nil, "media_failed")
		return
	}
	defer proxy.Close()
	run := s.mediaExec()
	guarded := func(_ context.Context, name string, args, env []string, max int64) ([]byte, []byte, error) {
		args = append(append(append([]string{}, args[:len(args)-2]...), "--proxy", proxy.URL(), "--geo-verification-proxy", proxy.URL()), args[len(args)-2:]...)
		return run(proxy.Context(), name, args, env, max)
	}
	urls, e := media.ChannelEntries(ctx, guarded, env, f.URL, cookies)
	code := ""
	if e != nil {
		code = media.Code(e)
		if code == "" {
			code = "media_failed"
		}
	}
	if proxy.Denied() {
		code = "url_denied"
	}
	r.complete(ctx, owner, f, urls, code)
}
func (r *FollowRunner) complete(ctx context.Context, owner string, expected MediaFollow, urls []string, code string) {
	if ctx.Err() != nil {
		return
	}
	s := r.Service
	s.withState(ctx, func(st *State) (any, error) {
		p := preferences(st, owner)
		for i, f := range p.Follows {
			if f.ID != expected.ID {
				continue
			}
			f.LastCheck = time.Now().UTC()
			f.NextCheck = f.LastCheck.Add(time.Duration(f.IntervalHours) * time.Hour)
			f.Error = code
			if f.Seen == nil {
				f.Seen = map[string]bool{}
			}
			if code == "" {
				for _, u := range urls {
					if !safeURL(u) || !s.allowedURL(u) {
						continue
					}
					sum := sha256.Sum256([]byte(u))
					key := hex.EncodeToString(sum[:])
					if f.Seen[key] {
						continue
					}
					if len(f.Seen) >= 10000 {
						f.Error = "media_archive_full"
						break
					}
					if len(st.Jobs)+len(st.Resolved) >= 128 {
						f.Error = "queue_limit"
						break
					}
					options, subfolder := p.Defaults, f.Path
					root, grant := st.Folders[owner], st.Grants[owner]
					if root == "" || grant == "" || !validSubfolder(subfolder) {
						f.Error = "background_destination_required"
						break
					}
					if rule := matchingRule(p, u, f.URL); rule != nil {
						options, subfolder = rule.Options, rule.Path
						if !validSubfolder(subfolder) {
							f.Error = "invalid_subfolder"
							break
						}
					}
					// The exact same durable preparation/admission path as a pasted address.
					id := "prepare-" + randomID()
					st.Jobs[id] = Job{Subfolder: subfolder, Owner: owner, Destination: root, DelegationID: grant, Filename: u, BatchKey: "follow-" + f.ID + "-" + key[:16], BatchKind: linkMedia, Preparation: &Preparation{MediaRules: p.Rules, URL: u, Status: "queued", Started: time.Now(), MediaOptions: &options, FollowID: f.ID}}
					f.Seen[key] = true
				}
			}
			p.Follows[i] = f
			setPreferences(st, owner, p)
			break
		}
		return nil, nil
	})
}

// PurgeMediaUser is also called by account cleanup. No NAS data is removed.
// Jobs are canceled through the existing deletion path; SDK references stay.
func (s *Service) PurgeMediaUser(ctx context.Context, owner string) error {
	_, e := s.withState(ctx, func(st *State) (any, error) {
		for id, j := range st.Resolved {
			if j.Owner == owner && j.SubmissionRID == "" {
				delete(st.Resolved, id)
			}
		}
		for id, j := range st.Jobs {
			if j.Owner == owner {
				j.DeleteRequested = true
				if j.DeletePhase == "" {
					j.DeletePhase = "cancel"
					if !j.SaveStarted || j.DelegationID == "" || j.Published {
						j.DeletePhase = "engine"
						j.DeleteKept = j.Published
					}
				}
				st.Jobs[id] = j
			}
		}
		return nil, nil
	})
	if e != nil {
		return e
	}
	if e = s.cookies().Purge(owner); e != nil {
		return e
	}
	_, e = s.withState(ctx, func(st *State) (any, error) { delete(st.MediaPreferences, owner); return nil, nil })
	return e
}

// ReapDeletedMediaUsers runs during the existing staging sweep, never on a
// dedicated timer. NSS lookup failures are not deletion evidence.
func (s *Service) ReapDeletedMediaUsers(ctx context.Context) error {
	st, err := s.readState()
	if err != nil {
		return err
	}
	for owner := range st.MediaPreferences {
		_, name, ok := strings.Cut(owner, ":")
		if !ok {
			continue
		}
		_, err := user.Lookup(name)
		if _, deleted := err.(user.UnknownUserError); deleted {
			if err = s.PurgeMediaUser(ctx, owner); err != nil {
				return err
			}
		}
	}
	return nil
}
