package gopeed

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"dsmapp/internal/multisave"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// multiMover is an SDK delegate that commits every admitted file unless told
// otherwise by destination name.
type multiMover struct {
	moves    []delegate.MoveParams
	cancels  []string
	fail     map[string]string
	state    map[string]string
	statuses map[string]delegate.OperationResult
	discards []string
	discard  error
}

func (m *multiMover) MoveFromStaging(_ context.Context, p delegate.MoveParams) (delegate.OperationAdmission, error) {
	m.moves = append(m.moves, p)
	if code := m.fail[p.Name]; code != "" {
		return delegate.OperationAdmission{}, &api.Error{Code: code}
	}
	if _, ok := m.statuses[p.JobID]; !ok {
		out := delegate.OperationResult{JobID: p.JobID, State: "committed", Receipt: &delegate.Receipt{Path: path.Join("/home", p.Name), Size: p.Size}}
		if st := m.state[p.Name]; st != "" {
			out = delegate.OperationResult{JobID: p.JobID, State: st}
		}
		m.statuses[p.JobID] = out
	}
	return delegate.OperationAdmission{JobID: p.JobID, State: "queued"}, nil
}
func (m *multiMover) OperationStatus(_ context.Context, _ map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	return m.statuses[ref.JobID], nil
}
func (m *multiMover) CancelOperation(_ context.Context, _ map[string]string, ref delegate.OperationRef) (delegate.OperationResult, error) {
	m.cancels = append(m.cancels, ref.JobID)
	out := delegate.OperationResult{JobID: ref.JobID, State: "canceled"}
	m.statuses[ref.JobID] = out
	return out, nil
}
func (m *multiMover) DiscardStaging(_ context.Context, ref delegate.OperationRef) error {
	m.discards = append(m.discards, ref.JobID)
	return m.discard
}

// createdBT resolves and creates the fixture torrent and stages its files.
func createdBT(t *testing.T) (*Service, *btFixture, *multiMover) {
	t.Helper()
	s, f := newBTFixture(t)
	m := &multiMover{fail: map[string]string{}, state: map[string]string{}, statuses: map[string]delegate.OperationResult{}}
	s.Delegate = m
	btResolve(t, s, "admin", torrentDataURI())
	if out := btCreate(t, s, "admin", "rid-1"); out["success"] != true {
		t.Fatal(out)
	}
	stageFile(t, f.stage, "Album/cd1/a.flac", "abc")
	stageFile(t, f.stage, "Album/cover.jpg", "jpeg")
	return s, f, m
}

