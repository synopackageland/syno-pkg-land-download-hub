package gopeed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// btResolveBudget bounds one wait for magnet metadata inside the SDK's CGI
// request limit. Upstream resolve blocks until peers deliver the info
// dictionary and keeps waiting after this request gives up, so the client
// polls with the same resolve and a later one answers from the engine.
const btResolveBudget = 12 * time.Second

// maxTrackedFiles bounds the per-file records of all jobs, keeping jobs.json
// within its read limit.
const maxTrackedFiles = 4096

// btSource is a pasted or uploaded BT source, checked before any lock or
// engine call.
type btSource struct {
	kind    SourceKind
	hash    string
	torrent []byte
	magnet  string
}

// resolveSource routes one address of the single download input. BT metadata
// waits run outside the engine lock so other users are not blocked by peers.
func (s *Service) resolveSource(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
	return s.resolveAdmittedSource(ctx, sourceAccess{caller: c, request: r}, raw)
}

func (s *Service) resolveAdmittedSource(ctx context.Context, a sourceAccess, raw json.RawMessage) (any, error) {
	body, err := decodeTask(raw)
	if err != nil {
		return nil, err
	}
	req, _ := body["req"].(map[string]any)
	rawURL, _ := req["url"].(string)
	kind, err := ClassifySource(rawURL, s.Media)
	if err != nil {
		return nil, err
	}
	if kind == SourceMedia {
		return nil, refused("media_probe_required")
	}
	if kind == SourceEngine && strings.HasPrefix(strings.ToLower(rawURL), "http") && s.allowedURL(rawURL) && s.servesTorrent(ctx, rawURL) {
		kind = SourceTorrentURL
	}
	if kind == SourceEngine {
		return a.resolveLocked(ctx, s, func(owner string, st *State) (any, error) {
			return s.resolveSourceStep(ctx, a, owner, st, raw)
		})
	}
	src, err := s.btSource(ctx, kind, rawURL)
	if err != nil {
		return nil, err
	}
	opts, _ := body["opts"].(map[string]any)
	folder, _ := opts["path"].(string)
	admitted, err := a.resolveLocked(ctx, s, func(owner string, st *State) (any, error) {
		return s.admitBTSource(ctx, a, owner, st, folder, src)
	})
	if err != nil {
		return nil, err
	}
	job := admitted.(Job)
	engineURL := src.magnet
	if engineURL == "" {
		engineURL = s.torrentPath(src.hash)
	}
	rctx, cancel := context.WithTimeout(ctx, s.metadataBudget())
	defer cancel()
	rep, err := s.engineVia(rctx, s.slowHTTP(), "POST", "/api/v1/resolve", nil, map[string]any{"req": map[string]any{"url": engineURL}, "opts": map[string]any{"path": job.Stage}})
	if err != nil || rep.Code != 0 {
		s.dropUnresolvedStage(ctx, job.Stage)
	}
	if err != nil {
		if rctx.Err() != nil && ctx.Err() == nil {
			return nil, refused("metadata_pending")
		}
		return nil, err
	}
	if rep.Code != 0 {
		return rep, nil
	}
	return a.resolveLocked(ctx, s, func(owner string, st *State) (any, error) {
		return s.commitBT(owner, st, job, rep)
	})
}

// dropUnresolvedStage removes the empty hash stage of a resolve that did not
// answer, unless another preview or task of the same hash uses it.
func (s *Service) dropUnresolvedStage(ctx context.Context, stage string) {
	_, _ = s.withState(context.WithoutCancel(ctx), func(st *State) (any, error) {
		s.removeEmptyStage(st, stage)
		return nil, nil
	})
}

func (s *Service) allowedURL(raw string) bool {
	if s.checkURL != nil {
		return s.checkURL(raw)
	}
	return safeSource(raw)
}

func (s *Service) client() *http.Client {
	if s.fetch != nil {
		return s.fetch
	}
	return publicClient()
}

func (s *Service) metadataBudget() time.Duration {
	if s.metadataWait > 0 {
		return s.metadataWait
	}
	return btResolveBudget
}

// slowHTTP is the engine client for BT metadata, which may take many seconds.
func (s *Service) slowHTTP() *http.Client {
	// A second past the metadata budget, so that budget is what expires.
	wait := s.metadataBudget() + time.Second
	c := *s.HTTP
	c.Timeout = wait
	if t, ok := c.Transport.(*http.Transport); ok {
		t = t.Clone()
		t.ResponseHeaderTimeout = wait
		c.Transport = t
	}
	return &c
}

