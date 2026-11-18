package gopeed

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsmapp/internal/media"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

const bunny = `{"_type":"video","extractor_key":"Youtube","webpage_url_domain":"youtube.com","title":"Big Buck Bunny","duration":635,"filesize_approx":1000,"formats":[{"height":720,"vcodec":"avc1"},{"vcodec":"none","acodec":"opus"}]}`

func mediaFixture(t *testing.T, stdout, stderr string) (*Service, *int) {
	t.Helper()
	s, _, _ := fixture(t)
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	s.DestinationDelegate = ownDestination{}
	runs := new(int)
	s.MediaEnv = func() media.Env { return media.Env{YtDlp: "/tools/yt-dlp", Home: filepath.Join(s.Data, "media")} }
	s.MediaExec = func(_ context.Context, name string, args, _ []string, _ int64) ([]byte, []byte, error) {
		*runs++
		if name != "/tools/yt-dlp" {
			t.Fatalf("unmanaged tool %s", name)
		}
		if stderr != "" {
			return nil, []byte(stderr), os.ErrInvalid
		}
		return []byte(stdout), nil, nil
	}
	return s, runs
}

func probe(t *testing.T, s *Service, user, url string) map[string]any {
	t.Helper()
	out, _ := call(t, s, user, "test.media.probe", map[string]string{"url": url, "path": "/home"})
	return out
}