func stageFile(t *testing.T, stage, rel, body string) {
	t.Helper()
	p := filepath.Join(stage, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

func (f *btFixture) finish(uploading bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.task["status"], f.task["uploading"] = "done", uploading
}

func TestBTTaskPublishesEveryFileOnlyAfterSeedingStops(t *testing.T) {
	s, f, m := createdBT(t)
	ctx := context.Background()
	_ = s.SaveCompleted(ctx)
	f.finish(true)
	_ = s.SaveCompleted(ctx)
	if len(m.moves) != 0 {
		t.Fatal("moved files of a running or seeding task", m.moves)
	}
	f.finish(false)
	if err := s.SaveCompleted(ctx); err != nil {
		t.Fatal(err)
	}
	job := state(t, s).Jobs["bt-task"]
	if !job.Published || job.SaveState != "committed" || len(job.Files) != 2 || len(m.moves) != 2 {
		t.Fatalf("%+v %+v", job, m.moves)
	}
	stage := "bt-" + f.hash
	if m.moves[0].Staging != stage+"/Album/cd1/a.flac" || m.moves[0].Name != "Album - cd1 - a.flac" || m.moves[1].Name != "Album - cover.jpg" || m.moves[0].DelegationID != "grant-admin" {
		t.Fatalf("%+v", m.moves)
	}
	if job.Files[0].Rel != "Album/cd1/a.flac" {
		t.Fatalf("%+v", job.Files)
	}
	_ = s.SaveCompleted(ctx)
	if len(m.moves) != 2 {
		t.Fatal("published task moved again")
	}
}

func TestBTFilesWithUnexpectedSizesAreNeverPublished(t *testing.T) {
	s, f, m := createdBT(t)
	stageFile(t, f.stage, "Album/cd1/a.flac", "ab")
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	if job := state(t, s).Jobs["bt-task"]; job.SaveStarted || terminalSave(job) || len(m.moves) != 0 {
		t.Fatalf("%+v %+v", job, m.moves)
	}
}

// The engine may report done before its storage has renamed a finished
// ".part" file; the save waits for the final file instead of failing.
func TestBTFileFinalizedAfterDoneIsPublishedOnALaterPass(t *testing.T) {
	s, f, m := createdBT(t)
	final := filepath.Join(f.stage, "Album/cd1/a.flac")
	if err := os.Rename(final, final+".part"); err != nil {
		t.Fatal(err)
	}
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	if job := state(t, s).Jobs["bt-task"]; job.SaveStarted || terminalSave(job) || len(m.moves) != 0 {
		t.Fatalf("%+v %+v", job, m.moves)
	}
	if err := os.Rename(final+".part", final); err != nil {
		t.Fatal(err)
	}
	_ = s.SaveCompleted(context.Background())
	if job := state(t, s).Jobs["bt-task"]; !job.Published || len(m.moves) != 2 {
		t.Fatalf("%+v %+v", job, m.moves)
	}
}

func TestPartialFailureRetriesOnlyTheFailedFiles(t *testing.T) {
	s, f, m := createdBT(t)
	m.fail["Album - cover.jpg"] = "EXISTS"
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	job := state(t, s).Jobs["bt-task"]
	if job.Published || job.SaveState != "save_failed" || job.SaveError != "EXISTS" || job.Files[0].State != "committed" {
		t.Fatalf("%+v", job)
	}
	_ = s.SaveCompleted(context.Background())
	if len(m.moves) != 2 {
		t.Fatal("terminal failure retried without the owner", m.moves)
	}
	delete(m.fail, "Album - cover.jpg")
	if out, _ := call(t, s, "normal1", "gopeed.publish", map[string]string{"id": "bt-task"}); out["success"] != false {
		t.Fatal("another user retried", out)
	}
	if out, _ := call(t, s, "admin", "gopeed.publish", map[string]string{"id": "bt-task"}); out["success"] != true {
		t.Fatal(out)
	}
	if len(m.moves) != 3 || m.moves[2].Name != "Album - cover.jpg" || !strings.HasSuffix(m.moves[2].JobID, ".f1.a1") {
		t.Fatalf("%+v", m.moves)
	}
	if !state(t, s).Jobs["bt-task"].Published {
		t.Fatal("retry did not publish")
	}
}

func TestDeletingBTTaskCancelsUnfinishedFilesAndRemovesItsStage(t *testing.T) {
	s, f, m := createdBT(t)
	m.state["Album - cover.jpg"] = "copying"
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	out, _ := call(t, s, "admin", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/bt-task"})
	if out["success"] != true {
		t.Fatal(out)
	}
	if len(m.cancels) != 1 || !strings.HasSuffix(m.cancels[0], ".f1.a0") {
		t.Fatal(m.cancels)
	}
	// Only the canceled file kept a staging source; the committed one has none.
	if len(m.discards) != 1 || m.discards[0] != m.cancels[0] {
		t.Fatal("staging not discarded through the SDK", m.discards)
	}
	st := state(t, s)
	if _, ok := st.Jobs["bt-task"]; ok || len(st.Removals) != 1 || !st.Removals[0].FileKept {
		t.Fatalf("%+v", st)
	}
	if _, err := os.Stat(f.stage); !os.IsNotExist(err) {
		t.Fatal("stage kept", err)
	}
	if _, err := os.Stat(s.torrentPath(f.hash)); !os.IsNotExist(err) {
		t.Fatal("torrent kept", err)
	}
}

// A file whose move may still copy keeps the task deleting until the SDK
// discards its staging.
func TestDeletingBTTaskWaitsUntilTheSDKDiscardsTheStaging(t *testing.T) {
	s, f, m := createdBT(t)
	m.state["Album - cover.jpg"] = "copying"
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	m.discard = &api.Error{Code: "operation-active"}
	call(t, s, "admin", "gopeed.request", Input{Method: "DELETE", Path: "/api/v1/tasks/bt-task"})
	if job, ok := state(t, s).Jobs["bt-task"]; !ok || job.DeleteError != "operation-active" {
		t.Fatalf("deleted before the staging was discarded: %+v", job)
	}
	m.discard = nil
	_ = s.SaveCompleted(context.Background())
	if _, ok := state(t, s).Jobs["bt-task"]; ok || len(m.cancels) != 1 || len(m.discards) != 2 {
		t.Fatal("deletion did not finish", m.cancels, m.discards)
	}
}

func TestBTTaskViewShowsFilesButNoPrivatePaths(t *testing.T) {
	s, f, _ := createdBT(t)
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	out, _ := call(t, s, "admin", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks/bt-task"})
	data := out["data"].(map[string]any)["data"].(map[string]any)
	files := data["publication"].(map[string]any)["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["name"] != "Album - cd1 - a.flac" || files[0].(map[string]any)["state"] != "committed" {
		t.Fatal(files)
	}
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), s.StagingDir) || strings.Contains(string(raw), s.Data) {
		t.Fatal("private path exposed", string(raw))
	}
	if got := revealName(state(t, s).Jobs["bt-task"]); got != "Album - cd1 - a.flac" {
		t.Fatal("reveal opens", got)
	}
}

func TestPreBTJobsFileStaysReadableAsSingleFileJobs(t *testing.T) {
	s, _, _ := fixture(t)
	legacy := `{"jobs":{"old":{"owner":"uid:alice","stage":"/s/x","destination":"/home","filename":"a.iso","published":true,"uncertain":false,"delegationId":"g","saveJobId":"gopeed-old","saveState":"committed"}},"resolved":{},"folders":{"uid:alice":"/home"}}`
	if err := os.WriteFile(filepath.Join(s.Data, "jobs.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	job := state(t, s).Jobs["old"]
	if job.multiFile() || !job.Published || job.Filename != "a.iso" {
		t.Fatalf("%+v", job)
	}
}

func TestTrackedFilesShareOneBoundedBudget(t *testing.T) {
	s, f := newBTFixture(t)
	btResolve(t, s, "admin", torrentDataURI())
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["big"] = Job{Owner: "uid:normal1", Hash: strings.Repeat("c", 40), FileCount: maxTrackedFiles - 1}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out := btCreate(t, s, "admin", "rid-1"); out["success"] != false || out["error"].(map[string]any)["code"] != "queue_limit" || f.posts != 0 {
		t.Fatal(out)
	}
	if _, err = s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["big"] = Job{Owner: "uid:normal1", Hash: strings.Repeat("c", 40), FileCount: maxTrackedFiles - 2}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if out := btCreate(t, s, "admin", "rid-1"); out["success"] != true {
		t.Fatal("both files fit", out)
	}
}

func TestOwnerRetryRepublishesOnlyFailedMediaFiles(t *testing.T) {
	s, _, _ := fixture(t)
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	stage := filepath.Join(s.StagingDir, "media-stage")
	stageFile(t, stage, "a.mp4", "abc")
	stageFile(t, stage, "b.mp4", "abcd")
	m := &multiMover{fail: map[string]string{}, state: map[string]string{}, statuses: map[string]delegate.OperationResult{}}
	s.Delegate = m
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["media-1"] = Job{Kind: string(SourceMedia), Owner: "uid:admin", Stage: stage, Destination: "/home", DelegationID: "grant-admin", Filename: "List",
			Media: &MediaJob{URL: "https://video.example/list", Title: "List", Status: "done"}, SaveStarted: true, SaveState: "save_failed", SaveError: "EXISTS",
			Files: []multisave.Item{{Source: "a.mp4", Rel: "List/a.mp4", Name: "List - a.mp4", Size: 3, State: "committed"}, {Source: "b.mp4", Rel: "List/b.mp4", Name: "List - b.mp4", Size: 4, State: "save_failed", Code: "EXISTS"}}}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := call(t, s, "admin", "gopeed.publish", map[string]string{"id": "media-1"}); out["success"] != true {
		t.Fatal(out)
	}
	job := state(t, s).Jobs["media-1"]
	if len(m.moves) != 1 || m.moves[0].Name != "List - b.mp4" || !job.Published || job.Media == nil {
		t.Fatalf("%+v %+v", m.moves, job)
	}
}

func TestOwnerPublishOfAnUnplannedMediaJobUsesTheMediaPlan(t *testing.T) {
	s, _, _ := fixture(t)
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	stage := filepath.Join(s.StagingDir, "media-stage")
	stageFile(t, stage, "clip.mp4", "abc")
	m := &multiMover{fail: map[string]string{}, state: map[string]string{}, statuses: map[string]delegate.OperationResult{}}
	s.Delegate = m
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["media-2"] = Job{Kind: string(SourceMedia), Owner: "uid:admin", Stage: stage, Destination: "/home", DelegationID: "grant-admin",
			Media: &MediaJob{URL: "https://video.example/v", Title: "Clip", Status: "done"}}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if out, _ := call(t, s, "admin", "gopeed.publish", map[string]string{"id": "media-2"}); out["success"] != true {
		t.Fatal(out)
	}
	if len(m.moves) != 1 || m.moves[0].Name != "clip.mp4" || !state(t, s).Jobs["media-2"].Published {
		t.Fatalf("%+v", m.moves)
	}
}

// The owner may retry a failed file while another file is still saving (B2).
func TestFailedFileRetriesWhileAnotherFileIsStillSaving(t *testing.T) {
	s, f, m := createdBT(t)
	m.state["Album - cd1 - a.flac"] = "copying"
	m.fail["Album - cover.jpg"] = "destination-exists"
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	job := state(t, s).Jobs["bt-task"]
	if job.Files[0].State != "copying" || job.Files[1].State != "save_failed" {
		t.Fatalf("%+v", job.Files)
	}
	delete(m.fail, "Album - cover.jpg")
	if out, _ := call(t, s, "admin", "gopeed.publish", map[string]string{"id": "bt-task"}); out["success"] != true {
		t.Fatal(out)
	}
	last := m.moves[len(m.moves)-1]
	if last.Name != "Album - cover.jpg" || !strings.HasSuffix(last.JobID, ".f1.a1") {
		t.Fatalf("%+v", m.moves)
	}
	if job = state(t, s).Jobs["bt-task"]; job.Files[1].State != "committed" {
		t.Fatalf("%+v", job.Files)
	}
}

// The engine creates zero-length files only when it opens a torrent, and a
// repeated resolve clears the stage after that; such a file has no data.
func TestBTZeroLengthFileMissingFromTheStageIsStillPublished(t *testing.T) {
	s, f := newBTFixture(t)
	f.files = append(f.files, map[string]any{"name": "empty.nfo", "path": "", "size": 0})
	m := &multiMover{fail: map[string]string{}, state: map[string]string{}, statuses: map[string]delegate.OperationResult{}}
	s.Delegate = m
	btResolve(t, s, "admin", torrentDataURI())
	if out := btCreate(t, s, "admin", "rid-1"); out["success"] != true {
		t.Fatal(out)
	}
	stageFile(t, f.stage, "Album/cd1/a.flac", "abc")
	stageFile(t, f.stage, "Album/cover.jpg", "jpeg")
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	if job := state(t, s).Jobs["bt-task"]; !job.Published || len(m.moves) != 3 || m.moves[2].Size != 0 {
		t.Fatalf("%+v %+v", job, m.moves)
	}
}

// The UI no longer chooses files; a stray choice in the request is ignored.
func TestATorrentAlwaysDownloadsAndPublishesEveryFile(t *testing.T) {
	s, f := newBTFixture(t)
	m := &multiMover{fail: map[string]string{}, state: map[string]string{}, statuses: map[string]delegate.OperationResult{}}
	s.Delegate = m
	btResolve(t, s, "admin", torrentDataURI())
	raw, _ := json.Marshal(map[string]any{"rid": "rid-1", "opts": map[string]any{"selectFiles": []int{1}}})
	if out, _ := call(t, s, "admin", "test.create", json.RawMessage(raw)); out["success"] != true {
		t.Fatal(out)
	}
	if len(f.patches) != 0 {
		t.Fatal("selection sent", f.patches)
	}
	stageFile(t, f.stage, "Album/cd1/a.flac", "abc")
	stageFile(t, f.stage, "Album/cover.jpg", "jpeg")
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	if len(m.moves) != 2 || !state(t, s).Jobs["bt-task"].Published {
		t.Fatalf("%+v", m.moves)
	}
}

func selectFiles(t *testing.T, s *Service, user string, files any) map[string]any {
	t.Helper()
	out, _ := call(t, s, user, "downloads.select_files", map[string]any{"id": "bt-task", "files": files})
	return out
}

// The owner chooses files while the torrent downloads; a choice the engine
// lost is sent again and only the chosen files are saved.
func TestChosenFilesAreReappliedAndOnlyTheyArePublished(t *testing.T) {
	s, f, m := createdBT(t)
	if out := selectFiles(t, s, "admin", []int{1}); out["success"] != true {
		t.Fatal(out)
	}
	if len(f.patches) != 1 || !reflect.DeepEqual(f.patches[0]["opts"], map[string]any{"selectFiles": []any{float64(1)}}) {
		t.Fatal(f.patches)
	}
	if got := state(t, s).Jobs["bt-task"].Select; !reflect.DeepEqual(got, []int{1}) {
		t.Fatal(got)
	}
	f.mu.Lock()
	f.task["meta"].(map[string]any)["opts"].(map[string]any)["selectFiles"] = []any{0, 1}
	f.mu.Unlock()
	_ = s.SaveCompleted(context.Background())
	if len(f.patches) != 2 {
		t.Fatal("lost selection not reapplied", f.patches)
	}
	f.finish(false)
	_ = s.SaveCompleted(context.Background())
	if len(m.moves) != 1 || m.moves[0].Name != "cover.jpg" || m.moves[0].Staging != "bt-"+f.hash+"/Album/cover.jpg" {
		t.Fatalf("%+v", m.moves)
	}
	if !state(t, s).Jobs["bt-task"].Published {
		t.Fatal("not published")
	}
	if out := selectFiles(t, s, "admin", []int{0}); out["success"] != false || out["error"].(map[string]any)["code"] != "selection_closed" {
		t.Fatal("a saved task changed its files", out)
	}
}

func TestFileChoicesAreValidatedAndOwnerOnly(t *testing.T) {
	s, f, _ := createdBT(t)
	for _, files := range []any{[]int{}, []int{2}, []int{-1}, []int{1, 1}, []any{0.5}, nil} {
		if out := selectFiles(t, s, "admin", files); out["success"] != false || out["error"].(map[string]any)["code"] != "invalid_selection" {
			t.Fatal(files, out)
		}
	}
	if out := selectFiles(t, s, "normal1", []int{0}); out["success"] != false || out["error"].(map[string]any)["code"] != "task_denied" {
		t.Fatal(out)
	}
	if len(f.patches) != 0 {
		t.Fatal(f.patches)
	}
	// Choosing every file again is the default: no stored choice.
	if out := selectFiles(t, s, "admin", []int{1, 0}); out["success"] != true || state(t, s).Jobs["bt-task"].Select != nil {
		t.Fatal(out)
	}
	if len(f.patches) != 1 || !reflect.DeepEqual(f.patches[0]["opts"], map[string]any{"selectFiles": []any{float64(0), float64(1)}}) {
		t.Fatal(f.patches)
	}
}