// servesTorrent recognizes .torrent downloads by their Content-Type. A failed
// probe keeps the address on the HTTP downloader.
func (s *Service) servesTorrent(ctx context.Context, raw string) bool {
	pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(pctx, "HEAD", raw, nil)
	if err != nil {
		return false
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200 && strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "application/x-bittorrent")
}

func (s *Service) btSource(ctx context.Context, kind SourceKind, raw string) (btSource, error) {
	src := btSource{kind: kind}
	var err error
	switch kind {
	case SourceMagnet:
		if !publicMagnetEndpoints(raw) {
			return src, refused("url_denied")
		}
		src.magnet = raw
		src.hash, err = magnetInfoHash(raw)
		return src, err
	case SourceTorrentFile:
		encoded := strings.TrimPrefix(raw, torrentDataPrefix)
		if base64.StdEncoding.DecodedLen(len(encoded)) > maxTorrentBytes+2 {
			return src, refused("torrent_too_large")
		}
		src.torrent, err = base64.StdEncoding.Strict().DecodeString(encoded)
		if err != nil {
			return src, refused("invalid_torrent")
		}
	case SourceTorrentURL:
		if !s.allowedURL(raw) {
			return src, refused("url_denied")
		}
		if src.torrent, err = s.fetchTorrent(ctx, raw); err != nil {
			return src, err
		}
	}
	if len(src.torrent) > maxTorrentBytes {
		return src, refused("torrent_too_large")
	}
	src.hash, err = torrentInfoHash(src.torrent)
	return src, err
}

func (s *Service) fetchTorrent(ctx context.Context, raw string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, refused("url_denied")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, refused("torrent_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, refused("torrent_unavailable")
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxTorrentBytes+1))
	if err != nil {
		return nil, refused("torrent_unavailable")
	}
	if len(b) > maxTorrentBytes {
		return nil, refused("torrent_too_large")
	}
	return b, nil
}

func (s *Service) torrentPath(hash string) string {
	return filepath.Join(s.Data, "torrents", hash+".torrent")
}

// btActive reports a submitted task already owning the hash's storage.
func btActive(st *State, hash string) bool {
	for _, j := range st.Jobs {
		if j.Hash == hash {
			return true
		}
	}
	for _, j := range st.Resolved {
		if j.Hash == hash && j.SubmissionRID != "" {
			return true
		}
	}
	return false
}

// admitBT checks the owner's destination and reserves the hash stage. The
// engine binds a torrent's storage to its first stage, so the stage is
// derived from the hash and only one submitted task may use it.
func (s *Service) admitBT(ctx context.Context, c api.Caller, r *http.Request, owner string, st *State, folder string, src btSource) (any, error) {
	return s.admitBTSource(ctx, sourceAccess{caller: c, request: r, owner: owner}, owner, st, folder, src)
}

func (s *Service) admitBTSource(ctx context.Context, a sourceAccess, owner string, st *State, folder string, src btSource) (any, error) {
	if err := a.destination(ctx, s, folder); err != nil {
		return nil, err
	}
	if len(st.Jobs)+len(st.Resolved) >= 128 {
		return nil, refused("queue_limit")
	}
	current, err := a.current(ctx, s)
	if err != nil {
		return nil, err
	}
	if current.Path != folder {
		return nil, refused("background_destination_required")
	}
	if btActive(st, src.hash) {
		return nil, refused("torrent_in_use")
	}
	st.Folders[owner], st.Grants[owner] = current.Path, current.ID
	if src.torrent != nil {
		if err = os.MkdirAll(filepath.Dir(s.torrentPath(src.hash)), 0700); err != nil {
			return nil, err
		}
		if err = os.WriteFile(s.torrentPath(src.hash), src.torrent, 0600); err != nil {
			return nil, err
		}
	}
	// No task holds this hash, so anything in its stage is a leftover. The
	// file storage would count full-size leftovers as complete pieces and
	// leave the neighbouring files as .part (B7).
	stage := filepath.Join(s.StagingDir, "bt-"+src.hash)
	if err = os.RemoveAll(stage); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(stage, 0700); err != nil {
		return nil, err
	}
	if _, err = s.ensureProtocolConfig(ctx); err != nil {
		return nil, err
	}
	return Job{Subfolder: sourceSubfolder(a, st, owner), Kind: string(src.kind), Hash: src.hash, Owner: owner, Stage: stage, Destination: folder, DelegationID: current.ID}, nil
}

