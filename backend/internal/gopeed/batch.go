package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"dsmapp/internal/media"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// maxBatchItems bounds one batch; the dialog sends longer lists in parts.
const maxBatchItems = 32

// Kinds a link is reported as. The backend decides; the user never picks one.
const (
	linkFile        = "file"
	linkTorrent     = "torrent"
	linkED2K        = "ed2k"
	linkMedia       = "media"
	linkExtension   = "extension"
	linkUnsupported = "unsupported"
	// linkBlocked is a recognized address the network floor refuses:
	// loopback, private or link-local hosts and sign-in details.
	linkBlocked = "blocked"
)

var batchKey = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)

type batchResult struct {
	Key    string   `json:"key"`
	Kind   string   `json:"kind,omitempty"`
	Status string   `json:"status"`
	IDs    []string `json:"ids,omitempty"`
	Code   string   `json:"code,omitempty"`
}

func (s *Service) batchRoutes() []api.Route {
	return []api.Route{
		api.HandleRequest("downloads.create_many", func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			return s.createMany(ctx, c, r, owner, raw)
		}),
		api.HandleRequest("downloads.select_files", func(ctx context.Context, c api.Caller, _ *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			return s.selectFiles(ctx, owner, raw)
		}),
		api.Handle("downloads.classify", func(ctx context.Context, _ api.Caller, raw json.RawMessage) (any, error) {
			return s.classify(ctx, raw)
		}),
	}
}

