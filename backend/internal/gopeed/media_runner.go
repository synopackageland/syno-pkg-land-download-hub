package gopeed

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"dsmapp/internal/media"
	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// MaxMediaRuns bounds concurrent yt-dlp downloads in the daemon.
const MaxMediaRuns = 2

// MediaRunLimit is the wall-clock cap of one download attempt.
const MediaRunLimit = 12 * time.Hour

// MediaProc is one running download.
type MediaProc interface {
	Stop()
	Wait() (stderr string, err error)
}

// MediaRunner starts queued media jobs in the package daemon. jobs.json is the
// only shared state; a restart requeues jobs that were running, and yt-dlp's
// --continue and download archive skip what is already complete.
type MediaRunner struct {
	Service *Service
	Start   func(name string, args, env []string, onLine func(string)) (MediaProc, error)

	mu        sync.Mutex
	running   map[string]*mediaRun
	closing   bool
	wg        sync.WaitGroup
	lastOwner string
}

type mediaRun struct {
	proc    MediaProc
	timeout bool
}

// StartMedia adapts media.Start to MediaRunner.Start.
func StartMedia(name string, args, env []string, onLine func(string)) (MediaProc, error) {
	return media.Start(name, args, env, onLine)
}

// Tick stops runs whose job no longer wants them and starts queued jobs.
func (r *MediaRunner) Tick(ctx context.Context) error {
	s := r.Service
	r.mu.Lock()
	if r.closing {
		r.mu.Unlock()
		return nil
	}
	if r.running == nil {
		r.running = map[string]*mediaRun{}
	}
	active := map[string]bool{}
	for id := range r.running {
		active[id] = true
	}
	r.mu.Unlock()
	env := s.mediaEnv()
	var start []string
	stop := map[string]bool{}
	_, err := s.withState(ctx, func(st *State) (any, error) {
		owners := map[string]int{}
		for id := range active {
			j, ok := st.Jobs[id]
			if ok {
				owners[j.Owner]++
			}
			if !ok || j.Media == nil || j.Media.Status != "running" || j.DeleteRequested {
				stop[id] = true
			}
		}
		slots := MaxMediaRuns - len(active)
		candidates := map[string]Job{}
		for id, j := range st.Jobs {
			if j.Media == nil || active[id] {
				continue
			}
			if j.DeleteRequested && j.Media.Status == "running" {
				m := *j.Media
				m.Status = "canceled"
				j.Media = &m
				st.Jobs[id] = j
			}
			if !j.DeleteRequested && (j.Media.Status == "queued" || j.Media.Status == "running") {
				candidates[id] = j
			}
		}
		for _, id := range fairJobIDs(candidates, owners, r.lastOwner) {
			j := st.Jobs[id]
			if j.Media == nil || active[id] {
				continue
			}
			m := *j.Media
			if m.Status == "running" {
				m.Status = "queued"
			}
			if m.Status == "queued" && !j.DeleteRequested && slots > 0 {
				if env.YtDlp == "" && j.Media.Tool != "gallery-dl" || j.Media.Tool == "gallery-dl" && env.GalleryDlp == "" {
					m.Status, m.Error = "error", "media_tool_missing"
				} else {
					m.Status = "running"
					m.RunID = randomID()
					start = append(start, id)
					r.lastOwner = j.Owner
					slots--
				}
			}
			j.Media = &m
			st.Jobs[id] = j
		}
		return nil, nil
	})
	r.mu.Lock()
	for id := range stop {
		if run := r.running[id]; run != nil {
			run.proc.Stop()
		}
	}
	r.mu.Unlock()
	if err != nil {
		return err
	}
	st, err := s.readState()
	if err != nil {
		return err
	}
	for _, id := range start {
		if e := r.launch(ctx, env, id, st.Jobs[id]); e != nil {
			r.finish(id, st.Jobs[id], "", e, false)
		}
	}
	return nil
}

