package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"dsmapp/internal/githubtoken"
	"dsmapp/internal/media"
	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// MediaJob is the durable yt-dlp request. The source URL is retained because
// the daemon must run it later; it is shown only to the task owner.
type MediaJob struct {
	Tool     string        `json:"tool,omitempty"`
	RunID    string        `json:"runId,omitempty"`
	URL      string        `json:"url"`
	Title    string        `json:"title"`
	Format   string        `json:"format,omitempty"`
	Options  media.Options `json:"options,omitempty"`
	FollowID string        `json:"followId,omitempty"`
	// Playlist downloads up to multisave.MaxFiles entries.
	Playlist bool `json:"playlist,omitempty"`
	// Status is queued, running, paused, done, error or canceled.
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

func (s *Service) mediaHome() string             { return filepath.Join(s.Data, "media") }
func (s *Service) mediaControl(id string) string { return filepath.Join(s.mediaHome(), "jobs", id) }
func (s *Service) mediaTools() media.Tools {
	if s.MediaTools != nil {
		return s.MediaTools()
	}
	return media.Tools{Home: s.mediaHome(), GitHub: githubtoken.At(s.Data)}
}
func (s *Service) mediaEnv() media.Env {
	if s.MediaEnv != nil {
		return s.MediaEnv()
	}
	return s.mediaTools().Env()
}
func (s *Service) mediaExec() media.Exec {
	if s.MediaExec != nil {
		return s.MediaExec
	}
	return media.Run
}

func mediaRefused(err error) error {
	if code := media.Code(err); code != "" {
		return refused(code)
	}
	return err
}

func (s *Service) mediaRoutes() []api.Route {
	var routes []api.Route
	handler := func(method string) api.Handler {
		return func(ctx context.Context, c api.Caller, raw json.RawMessage) (any, error) {
			if err := api.RequireAdmin(c); err != nil {
				return nil, err
			}
			return s.mediaToolRequest(ctx, method, raw)
		}
	}
	return append(routes,
		api.Handle("media.tools.status", handler("media.tools.status")),
		api.Handle("media.tools.check", handler("media.tools.check")),
		api.Handle("media.tools.update", handler("media.tools.update")),
		api.Handle("media.tools.rollback", handler("media.tools.rollback")),
	)
}

// mediaToolRequest serves the administrator's tool page. Installs and updates
// are queued for the package daemon, which verifies and activates them.
func (s *Service) mediaToolRequest(ctx context.Context, method string, raw json.RawMessage) (any, error) {
	tools := s.mediaTools()
	if method == "media.tools.status" {
		if err := decodeStrict(raw, &struct{}{}); err != nil {
			return nil, err
		}
		arch := tools.Arch
		if arch == "" {
			arch = runtime.GOARCH
		}
		return map[string]any{"state": tools.State(), "pending": tools.Pending(), "arch": arch}, nil
	}
	var in struct {
		Tool    string `json:"tool"`
		Version string `json:"version"`
		SHA256  string `json:"sha256"`
		Consent bool   `json:"consent"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	if _, ok := media.Specs[in.Tool]; !ok {
		return nil, refused("invalid_request")
	}
	if !tools.Supported(in.Tool) {
		return nil, refused("tool_arch_unavailable")
	}
	if method == "media.tools.check" {
		ctx, cancel := context.WithTimeout(ctx, checkDeadline)
		defer cancel()
		check, err := tools.CheckUpdate(ctx, in.Tool)
		if err != nil {
			return nil, sourceRefused(err)
		}
		return check, nil
	}
	if !in.Consent {
		return nil, refused("consent_required")
	}
	req := media.ToolRequest{ID: randomID(), Tool: in.Tool, Op: "rollback"}
	cur := tools.State().Tools[in.Tool]
	if method == "media.tools.rollback" {
		if cur.Previous == nil {
			return nil, refused("rollback_unavailable")
		}
	} else {
		latest, err := tools.Release(ctx, in.Tool)
		if err != nil {
			return nil, sourceRefused(err)
		}
		if latest.Version != in.Version || latest.SHA256 != in.SHA256 {
			return nil, refused("release_changed")
		}
		// yt-dlp updates itself once installed; other tools are downloaded.
		req.Op, req.Release = "install", &latest
		if in.Tool == "yt-dlp" && cur.Active != nil {
			req.Op = "update"
		}
	}
	queued, err := tools.Enqueue(ctx, req)
	if errors.Is(err, media.ErrToolPending) {
		return nil, refused("tool_update_pending")
	}
	if err != nil {
		return nil, refused("tool_install_failed")
	}
	return map[string]string{"request": queued.ID}, nil
}

// probeMedia classifies one pasted address. yt-dlp runs at most once, as the
// MediaDetector of ClassifySource, and outside the jobs lock.
func (s *Service) probeMedia(ctx context.Context, c api.Caller, r *http.Request, owner string, raw json.RawMessage) (any, error) {
	return s.probeMediaSource(ctx, sourceAccess{caller: c, request: r, owner: owner}, owner, raw)
}

func (s *Service) probeMediaSource(ctx context.Context, a sourceAccess, owner string, raw json.RawMessage) (any, error) {
	var in struct {
		URL  string `json:"url"`
		Path string `json:"path"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	env := s.mediaEnv()
	var info media.Info
	var probeErr error
	kind, err := ClassifySource(in.URL, func(u *url.URL) bool {
		info, probeErr = s.inspectOwnerMedia(ctx, owner, in.URL)
		return probeErr != nil || info.Kind == "media"
	})
	if err != nil {
		return nil, err
	}
	if kind != SourceTorrentFile && !s.allowedURL(in.URL) {
		return nil, refused("url_denied")
	}
	if kind != SourceMedia {
		return map[string]any{"kind": kind, "route": "gopeed", "toolMissing": info.ToolMissing}, nil
	}
	if probeErr != nil {
		if media.Code(probeErr) == "" {
			return nil, probeErr
		}
		return nil, mediaRefused(probeErr)
	}
	if info.Playlist && (info.Entries < 1 || info.Entries > multisave.MaxFiles) {
		return nil, refused("media_playlist_limit")
	}
	if err = a.destination(ctx, s, in.Path); err != nil {
		return nil, err
	}
	current, err := a.current(ctx, s)
	if err != nil {
		return nil, err
	}
	if current.Path != in.Path {
		return nil, refused("background_destination_required")
	}
	options := media.Options{Format: "best"}
	if a.admission != nil && a.admission.Preparation != nil {
		prep := a.admission.Preparation
		if prep.MediaOptions != nil {
			options = *prep.MediaOptions
		}
		if rule := matchingRule(MediaPreferences{Rules: prep.MediaRules}, in.URL, info.ChannelURL); rule != nil && rule.Channel != "" {
			if !validSubfolder(rule.Path) {
				return nil, refused("invalid_subfolder")
			}
			copy := *a.admission
			copy.Subfolder = rule.Path
			a.admission = &copy
			if !prep.MediaOverride {
				options = rule.Options
			}
		}
	}
	rid := randomID()
	stage := filepath.Join(s.StagingDir, randomID())
	_, err = a.resolveLocked(ctx, s, func(_ string, st *State) (any, error) {
		for id, j := range st.Resolved {
			if j.Owner == owner && j.Kind == string(SourceMedia) && j.SubmissionRID == "" {
				delete(st.Resolved, id)
			}
		}
		if len(st.Jobs)+len(st.Resolved) >= 128 {
			return nil, refused("queue_limit")
		}
		st.Folders[owner], st.Grants[owner] = current.Path, current.ID
		st.Resolved[rid] = Job{Subfolder: sourceSubfolder(a, st, owner), Kind: string(SourceMedia), Owner: owner, Stage: stage, Destination: in.Path, DelegationID: current.ID, Media: &MediaJob{Tool: info.Tool, URL: in.URL, Title: info.Title, Playlist: info.Playlist, Options: options}}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"kind": kind, "route": mediaRoute(info), "rid": rid, "info": info, "merge": env.FFmpegDir != ""}, nil
}

// createMedia turns the owner's probe into a queued job. It is idempotent per
// RID and never contacts the engine, so it commits atomically in jobs.json.
func (s *Service) createMedia(ctx context.Context, c api.Caller, r *http.Request, owner string, raw json.RawMessage) (any, error) {
	return s.createMediaSource(ctx, sourceAccess{caller: c, request: r, owner: owner}, owner, raw)
}

func (s *Service) createMediaSource(ctx context.Context, a sourceAccess, owner string, raw json.RawMessage) (any, error) {
	var in struct {
		RID     string         `json:"rid"`
		Format  string         `json:"format"`
		Options *media.Options `json:"options,omitempty"`
	}
	if err := decodeStrict(raw, &in); err != nil {
		return nil, err
	}
	if in.Format == "" {
		in.Format = "best"
	}
	if !media.ValidFormat(in.Format) || !engineTaskID.MatchString(in.RID) {
		return nil, refused("invalid_request")
	}
	id := "media-" + in.RID
	current, err := a.current(ctx, s)
	if err != nil {
		return nil, err
	}
	return a.locked(ctx, s, func(_ string, st *State) (any, error) {
		if job, ok := st.Jobs[id]; ok {
			if job.Owner != owner {
				return nil, refused("task_denied")
			}
			return map[string]string{"id": id}, nil
		}
		job, ok := st.Resolved[in.RID]
		if !ok || job.Owner != owner || job.Kind != string(SourceMedia) || job.Media == nil {
			return nil, refused("task_denied")
		}
		if current.ID != job.DelegationID || current.Path != job.Destination {
			return nil, refused("background_destination_required")
		}
		if err := os.MkdirAll(job.Stage, 0700); err != nil {
			return nil, err
		}
		m := *job.Media
		options := preferences(st, owner).Defaults
		options.Format = in.Format
		if in.Options != nil {
			options = *in.Options
		}
		options, err := options.Normalize()
		if err != nil {
			return nil, mediaRefused(err)
		}
		if m.Tool != "gallery-dl" && options.NeedsFFmpeg() && s.mediaEnv().FFmpegDir == "" {
			return nil, refused("media_ffmpeg_required")
		}
		m.Format, m.Options, m.Status = options.Format, options, "queued"
		if a.admission != nil && a.admission.Preparation != nil {
			m.FollowID = a.admission.Preparation.FollowID
		}
		job.Media = &m
		st.Jobs[id] = job
		delete(st.Resolved, in.RID)
		return map[string]string{"id": id}, nil
	})
}

type mediaProgress struct {
	Downloaded int64 `json:"downloaded"`
	Total      int64 `json:"total"`
	Speed      int64 `json:"speed"`
}

func (s *Service) readProgress(id string) mediaProgress {
	var p mediaProgress
	if b, err := os.ReadFile(filepath.Join(s.mediaControl(id), "progress.json")); err == nil && len(b) < 4096 {
		json.Unmarshal(b, &p)
	}
	return p
}

var mediaStatus = map[string]string{"queued": "wait", "running": "running", "paused": "pause", "done": "done", "error": "error", "canceled": "pause"}

// presentMedia shapes a media job like an engine task for the existing list.
func (s *Service) presentMedia(id string, job Job) map[string]any {
	p := s.readProgress(id)
	name := job.Media.Title
	if name == "" {
		name = id
	}
	files := []map[string]any{{"name": name, "size": p.Total}}
	if job.Media.Status == "done" && len(job.Files) > 0 {
		files = files[:0]
		for _, f := range job.Files {
			files = append(files, map[string]any{"name": f.Name, "size": f.Size})
		}
	}
	task := map[string]any{"id": id, "protocol": "media", "status": mediaStatus[job.Media.Status],
		"meta":     map[string]any{"opts": map[string]any{}, "req": map[string]any{"url": job.Media.URL}, "res": map[string]any{"name": name, "size": p.Total, "files": files}},
		"progress": map[string]any{"downloaded": p.Downloaded, "speed": p.Speed},
		"media":    map[string]any{"title": job.Media.Title, "format": job.Media.Format}}
	s.present(task, job)
	if job.Media.Status == "error" {
		pub := task["publication"].(map[string]any)
		pub["error"], pub["reason"] = job.Media.Error, ""
	}
	return task
}

// mediaRequest serves the owner's task routes for a media job.
func (s *Service) mediaRequest(st *State, id string, job Job, in Input, op string) (any, error) {
	switch {
	case in.Method == "GET" && op == "":
		data, _ := json.Marshal(s.presentMedia(id, job))
		return Reply{Data: data}, nil
	case in.Method == "PUT" && op == "/pause":
		if job.Media.Status == "queued" || job.Media.Status == "running" {
			job.Media.Status = "paused"
		}
	case in.Method == "PUT" && op == "/continue":
		if job.Media.Status == "paused" || job.Media.Status == "error" || job.Media.Status == "canceled" {
			job.Media.Status, job.Media.Error = "queued", ""
		}
	default:
		return nil, refused("endpoint_denied")
	}
	m := *job.Media
	job.Media = &m
	job.ManualPaused = m.Status == "paused"
	st.Jobs[id] = job
	return Reply{Data: json.RawMessage(`true`)}, nil
}

// stagedFiles lists completed files; yt-dlp partials and hidden files are not
// results. Symlinks or other special files refuse the whole result.
func stagedFiles(stage string) ([]multisave.Source, error) {
	var out []multisave.Source
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == stage {
			return nil
		}
		rel, _ := filepath.Rel(stage, p)
		if strings.HasPrefix(d.Name(), ".") || strings.HasSuffix(d.Name(), ".part") || strings.HasSuffix(d.Name(), ".ytdl") || strings.HasSuffix(d.Name(), ".info.json") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("unsafe staged file")
		}
		out = append(out, multisave.Source{Path: filepath.ToSlash(rel), Size: info.Size()})
		return nil
	})
	return out, err
}