// createMany creates one or more downloads per link into the caller's
// background destination. Each link succeeds or fails on its own. A key
// already used by the caller answers with the tasks it created, so a
// repeated batch after a lost reply never downloads a link twice.
func (s *Service) createMany(ctx context.Context, c api.Caller, r *http.Request, owner string, raw json.RawMessage) (any, error) {
	var in struct {
		Path         string         `json:"path"`
		MediaOptions *media.Options `json:"mediaOptions,omitempty"`
		Items        []struct {
			Key string `json:"key"`
			URL string `json:"url"`
		} `json:"items"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	if len(in.Items) == 0 || len(in.Items) > maxBatchItems {
		return nil, refused("invalid_request")
	}
	if in.MediaOptions != nil {
		o, e := in.MediaOptions.Normalize()
		if e != nil {
			return nil, mediaRefused(e)
		}
		in.MediaOptions = &o
	}
	seen := map[string]bool{}
	for _, it := range in.Items {
		if !batchKey.MatchString(it.Key) || it.URL == "" || seen[it.Key] {
			return nil, refused("invalid_request")
		}
		seen[it.Key] = true
	}
	// A lost reply remains an accepted receipt even if a rule selected a
	// different current SDK folder. Do not reselect or re-admit existing keys.
	if st, e := s.readState(); e == nil {
		results := make([]batchResult, 0, len(in.Items))
		for _, it := range in.Items {
			result := batchResult{Key: it.Key, Status: "created"}
			for id, j := range st.Jobs {
				if j.Owner == owner && j.BatchKey == it.Key {
					result.IDs = append(result.IDs, id)
					result.Kind = j.BatchKind
				}
			}
			if len(result.IDs) == 0 {
				break
			}
			results = append(results, result)
		}
		if len(results) == len(in.Items) {
			return map[string]any{"results": results}, nil
		}
	}
	// Validate the grant root, never the not-yet-created subfolder.
	grant, err := s.currentDestination(ctx, c, r)
	if err != nil {
		return nil, err
	}

	snapshot, err := s.readState()
	if err != nil {
		return nil, err
	}
	prefs := migrateMediaSubfolders(preferences(&snapshot, owner), grant.Path)
	if !validSubfolder(downloadSubfolder(&snapshot, owner)) {
		return nil, refused("invalid_subfolder")
	}
	if in.Path != grant.Path && in.Path != path.Join(grant.Path, downloadSubfolder(&snapshot, owner)) {
		return nil, refused("background_destination_required")
	}
	if err := s.destination(ctx, r, c, grant.Path); err != nil {
		return nil, err
	}
	return s.withState(ctx, func(st *State) (any, error) {
		st.Folders[owner], st.Grants[owner] = grant.Path, grant.ID
		results := make([]batchResult, len(in.Items))
		for i, it := range in.Items {
			result := batchResult{Key: it.Key, Status: "created"}
			found := false
			for id, j := range st.Jobs {
				if j.Owner == owner && j.BatchKey == it.Key {
					result.IDs = append(result.IDs, id)
					result.Kind = j.BatchKind
					found = true
				}
			}
			// Submission intents are never sent twice, even after a lost create reply.
			for _, j := range st.Resolved {
				if j.Owner == owner && j.BatchKey == it.Key && j.SubmissionRID != "" {
					found = true
					result.Status, result.Code = "pending", "deferred"
				}
			}
			if !found {
				kind := s.linkKind(it.URL, nil)
				source, _ := ClassifySource(it.URL, nil)
				var formatErr error
				if source == SourceTorrentFile || source == SourceMagnet {
					_, formatErr = s.btSource(ctx, source, it.URL)
				}

				switch {
				case formatErr != nil:
					result.Status, result.Code = "failed", errorCodeOf(formatErr)
				case kind == linkUnsupported || kind == linkBlocked:
					result.Status, result.Code = "failed", "url_denied"
				case len(st.Jobs)+len(st.Resolved) >= 128:
					result.Status, result.Code = "failed", "queue_limit"
				default:
					id := "prepare-" + randomID()
					name := it.URL
					if strings.HasPrefix(name, torrentDataPrefix) {
						name = "Torrent"
					}
					options := prefs.Defaults
					if in.MediaOptions != nil {
						options = *in.MediaOptions
					}
					dest, grantID := grant.Path, grant.ID
					subfolder := downloadSubfolder(&snapshot, owner)
					if rule := matchingRule(prefs, it.URL, ""); rule != nil {
						if !validSubfolder(rule.Path) {
							result.Status, result.Code = "failed", "invalid_subfolder"
							results[i] = result
							continue
						}
						subfolder = rule.Path
						if in.MediaOptions == nil {
							options = rule.Options
						}
					}
					st.Jobs[id] = Job{Subfolder: subfolder, Owner: owner, Destination: dest, DelegationID: grantID, Filename: name, BatchKey: it.Key, BatchKind: kind, Preparation: &Preparation{MediaRules: prefs.Rules, MediaOverride: in.MediaOptions != nil, MediaOptions: &options, URL: it.URL, Status: "queued", Started: time.Now()}}
					result.IDs, result.Kind = []string{id}, kind
				}
			}
			results[i] = result
		}
		return map[string]any{"results": results}, nil
	})
}

func errorCodeOf(err error) string {
	var e *api.Error
	if errors.As(err, &e) && e.Code != "" {
		return e.Code
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "engine_unavailable"
}

// createLink routes one link: media pages to the media downloader, every
// other address through the engine's resolve and create, with all files of
// a torrent or extension result.
func (s *Service) createLink(ctx context.Context, c api.Caller, r *http.Request, owner, folder, key, link string) (string, []string, error) {
	return s.createLinkSource(ctx, sourceAccess{caller: c, request: r, owner: owner}, owner, folder, key, link)
}

func (s *Service) createLinkSource(ctx context.Context, a sourceAccess, owner, folder, key, link string) (string, []string, error) {
	if kind, ids, found, err := s.batchReceipt(ctx, owner, key); found || err != nil {
		return kind, ids, err
	}
	link, resolveErr := s.resolveSearchLink(ctx, owner, link)
	if resolveErr != nil {
		return "", nil, resolveErr
	}
	if strings.HasPrefix(key, "search-") {
		if ids, err := s.existingSearchContent(ctx, owner, link); err != nil {
			return "", nil, err
		} else if len(ids) > 0 {
			return linkTorrent, ids, nil
		}
	}
	probed, err := s.probeMediaSource(ctx, a, owner, mustMarshal(map[string]string{"url": link, "path": folder}))
	if err != nil {
		return "", nil, err
	}
	p := probed.(map[string]any)
	if p["kind"] == SourceMedia {
		if a.admission != nil {
			st, e := s.readState()
			if e != nil {
				return "", nil, e
			}
			resolved := st.Resolved[p["rid"].(string)]
			copy := *a.admission
			copy.Destination, copy.DelegationID, copy.Subfolder = resolved.Destination, resolved.DelegationID, resolved.Subfolder
			a.admission = &copy
		}
		return s.queueMediaSource(ctx, a, owner, key, p["rid"].(string))
	}
	if p["toolMissing"] == true {
		return linkMedia, nil, refused("media_tool_missing")
	}
	resolved, err := s.resolveAdmittedSource(ctx, a, mustMarshal(map[string]any{"req": map[string]string{"url": link}, "opts": map[string]string{"path": folder}}))
	if err != nil {
		return "", nil, err
	}
	var data struct {
		ID string `json:"id"`
	}
	if rep, ok := resolved.(Reply); !ok || rep.Code != 0 || json.Unmarshal(rep.Data, &data) != nil || data.ID == "" {
		return "", nil, refused("source_unsupported")
	}
	return s.submitResolvedSource(ctx, a, data.ID, key, link)
}

// submitResolved tags the caller's resolve with the link's key and creates it.
func (s *Service) submitResolved(ctx context.Context, c api.Caller, r *http.Request, rid, key, link string) (string, []string, error) {
	return s.submitResolvedSource(ctx, sourceAccess{caller: c, request: r}, rid, key, link)
}

func (s *Service) submitResolvedSource(ctx context.Context, a sourceAccess, rid, key, link string) (string, []string, error) {
	if a.admission != nil {
		return s.submitPreparedSource(ctx, a, rid, key, link)
	}
	kind := ""
	out, err := a.locked(ctx, s, func(owner string, st *State) (any, error) {
		job, ok := st.Resolved[rid]
		if !ok || job.Owner != owner {
			return nil, refused("task_denied")
		}
		kind = jobKind(job, link)
		job.BatchKey, job.BatchKind = key, kind
		st.Resolved[rid] = job
		return s.createSource(ctx, a, owner, st, mustMarshal(map[string]string{"rid": rid}))
	})
	if err != nil {
		return kind, nil, err
	}
	rep, ok := out.(Reply)
	if !ok || rep.Code != 0 {
		return kind, nil, refused("engine_unavailable")
	}
	var id string
	if json.Unmarshal(rep.Data, &id) == nil {
		return kind, []string{id}, nil
	}
	var ids []string
	if json.Unmarshal(rep.Data, &ids) != nil || len(ids) == 0 {
		return kind, nil, refused("engine_unavailable")
	}
	return kind, ids, nil
}

// queueMedia queues the caller's media probe at the best quality.
func (s *Service) queueMedia(ctx context.Context, c api.Caller, r *http.Request, owner, key, rid string) (string, []string, error) {
	return s.queueMediaSource(ctx, sourceAccess{caller: c, request: r, owner: owner}, owner, key, rid)
}

func (s *Service) queueMediaSource(ctx context.Context, a sourceAccess, owner, key, rid string) (string, []string, error) {
	_, err := s.withState(ctx, func(st *State) (any, error) {
		if job, ok := st.Resolved[rid]; ok && job.Owner == owner {
			job.BatchKey, job.BatchKind = key, linkMedia
			st.Resolved[rid] = job
		}
		return nil, nil
	})
	if err != nil {
		return linkMedia, nil, err
	}
	payload := map[string]any{"rid": rid, "format": "best"}
	st, e := s.readState()
	if e != nil {
		return linkMedia, nil, e
	}
	if j, ok := st.Resolved[rid]; ok && j.Media != nil && j.Media.Options.Format != "" {
		payload["options"] = j.Media.Options
		payload["format"] = j.Media.Options.Format
	}
	out, err := s.createMediaSource(ctx, a, owner, mustMarshal(payload))
	if err != nil {
		return linkMedia, nil, err
	}
	return linkMedia, []string{out.(map[string]string)["id"]}, nil
}

// batchReceipt finds the caller's tasks of an earlier batch link, first
// adopting submissions whose reply was lost. A link is never posted again.
func (s *Service) batchReceipt(ctx context.Context, owner, key string) (kind string, ids []string, found bool, err error) {
	_ = s.ReconcileSubmissions(ctx)
	_, err = s.withState(ctx, func(st *State) (any, error) {
		for _, job := range st.Resolved {
			if job.Owner == owner && job.BatchKey == key && job.SubmissionRID != "" {
				found, kind = true, job.BatchKind
			}
		}
		for id, job := range st.Jobs {
			if job.Owner == owner && job.BatchKey == key && job.Preparation == nil {
				found, kind = true, job.BatchKind
				ids = append(ids, id)
			}
		}
		return nil, nil
	})
	sort.Strings(ids)
	if err == nil && found && len(ids) == 0 {
		err = refused("submission_pending")
	}
	return kind, ids, found, err
}

func jobKind(job Job, link string) string {
	switch {
	case job.Kind == string(SourceExtension):
		return linkExtension
	case job.Hash != "":
		return linkTorrent
	case ed2kAddress(link):
		return linkED2K
	}
	return linkFile
}

func ed2kAddress(link string) bool {
	l := strings.ToLower(link)
	return strings.HasPrefix(l, "ed2k:") || (strings.HasPrefix(l, "magnet:") && strings.Contains(l, "xt=urn:ed2k:"))
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

// maxClassifyLinks bounds one preview request.
const maxClassifyLinks = 256

// classify previews the selected downloader without admitting a download.
// HTTP(S) gets one guarded probe; protocol links and refused inputs never do.
func (s *Service) classify(ctx context.Context, raw json.RawMessage) (any, error) {
	var in struct {
		URLs []string `json:"urls"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	if len(in.URLs) == 0 || len(in.URLs) > maxClassifyLinks {
		return nil, refused("invalid_request")
	}
	patterns := s.extensionPatterns(ctx)
	results := make([]map[string]string, len(in.URLs))
	for i, link := range in.URLs {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		kind := s.linkKind(link, patterns)
		row := map[string]string{"url": link, "kind": kind, "route": "gopeed"}
		source, sourceErr := ClassifySource(link, nil)
		u, _ := url.Parse(link)
		if sourceErr == nil && source == SourceEngine && kind != linkBlocked && u != nil && (u.Scheme == "http" || u.Scheme == "https") {
			info, err := s.inspectMedia(ctx, link)
			row["kind"] = linkFile
			if kind == linkExtension {
				row["kind"] = linkExtension
			}
			if info.Kind == "media" || media.Code(err) != "" {
				row["kind"], row["route"] = linkMedia, mediaRoute(info)
			}
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				row["code"] = errorCodeOf(err)
				if media.Code(err) != "" {
					row["code"] = media.Code(err)
				}
				if row["code"] == "url_denied" {
					row["kind"], row["route"] = linkBlocked, ""
				}
			}
		}
		if kind == linkBlocked || kind == linkUnsupported {
			row["route"] = ""
		}
		results[i] = row
	}
	return map[string]any{"results": results}, nil
}