func (r *MediaRunner) launch(ctx context.Context, env media.Env, id string, job Job) error {
	s := r.Service
	control := s.mediaControl(id)
	if err := media.PrepareHome(env.Home); err != nil {
		return err
	}
	for _, dir := range []string{job.Stage, filepath.Join(control, "tmp")} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	if job.Media.Tool != "gallery-dl" && job.Media.Options.NeedsFFmpeg() && env.FFmpegDir == "" {
		return &media.Error{Code: "media_ffmpeg_required"}
	}
	// Crash leftovers belong only to this job and are never reused.
	leftovers, _ := filepath.Glob(filepath.Join(control, ".cookies-*"))
	for _, p := range leftovers {
		os.Remove(p)
	}
	cookies, cleanup, err := s.cookies().Copy(job.Owner, job.Media.URL, control)
	if err != nil {
		return err
	}
	archive := ""
	if job.Media.FollowID != "" && engineTaskID.MatchString(job.Media.FollowID) {
		dir := filepath.Join(s.mediaHome(), "follows", job.Media.FollowID)
		if err = os.MkdirAll(dir, 0700); err != nil {
			cleanup()
			return err
		}
		archive = filepath.Join(dir, "archive.txt")
	}
	args := media.DownloadArgs(env, media.Download{URL: job.Media.URL, Format: job.Media.Format, Options: job.Media.Options, Cookies: cookies, Archive: archive, Output: job.Stage, Control: control, MaxBytes: delegate.MaxMoveBytes, Playlist: job.Media.Playlist})
	progress := &progressWriter{path: filepath.Join(control, "progress.json")}
	exe := env.YtDlp
	if job.Media.Tool == "gallery-dl" {
		exe = env.GalleryDlp
		args = media.GalleryArgs(env, media.Download{URL: job.Media.URL, Output: job.Stage, Control: control, Cookies: cookies, MaxBytes: delegate.MaxMoveBytes}, false)
	}
	proc, err := r.Start(exe, args, media.Environ(env.Home), progress.line)
	if err != nil {
		cleanup()
		return err
	}
	run := &mediaRun{proc: proc}
	r.mu.Lock()
	r.running[id] = run
	r.wg.Add(1)
	r.mu.Unlock()
	limit := time.AfterFunc(MediaRunLimit, func() {
		r.mu.Lock()
		run.timeout = true
		r.mu.Unlock()
		proc.Stop()
	})
	// The tool's per-extractor range can reset for child galleries. Independently
	// bound the private output too, including files without a Content-Length.
	monitorDone := make(chan struct{})
	monitorExited := make(chan struct{})
	var galleryLimit bool
	go func() {
		defer close(monitorExited)
		if job.Media.Tool != "gallery-dl" {
			return
		}
		tick := time.NewTicker(time.Second)
		defer tick.Stop()
		for {
			select {
			case <-monitorDone:
				return
			case <-tick.C:
				if !galleryWithinLimits(job.Stage) {
					galleryLimit = true
					proc.Stop()
					return
				}
			}
		}
	}()
	go func() {
		defer r.wg.Done()
		stderr, err := proc.Wait()
		close(monitorDone)
		<-monitorExited
		if job.Media.Tool == "gallery-dl" && (galleryLimit || !galleryWithinLimits(job.Stage)) {
			err = &media.Error{Code: "media_too_large"}
		}
		cleanup()
		if err == nil && job.Media.Tool != "gallery-dl" && job.Media.Options.NFO && job.Media.Format != "audio" {
			err = media.WriteNFO(job.Stage)
		}
		limit.Stop()
		progress.flush()
		r.mu.Lock()
		delete(r.running, id)
		closing, timeout := r.closing, run.timeout
		r.mu.Unlock()
		if !closing {
			r.finish(id, job, stderr, err, timeout)
		}
	}()
	return nil
}

func galleryWithinLimits(stage string) bool {
	count := 0
	err := filepath.WalkDir(stage, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if p == stage {
			return nil
		}
		if d.IsDir() {
			return fs.ErrInvalid
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() || info.Size() > delegate.MaxMoveBytes {
			return fs.ErrInvalid
		}
		count++
		if count > multisave.MaxFiles {
			return fs.ErrInvalid
		}
		return nil
	})
	return err == nil
}

// finish records the outcome unless the owner already paused, resumed or
// deleted the job while it was stopping.
func (r *MediaRunner) finish(id string, job Job, stderr string, err error, timeout bool) {
	ctx, cancel := context.WithTimeout(context.Background(), r.Service.saveBudget())
	defer cancel()
	r.Service.withState(ctx, func(st *State) (any, error) {
		cur, ok := st.Jobs[id]
		if !ok || cur.Owner != job.Owner || cur.Media == nil || cur.Media.Status != "running" || cur.Media.RunID != job.Media.RunID {
			return nil, nil
		}
		m := *cur.Media
		switch {
		case cur.DeleteRequested:
			m.Status = "canceled"
		case err == nil:
			m.Status, m.Error = "done", ""
		case timeout:
			m.Status, m.Error = "error", "media_timeout"
		default:
			m.Status, m.Error = "error", media.Classify(stderr)
			if code := media.Code(err); code != "" {
				m.Error = code
			}
			if m.Error == "" {
				m.Error = "media_failed"
			}
		}
		cur.Media = &m
		st.Jobs[id] = cur
		return nil, nil
	})
}

// Close stops every run without recording an outcome, so the next daemon
// start requeues them.
func (r *MediaRunner) Close() {
	r.mu.Lock()
	r.closing = true
	for _, run := range r.running {
		run.proc.Stop()
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *MediaRunner) wait() { r.wg.Wait() }

// progressWriter keeps the newest yt-dlp progress and writes it at most once
// per second.
type progressWriter struct {
	path    string
	mu      sync.Mutex
	last    time.Time
	current mediaProgress
	dirty   bool
}

func (p *progressWriter) line(l string) {
	d, t, sp, ok := media.ParseProgress(l)
	if !ok {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.current, p.dirty = mediaProgress{Downloaded: d, Total: t, Speed: sp}, true
	if time.Since(p.last) >= time.Second {
		p.writeLocked()
	}
}

func (p *progressWriter) flush() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dirty {
		p.current.Speed = 0
		p.writeLocked()
	}
}

func (p *progressWriter) writeLocked() {
	b, _ := json.Marshal(p.current)
	tmp := p.path + ".tmp"
	if os.WriteFile(tmp, b, 0600) == nil && os.Rename(tmp, p.path) == nil {
		p.last, p.dirty = time.Now(), false
	}
}

// sortedIDs gives queued jobs a stable start order.
func sortedIDs(jobs map[string]Job) []string {
	ids := make([]string, 0, len(jobs))
	for id := range jobs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