type btFile struct {
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

type btResource struct {
	Name  string   `json:"name"`
	Hash  string   `json:"hash"`
	Files []btFile `json:"files"`
}

// btPlan validates the engine's chosen files as stage-relative sources;
// nil selected means every file.
func btPlan(res btResource, selected []int) ([]multisave.Item, error) {
	sources := make([]multisave.Source, 0, len(res.Files))
	pick := map[int]bool{}
	for _, i := range selected {
		pick[i] = true
	}
	for i, f := range res.Files {
		if selected != nil && !pick[i] {
			continue
		}
		p := f.Name
		if f.Path != "" {
			p = f.Path + "/" + f.Name
		}
		sources = append(sources, multisave.Source{Path: p, Size: f.Size})
	}
	title := res.Name
	if title == "" && len(res.Files) > 0 {
		title = res.Files[0].Name
	}
	items, err := multisave.Plan(title, res.Name, sources)
	if err != nil {
		return nil, refused("invalid_torrent_files")
	}
	return items, nil
}

func (s *Service) commitBT(owner string, st *State, job Job, rep Reply) (any, error) {
	var data struct {
		ID  string     `json:"id"`
		Res btResource `json:"res"`
	}
	if json.Unmarshal(rep.Data, &data) != nil || !engineTaskID.MatchString(data.ID) {
		return nil, refused("engine_unavailable")
	}
	if !strings.EqualFold(data.Res.Hash, job.Hash) {
		return nil, refused("torrent_mismatch")
	}
	items, err := btPlan(data.Res, nil)
	if err != nil {
		return nil, err
	}
	if btActive(st, job.Hash) {
		return nil, refused("torrent_in_use")
	}
	for rid, other := range st.Resolved {
		if other.Owner == owner && other.Hash == job.Hash && other.SubmissionRID == "" {
			delete(st.Resolved, rid)
		}
	}
	if len(st.Jobs)+len(st.Resolved) >= 128 {
		return nil, refused("queue_limit")
	}
	job.Filename = data.Res.Name
	if job.Filename == "" {
		job.Filename = items[0].Name
	}
	job.FileCount = len(data.Res.Files)
	st.Resolved[data.ID] = job
	return rep, nil
}

// trackedFiles counts the file records every job may persist.
func trackedFiles(st *State) int {
	n := 0
	for _, j := range st.Jobs {
		switch {
		case len(j.Files) > 0:
			n += len(j.Files)
		default:
			n += j.FileCount
		}
	}
	return n
}

// btSelection validates an owner's file choice; nil means every file.
func btSelection(list []any, count int) ([]int, error) {
	if len(list) == 0 || len(list) > count {
		return nil, refused("invalid_selection")
	}
	seen := map[int]bool{}
	out := make([]int, 0, len(list))
	for _, v := range list {
		f, ok := v.(float64)
		i := int(f)
		if !ok || float64(i) != f || i < 0 || i >= count || seen[i] {
			return nil, refused("invalid_selection")
		}
		seen[i] = true
		out = append(out, i)
	}
	if len(out) == count {
		return nil, nil
	}
	slices.Sort(out)
	return out, nil
}

// applySelection sends the owner's file choice; the engine stops fetching
// the other files. Every file is sent when there is no choice.
func (s *Service) applySelection(ctx context.Context, id string, job Job) bool {
	chosen := job.Select
	if chosen == nil {
		chosen = make([]int, job.FileCount)
		for i := range chosen {
			chosen[i] = i
		}
	}
	rep, err := s.engine(ctx, "PATCH", "/api/v1/tasks/"+id, nil, map[string]any{"opts": map[string]any{"selectFiles": chosen}})
	return err == nil && rep.Code == 0
}

// selectFiles changes which files of the owner's torrent are downloaded and
// saved. Once its files are planned for saving the choice is closed.
func (s *Service) selectFiles(ctx context.Context, owner string, raw json.RawMessage) (any, error) {
	var in struct {
		ID    string `json:"id"`
		Files []any  `json:"files"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	return s.withState(ctx, func(st *State) (any, error) {
		job, ok := st.Jobs[in.ID]
		if !ok || job.Owner != owner || job.Hash == "" {
			return nil, refused("task_denied")
		}
		if job.DeleteRequested || job.Published || len(job.Files) > 0 {
			return nil, refused("selection_closed")
		}
		chosen, err := btSelection(in.Files, job.FileCount)
		if err != nil {
			return nil, err
		}
		job.Select = chosen
		st.Jobs[in.ID] = job
		// A choice the engine misses is sent again by the save pass.
		s.applySelection(ctx, in.ID, job)
		return map[string]any{"files": len(in.Files)}, nil
	})
}