func (s *Service) linkKind(link string, patterns []string) string {
	kind, err := ClassifySource(link, func(u *url.URL) bool { return media.KnownMediaSite(u.String()) })
	if err != nil {
		return linkUnsupported
	}
	if kind != SourceTorrentFile && !s.allowedURL(link) {
		return linkBlocked
	}
	switch kind {
	case SourceMagnet, SourceTorrentURL, SourceTorrentFile:
		return linkTorrent
	case SourceMedia:
		return linkMedia
	}
	if ed2kAddress(link) {
		return linkED2K
	}
	for _, p := range patterns {
		if matchURLPattern(p, link) {
			return linkExtension
		}
	}
	return linkFile
}

// extensionPatterns lists the resolve URL patterns of enabled extensions.
// An engine that does not answer leaves extension links shown as files.
func (s *Service) extensionPatterns(ctx context.Context) []string {
	lctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	rep, err := s.engine(lctx, "GET", "/api/v1/extensions", nil, nil)
	if err != nil || rep.Code != 0 {
		return nil
	}
	var list []struct {
		Disabled bool `json:"disabled"`
		Scripts  []struct {
			Event string `json:"event"`
			Match struct {
				URLs []string `json:"urls"`
			} `json:"match"`
		} `json:"scripts"`
	}
	if json.Unmarshal(rep.Data, &list) != nil {
		return nil
	}
	var out []string
	for _, ext := range list {
		if ext.Disabled {
			continue
		}
		for _, script := range ext.Scripts {
			if script.Event == "onResolve" {
				out = append(out, script.Match.URLs...)
			}
		}
	}
	return out
}