func (s *Service) updateMediaJob(ctx context.Context, id string, expected Job, fn func(*Job)) error {
	_, err := s.withState(ctx, func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if !ok || cur.Owner != expected.Owner || cur.DelegationID != expected.DelegationID || cur.Media == nil {
			return nil, refused("task_denied")
		}
		if cur.DeleteRequested {
			return nil, refused("task_deleting")
		}
		m := *cur.Media
		cur.Media = &m
		fn(&cur)
		st.Jobs[id] = cur
		return nil, nil
	})
	return err
}

// saveMedia publishes a finished media job's files with multisave. The plan
// is persisted before the first SDK admission.
func (s *Service) saveMedia(ctx context.Context, id string, job Job) error {
	unlock, err := s.lockSave(ctx, id)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := s.readState()
	if err != nil {
		return err
	}
	current, ok := st.Jobs[id]
	if !ok || current.Owner != job.Owner {
		return refused("task_denied")
	}
	return s.saveMediaLocked(ctx, id, current)
}
func (s *Service) saveMediaLocked(ctx context.Context, id string, job Job) error {
	if job.Media == nil || job.Media.Status != "done" || job.Published || job.ManualPaused || job.SavePausePending {
		return nil
	}
	if s.Delegate == nil {
		return refused("save_unavailable")
	}
	rel, err := filepath.Rel(s.StagingDir, job.Stage)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return refused("unsafe_file")
	}
	if len(job.Files) == 0 {
		sources, err := stagedFiles(job.Stage)
		var items []multisave.Item
		if err == nil {
			items, err = multisave.Plan(job.Media.Title, "", sources)
			if err == nil {
				err = pairMediaSidecars(items)
			}
		}
		if err != nil {
			return s.updateMediaJob(ctx, id, job, func(j *Job) { j.Media.Status, j.Media.Error = "error", "media_failed" })
		}
		job.Files = items
		job.Filename = job.Media.Title
		if len(items) == 1 {
			job.Filename = items[0].Name
		}
		if err = s.updateMediaJob(ctx, id, job, func(j *Job) {
			j.Files, j.Filename, j.SaveStarted, j.SaveState = items, job.Filename, true, "pending"
		}); err != nil {
			return err
		}
	}
	b := multisave.Batch{Task: id, DelegationID: job.DelegationID, Destination: job.destinationPath(), Subfolder: job.Subfolder, Stage: filepath.ToSlash(rel)}
	items := multisave.Advance(ctx, s.Delegate, b, job.Files)
	sum := multisave.Summarize(items)
	return s.updateMediaJob(context.WithoutCancel(ctx), id, job, func(j *Job) {
		j.Files, j.Published, j.SaveState, j.SaveError, j.SaveReason = items, sum.Published, sum.State, sum.Code, sum.Reason
		j.Uncertain = sum.State == "uncertain"
	})
}

