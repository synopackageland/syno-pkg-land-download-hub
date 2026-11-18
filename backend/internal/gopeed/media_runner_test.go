package gopeed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"dsmapp/internal/media"
)

type fakeProc struct {
	once    sync.Once
	stopped chan struct{}
	exit    chan error
	stderr  string
}

func (p *fakeProc) Stop() { p.once.Do(func() { close(p.stopped) }) }
func (p *fakeProc) Wait() (string, error) {
	select {
	case err := <-p.exit:
		return p.stderr, err
	case <-p.stopped:
		return "", errors.New("signal: terminated")
	}
}

type fakeStarts struct {
	mu    sync.Mutex
	args  [][]string
	env   [][]string
	procs []*fakeProc
}

func runnerFixture(t *testing.T) (*Service, *MediaRunner, *fakeStarts) {
	s, rid := probedMedia(t)
	createMedia(t, s, "alice", rid, "720")
	f := &fakeStarts{}
	r := &MediaRunner{Service: s, Start: func(name string, args, env []string, onLine func(string)) (MediaProc, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if name != "/tools/yt-dlp" {
			t.Fatalf("unmanaged tool %s", name)
		}
		onLine(media.ProgressPrefix + " 40 100 NA 7")
		p := &fakeProc{stopped: make(chan struct{}), exit: make(chan error, 1)}
		f.args, f.env, f.procs = append(f.args, args), append(f.env, env), append(f.procs, p)
		return p, nil
	}}
	t.Cleanup(r.Close)
	return s, r, f
}

func mediaStatusOf(t *testing.T, s *Service, id string) *MediaJob {
	st, err := s.readState()
	if err != nil {
		t.Fatal(err)
	}
	return st.Jobs[id].Media
}

func onlyMediaID(t *testing.T, s *Service) string {
	st, _ := s.readState()
	for id, j := range st.Jobs {
		if j.Media != nil {
			return id
		}
	}
	t.Fatal("no media job")
	return ""
}

func TestRunnerDownloadsQueuedJobIntoItsStage(t *testing.T) {
	s, r, f := runnerFixture(t)
	id := onlyMediaID(t, s)
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m := mediaStatusOf(t, s, id); m.Status != "running" || len(f.procs) != 1 {
		t.Fatal(m, len(f.procs))
	}
	st, _ := s.readState()
	job := st.Jobs[id]
	args := strings.Join(f.args[0], " ")
	if !strings.Contains(args, "-P "+job.Stage) || !strings.Contains(args, "-P temp:"+s.mediaControl(id)+"/tmp") || !strings.Contains(args, "[height<=720]") || !strings.HasSuffix(args, "-- "+job.Media.URL) {
		t.Fatal(args)
	}
	if !slices.Contains(f.env[0], "HOME="+filepath.Join(s.mediaHome(), "home")) {
		t.Fatal(f.env[0])
	}
	for _, dir := range []string{job.Stage, s.mediaControl(id) + "/tmp", s.mediaHome() + "/cache", s.mediaHome() + "/tmp"} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatal(dir, err)
		}
	}
	if p := s.readProgress(id); p.Downloaded != 40 || p.Total != 100 || p.Speed != 7 {
		t.Fatal(p)
	}
	r.Tick(context.Background())
	if len(f.procs) != 1 {
		t.Fatal("started twice")
	}
	f.procs[0].exit <- nil
	r.wait()
	if m := mediaStatusOf(t, s, id); m.Status != "done" {
		t.Fatal(m)
	}
}

func TestRunnerFailureKeepsClassifiedReason(t *testing.T) {
	s, r, f := runnerFixture(t)
	id := onlyMediaID(t, s)
	r.Tick(context.Background())
	f.procs[0].stderr = "ERROR: [youtube] x: Sign in to confirm your age"
	f.procs[0].exit <- errors.New("exit status 1")
	r.wait()
	if m := mediaStatusOf(t, s, id); m.Status != "error" || m.Error != "media_login_required" {
		t.Fatal(m)
	}
}

func TestRunnerStopsPausedAndDeletedJobs(t *testing.T) {
	s, r, f := runnerFixture(t)
	id := onlyMediaID(t, s)
	r.Tick(context.Background())
	call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/pause"})
	r.Tick(context.Background())
	r.wait()
	if m := mediaStatusOf(t, s, id); m.Status != "paused" {
		t.Fatal(m)
	}
	call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/continue"})
	r.Tick(context.Background())
	if len(f.procs) != 2 || mediaStatusOf(t, s, id).Status != "running" {
		t.Fatal("not resumed")
	}
	call(t, s, "alice", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/" + id})
	r.Tick(context.Background())
	r.wait()
	if m := mediaStatusOf(t, s, id); m.Status != "canceled" {
		t.Fatal(m)
	}
	s.SaveCompleted(context.Background())
	if st, _ := s.readState(); st.Jobs[id].Media != nil {
		t.Fatal("deleted job kept")
	}
}

func TestRunnerRequeuesInterruptedJobsAndHonoursConcurrency(t *testing.T) {
	s, r, f := runnerFixture(t)
	for _, name := range []string{"bob", "alice"} {
		out := probe(t, s, name, "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
		createMedia(t, s, name, out["data"].(map[string]any)["rid"].(string), "best")
	}
	r.Tick(context.Background())
	if len(f.procs) != MaxMediaRuns {
		t.Fatal(len(f.procs))
	}
	r.Close()
	restarted := &MediaRunner{Service: s, Start: r.Start}
	t.Cleanup(restarted.Close)
	restarted.Tick(context.Background())
	st, _ := s.readState()
	running := 0
	for _, j := range st.Jobs {
		if j.Media.Status == "running" {
			running++
		}
	}
	if running != MaxMediaRuns || len(f.procs) != 2*MaxMediaRuns {
		t.Fatal(running, len(f.procs))
	}
}

func TestRunnerReportsMissingTool(t *testing.T) {
	s, r, f := runnerFixture(t)
	id := onlyMediaID(t, s)
	s.MediaEnv = func() media.Env { return media.Env{Home: s.mediaHome()} }
	r.Tick(context.Background())
	if m := mediaStatusOf(t, s, id); m.Status != "error" || m.Error != "media_tool_missing" || len(f.procs) != 0 {
		t.Fatal(m)
	}
}