// matchURLPattern matches a browser-style pattern such as
// "*://*.example.com/*": "*" scheme is HTTP or HTTPS, "*." hosts include
// subdomains and "*" in the path matches any text.
func matchURLPattern(pattern, link string) bool {
	scheme, rest, ok := strings.Cut(pattern, "://")
	u, err := url.Parse(link)
	if !ok || err != nil {
		return false
	}
	if scheme == "*" {
		if u.Scheme != "http" && u.Scheme != "https" {
			return false
		}
	} else if !strings.EqualFold(scheme, u.Scheme) {
		return false
	}
	host, path, _ := strings.Cut(rest, "/")
	h := strings.ToLower(u.Hostname())
	host = strings.ToLower(host)
	switch {
	case host == "*":
	case strings.HasPrefix(host, "*."):
		if h != host[2:] && !strings.HasSuffix(h, host[1:]) {
			return false
		}
	case h != host:
		return false
	}
	glob := "^/" + strings.ReplaceAll(regexp.QuoteMeta(path), `\*`, ".*") + "$"
	re, err := regexp.Compile(glob)
	return err == nil && re.MatchString(u.EscapedPath()+queryPart(u))
}

func queryPart(u *url.URL) string {
	if u.RawQuery == "" {
		return ""
	}
	return "?" + u.RawQuery
}