// advanceMediaDeletion cancels admitted saves, waits for the runner to stop
// yt-dlp, then forgets the job and removes its package-private files.
func (s *Service) advanceMediaDeletion(ctx context.Context, id string, job Job) error {
	kept := false
	if len(job.Files) > 0 && s.Delegate != nil {
		rel, _ := filepath.Rel(s.StagingDir, job.Stage)
		batch := multisave.Batch{Task: id, DelegationID: job.DelegationID, Destination: job.destinationPath(), Subfolder: job.Subfolder, Stage: filepath.ToSlash(rel)}
		items, done, k, err := multisave.Cancel(ctx, s.Delegate, batch, job.Files)
		kept = k
		if _, e := s.withState(context.WithoutCancel(ctx), func(st *State) (any, error) {
			if cur, ok := st.Jobs[id]; ok && cur.Owner == job.Owner {
				cur.Files = items
				st.Jobs[id] = cur
			}
			return nil, nil
		}); e != nil {
			return e
		}
		if err != nil || !done || s.discardFiles(ctx, batch, items) != "" {
			return err
		}
	}
	if job.Media != nil && job.Media.Status == "running" {
		return nil
	}
	removed := false
	_, err := s.withState(ctx, func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if !ok || cur.Owner != job.Owner || !cur.DeleteRequested || cur.Media == nil || cur.Media.Status == "running" {
			return nil, nil
		}
		st.Removals = append(st.Removals, Removal{ID: id, Owner: cur.Owner, Filename: cur.Filename, FileKept: kept || cur.Published})
		if len(st.Removals) > 128 {
			st.Removals = st.Removals[len(st.Removals)-128:]
		}
		delete(st.Jobs, id)
		removed = true
		return nil, nil
	})
	if err == nil && removed {
		os.RemoveAll(job.Stage)
		os.RemoveAll(s.mediaControl(id))
	}
	return err
}

func mediaRoute(info media.Info) string {
	if info.Tool == "gallery-dl" {
		return info.Tool
	}
	return "yt-dlp"
}