func TestMediaProbeUsesSourceClassification(t *testing.T) {
	s, runs := mediaFixture(t, bunny, "")
	out := probe(t, s, "alice", "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567")
	if out["success"] != true || out["data"].(map[string]any)["kind"] != "magnet" || *runs != 0 {
		t.Fatalf("magnet must not run yt-dlp: %v %d", out, *runs)
	}
	out = probe(t, s, "alice", "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	data := out["data"].(map[string]any)
	info := data["info"].(map[string]any)
	if data["kind"] != "media" || info["title"] != "Big Buck Bunny" || info["duration"] != 635.0 || data["rid"] == "" || *runs != 1 {
		t.Fatalf("%v", out)
	}
	for _, dir := range []string{"home", "cache", "tmp"} {
		if fi, err := os.Stat(filepath.Join(s.mediaHome(), dir)); err != nil || !fi.IsDir() {
			t.Fatal("yt-dlp ran without its private directory", dir, err)
		}
	}
	st, _ := s.readState()
	job := st.Resolved[data["rid"].(string)]
	if job.Owner != "uid:alice" || job.Kind != "media" || job.Media == nil || job.Media.URL != "https://www.youtube.com/watch?v=aqz-KE-bpKQ" || job.DelegationID != "grant-alice" || !strings.HasPrefix(job.Stage, s.StagingDir+"/") {
		t.Fatalf("%+v", job)
	}
	// A new probe replaces the owner's previous unsubmitted media probe.
	probe(t, s, "alice", "https://www.youtube.com/watch?v=other")
	st, _ = s.readState()
	if len(st.Resolved) != 1 {
		t.Fatalf("stale probes kept: %d", len(st.Resolved))
	}
}

func TestMediaProbeFallsBackToTheEngineAndReportsReasons(t *testing.T) {
	s, _ := mediaFixture(t, "", "ERROR: No suitable extractor found for URL https://example.com/a.zip")
	if out := probe(t, s, "alice", "https://example.com/a.zip"); out["data"].(map[string]any)["kind"] != "engine" {
		t.Fatal(out)
	}
	s, _ = mediaFixture(t, "", "ERROR: [twitter] 1: NSFW tweet requires authentication")
	if out := probe(t, s, "alice", "https://x.com/a/status/1"); out["error"].(map[string]any)["code"] != "media_login_required" {
		t.Fatal(out)
	}
	s, _ = mediaFixture(t, bunny, "")
	s.MediaEnv = func() media.Env { return media.Env{Home: s.Data} }
	out := probe(t, s, "alice", "https://www.youtube.com/watch?v=x")
	if d := out["data"].(map[string]any); d["kind"] != "engine" || d["toolMissing"] != true {
		t.Fatal(out)
	}
	for _, url := range []string{"http://127.0.0.1/v", "file:///etc/passwd", "https://user:pw@example.com/v"} {
		if out := probe(t, s, "alice", url); out["success"] == true {
			t.Fatalf("%s accepted", url)
		}
	}
}

func createMedia(t *testing.T, s *Service, user, rid, format string) map[string]any {
	t.Helper()
	out, _ := call(t, s, user, "test.media.create", map[string]string{"rid": rid, "format": format})
	return out
}

func probedMedia(t *testing.T) (*Service, string) {
	s, _ := mediaFixture(t, bunny, "")
	out := probe(t, s, "alice", "https://www.youtube.com/watch?v=aqz-KE-bpKQ")
	return s, out["data"].(map[string]any)["rid"].(string)
}

func TestMediaCreateIsOwnerBoundAndIdempotent(t *testing.T) {
	s, rid := probedMedia(t)
	if out := createMedia(t, s, "bob", rid, "best"); out["success"] == true {
		t.Fatal("other user created alice's probe")
	}
	if out := createMedia(t, s, "alice", rid, "bv*+ba"); out["success"] == true {
		t.Fatal("raw format accepted")
	}
	first := createMedia(t, s, "alice", rid, "720")
	again := createMedia(t, s, "alice", rid, "720")
	id, _ := first["data"].(map[string]any)["id"].(string)
	if id != "media-"+rid || again["data"].(map[string]any)["id"] != id {
		t.Fatal(first, again)
	}
	st, _ := s.readState()
	job := st.Jobs[id]
	if job.Media.Status != "queued" || job.Media.Format != "720" || len(st.Resolved) != 0 || job.Owner != "uid:alice" {
		t.Fatalf("%+v", job)
	}
}

func listTasks(t *testing.T, s *Service, user string) []any {
	out, _ := call(t, s, user, "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	return out["data"].(map[string]any)["data"].([]any)
}

func mediaTask(t *testing.T, s *Service, user, id string) map[string]any {
	for _, x := range listTasks(t, s, user) {
		if task := x.(map[string]any); task["id"] == id {
			return task
		}
	}
	return nil
}

func TestMediaTasksAppearOnlyInOwnersListAndPause(t *testing.T) {
	s, rid := probedMedia(t)
	id := createMedia(t, s, "alice", rid, "best")["data"].(map[string]any)["id"].(string)
	task := mediaTask(t, s, "alice", id)
	if task == nil || task["status"] != "wait" || task["protocol"] != "media" || task["meta"].(map[string]any)["res"].(map[string]any)["name"] != "Big Buck Bunny" {
		t.Fatalf("%v", task)
	}
	if mediaTask(t, s, "bob", id) != nil {
		t.Fatal("foreign task listed")
	}
	if out, _ := call(t, s, "bob", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/pause"}); out["success"] == true {
		t.Fatal("foreign pause")
	}
	call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/pause"})
	if task = mediaTask(t, s, "alice", id); task["status"] != "pause" {
		t.Fatal(task)
	}
	call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/continue"})
	if task = mediaTask(t, s, "alice", id); task["status"] != "wait" {
		t.Fatal(task)
	}
	os.MkdirAll(s.mediaControl(id), 0700)
	os.WriteFile(filepath.Join(s.mediaControl(id), "progress.json"), []byte(`{"downloaded":40,"total":100,"speed":7}`), 0600)
	task = mediaTask(t, s, "alice", id)
	if p := task["progress"].(map[string]any); p["downloaded"] != 40.0 || p["speed"] != 7.0 {
		t.Fatal(task)
	}
}

func finishMedia(t *testing.T, s *Service, id string, status, code string, files map[string]string) Job {
	var job Job
	s.withState(context.Background(), func(st *State) (any, error) {
		job = st.Jobs[id]
		job.Media.Status, job.Media.Error = status, code
		st.Jobs[id] = job
		return nil, nil
	})
	os.MkdirAll(job.Stage, 0700)
	for name, body := range files {
		os.WriteFile(filepath.Join(job.Stage, name), []byte(body), 0600)
	}
	return job
}

func TestFinishedMediaPublishesThroughMultisave(t *testing.T) {
	s, rid := probedMedia(t)
	id := createMedia(t, s, "alice", rid, "best")["data"].(map[string]any)["id"].(string)
	job := finishMedia(t, s, id, "done", "", map[string]string{"Big Buck Bunny [aqz].mp4": "video"})
	f := &fakeSave{state: "committed", receipt: &delegate.Receipt{Path: "/home/Big Buck Bunny [aqz].mp4", Size: 5}}
	s.Delegate = f
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	st, _ := s.readState()
	got := st.Jobs[id]
	rel, _ := filepath.Rel(s.StagingDir, job.Stage)
	if !got.Published || len(got.Files) != 1 || got.Files[0].Name != "Big Buck Bunny [aqz].mp4" || f.input.Staging != rel+"/Big Buck Bunny [aqz].mp4" || got.Filename != "Big Buck Bunny [aqz].mp4" {
		t.Fatalf("%+v %+v", got, f.input)
	}
	if task := mediaTask(t, s, "alice", id); task["status"] != "done" || task["publication"].(map[string]any)["published"] != true {
		t.Fatal(task)
	}
}

func TestPlaylistFilesAreFlattenedUnderTitle(t *testing.T) {
	s, rid := probedMedia(t)
	id := createMedia(t, s, "alice", rid, "best")["data"].(map[string]any)["id"].(string)
	finishMedia(t, s, id, "done", "", map[string]string{"001 - a.mp4": "a", "002 - b.mp4": "bb", "002 - b.mp4.part": "partial"})
	f := &multiSave{}
	s.Delegate = f
	s.SaveCompleted(context.Background())
	st, _ := s.readState()
	got := st.Jobs[id]
	if len(f.moves) != 2 || len(got.Files) != 2 || got.Files[0].Name != "Big Buck Bunny - 001 - a.mp4" || got.Files[1].Name != "Big Buck Bunny - 002 - b.mp4" || got.Published {
		t.Fatalf("%+v", got.Files)
	}
}

func TestFailedMediaShowsReasonAndCanRetry(t *testing.T) {
	s, rid := probedMedia(t)
	id := createMedia(t, s, "alice", rid, "best")["data"].(map[string]any)["id"].(string)
	finishMedia(t, s, id, "error", "media_login_required", nil)
	s.Delegate = &fakeSave{}
	s.SaveCompleted(context.Background())
	task := mediaTask(t, s, "alice", id)
	if task["status"] != "error" || task["publication"].(map[string]any)["error"] != "media_login_required" {
		t.Fatal(task)
	}
	call(t, s, "alice", "gopeed.request", Input{Method: "PUT", Path: "/api/v1/tasks/" + id + "/continue"})
	if task = mediaTask(t, s, "alice", id); task["status"] != "wait" || task["publication"].(map[string]any)["error"] != "" {
		t.Fatal(task)
	}
}

func TestDeletingMediaWaitsForRunnerThenRemovesPrivateFiles(t *testing.T) {
	s, rid := probedMedia(t)
	id := createMedia(t, s, "alice", rid, "best")["data"].(map[string]any)["id"].(string)
	job := finishMedia(t, s, id, "running", "", map[string]string{"x.mp4.part": "p"})
	os.MkdirAll(s.mediaControl(id), 0700)
	if out, _ := call(t, s, "bob", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/" + id}); out["success"] == true {
		t.Fatal("foreign delete")
	}
	call(t, s, "alice", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/" + id})
	st, _ := s.readState()
	if _, ok := st.Jobs[id]; !ok {
		t.Fatal("running download removed before its process stopped")
	}
	finishMedia(t, s, id, "canceled", "", nil)
	s.SaveCompleted(context.Background())
	st, _ = s.readState()
	if _, ok := st.Jobs[id]; ok || len(st.Removals) != 1 || st.Removals[0].Owner != "uid:alice" {
		t.Fatalf("%+v", st)
	}
	for _, dir := range []string{job.Stage, s.mediaControl(id)} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("%s kept: %v", dir, err)
		}
	}
}

type multiSave struct{ moves []delegate.MoveParams }

func (m *multiSave) MoveFromStaging(_ context.Context, p delegate.MoveParams) (delegate.OperationAdmission, error) {
	m.moves = append(m.moves, p)
	return delegate.OperationAdmission{JobID: p.JobID, State: "queued"}, nil
}
func (m *multiSave) OperationStatus(_ context.Context, _ map[string]string, r delegate.OperationRef) (delegate.OperationResult, error) {
	return delegate.OperationResult{JobID: r.JobID, State: "queued"}, nil
}
func (m *multiSave) DiscardStaging(context.Context, delegate.OperationRef) error { return nil }
func (m *multiSave) CancelOperation(_ context.Context, _ map[string]string, r delegate.OperationRef) (delegate.OperationResult, error) {
	return delegate.OperationResult{JobID: r.JobID, State: "canceled"}, nil
}
