package gopeed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
)

func asyncSearchFixture(t *testing.T) (*Service, string) {
	t.Helper()
	s := searchFixture(t)
	s.SearchIdentity = func(user string) (background.Submitter, error) {
		uid := "1201"
		if user != "normal1" {
			uid = "1202"
		}
		return background.Submitter{User: user, UID: uid}, nil
	}
	src, err := s.Search.SaveSource(context.Background(), "uid:normal1", search.SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	s.Search.Stream = func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		return emit(0, search.Reply{Results: []search.Hit{{Title: "Ubuntu", Link: "https://example.org/private?token=secret"}}})
	}
	return s, src.ID
}
func startSearchJob(t *testing.T, s *Service, source, key string) string {
	t.Helper()
	out, _ := call(t, s, "normal1", "search.jobs.start", map[string]any{"key": key, "sourceIds": []string{source}, "query": "ubuntu", "page": 1})
	if out["success"] != true {
		t.Fatal(out)
	}
	data := out["data"].(map[string]any)
	if data["outcome"] != "accepted" {
		t.Fatal(out)
	}
	return data["job"].(map[string]any)["id"].(string)
}
func runSearchJob(t *testing.T, s *Service) {
	t.Helper()
	runner := s.SearchRunner(background.WorkerInfo{PID: 1, UID: 1000, Username: "pkg"})
	job, err := runner.Store.Claim(context.Background())
	if err != nil || job == nil {
		t.Fatalf("claim: %v %v", job, err)
	}
	runner.Process(context.Background(), job)
}
func TestSearchJobsAdmissionIsPromptOwnerBoundAndDoesNotRunWork(t *testing.T) {
	s, src := asyncSearchFixture(t)
	s.Search.Stream = func(context.Context, search.Request, func(int, search.Reply) error) error {
		t.Fatal("request ran worker")
		return nil
	}
	started := time.Now()
	id := startSearchJob(t, s, src, "first")
	if time.Since(started) > time.Second {
		t.Fatal("slow admission")
	}
	out, _ := call(t, s, "normal1", "search.jobs.start", map[string]any{"key": "second", "sourceIds": []string{src}, "query": "different", "page": 1})
	d := out["data"].(map[string]any)
	if d["outcome"] != "busy" || d["job"].(map[string]any)["id"] != id {
		t.Fatal(out)
	}
	for _, u := range []string{"normal2", "admin"} {
		for _, m := range []string{"search.jobs.status", "search.jobs.result", "search.jobs.cancel", "search.jobs.pages"} {
			params := map[string]any{"id": id}
			if m == "search.jobs.pages" {
				params["offset"], params["limit"] = 0, 1
			}
			out, _ := call(t, s, u, m, params)
			if out["success"] != false {
				t.Fatal("foreign job", u, m, out)
			}
		}
	}
	for _, m := range []string{"search.stream", "search.query", "search.batch", "search.sources.test", "search.sources.unlock", "search.sources.clearance.test"} {
		out, _ := call(t, s, "normal1", m, map[string]any{})
		if errorCode(out) != "unknown_method" {
			t.Fatal("legacy synchronous route retained", m, out)
		}
	}
	out, _ = call(t, s, "normal1", "search.jobs.result", map[string]any{"id": id})
	if out["success"] != false {
		t.Fatal("queued result", out)
	}
	out, _ = call(t, s, "normal1", "search.jobs.cancel", map[string]any{"id": id})
	if out["success"] != true || out["data"].(map[string]any)["state"] != "canceled" {
		t.Fatal(out)
	}
}
func TestSearchJobsWorkerResultsAndLargePagesUseSDKOwnership(t *testing.T) {
	s, src := asyncSearchFixture(t)
	s.Search.Stream = func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		hits := []search.Hit{}
		for i := 0; i < 100; i++ {
			hits = append(hits, search.Hit{Title: fmt.Sprintf("%03d %s", i, strings.Repeat("Ubuntu ", 70)), Link: fmt.Sprintf("https://example.org/file/%d?token=secret", i)})
		}
		return emit(0, search.Reply{Results: hits})
	}
	id := startSearchJob(t, s, src, "large-results")
	runSearchJob(t, s)
	out, _ := call(t, s, "normal1", "search.jobs.status", map[string]any{"id": id})
	if out["success"] != true || out["data"].(map[string]any)["state"] != "succeeded" {
		t.Fatal(out)
	}
	// A different CGI assembly can reconnect by id with no in-memory goroutine.
	again := *s
	again.Data = filepath.Clean(s.Data)
	out, _ = call(t, &again, "normal1", "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
	if out["success"] != true {
		t.Fatal(out)
	}
	page := out["data"].(map[string]any)
	rows := page["items"].([]any)
	if len(rows) != 1 || len(rows[0].(map[string]any)["results"].([]any)) != 100 {
		t.Fatal(out)
	}
	b, _ := json.Marshal(out)
	if len(b) <= background.MaxResultBytes || strings.Contains(string(b), "token=secret") {
		t.Fatal("lost large results or secret leak")
	}
	out, _ = call(t, s, "normal1", "search.jobs.result", map[string]any{"id": id})
	if out["success"] != true {
		t.Fatal(out)
	}
	b, _ = json.Marshal(out)
	if len(b) > background.MaxResultBytes {
		t.Fatal("unbounded SDK result")
	}
	for _, user := range []string{"normal2", "admin"} {
		for _, method := range []string{"search.jobs.result", "search.jobs.pages"} {
			params := map[string]any{"id": id}
			if method == "search.jobs.pages" {
				params["offset"], params["limit"] = 0, 1
			}
			out, _ := call(t, s, user, method, params)
			if out["success"] != false {
				t.Fatal("completed foreign results visible", user, method, out)
			}
		}
	}
	for _, params := range []map[string]any{{"id": id, "offset": -1, "limit": 1}, {"id": id, "offset": 0, "limit": 101}, {"id": id, "owner": "normal2"}} {
		out, _ = call(t, s, "normal1", "search.jobs.pages", params)
		if out["success"] != false {
			t.Fatal("invalid bounds accepted", out)
		}
	}
}

func TestSearchJobsUIDChangeRefusesPendingWorkAndReads(t *testing.T) {
	s, src := asyncSearchFixture(t)
	id := startSearchJob(t, s, src, "uid-change")
	s.SearchIdentity = func(name string) (background.Submitter, error) {
		return background.Submitter{User: name, UID: "9999"}, nil
	}
	out, _ := call(t, s, "normal1", "search.jobs.status", map[string]any{"id": id})
	if out["success"] != false {
		t.Fatal("old uid visible", out)
	}
	runSearchJob(t, s)
	jobs, err := s.searchJobStore().ListFor(context.Background(), background.Access{Admin: true})
	if err != nil || len(jobs) != 1 || jobs[0].State != background.StateFailed {
		t.Fatal(jobs, err)
	}
}

func TestSearchJobWorkerProcess(t *testing.T) {
	if os.Getenv("SEARCH_WORKER_CHILD") != "1" {
		return
	}
	data := os.Getenv("SEARCH_WORKER_DATA")
	s := New(data)
	s.Search = &search.Service{Data: filepath.Join(data, "search"), Stream: func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		return emit(0, search.Reply{Results: []search.Hit{{Title: "From independent worker", Link: "https://example.org/file"}}})
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := s.SearchRunner(background.WorkerInfo{PID: os.Getpid(), UID: os.Getuid(), Username: os.Getenv("SEARCH_WORKER_USER")}).Run(ctx); err != nil {
		t.Fatal(err)
	}
}
func TestSearchJobsRunOutsideRequestInIndependentSDKWorker(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("production worker refuses root")
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	s := New(t.TempDir())
	s.Search = &search.Service{Data: filepath.Join(s.Data, "search")}
	owner, err := s.Owner(u.Username)
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.Search.SaveSource(context.Background(), owner, search.SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := call(t, s, u.Username, "search.jobs.start", map[string]any{"key": "process-proof", "sourceIds": []string{src.ID}, "query": "ubuntu", "page": 1})
	if out["success"] != true {
		t.Fatal(out)
	}
	id := out["data"].(map[string]any)["job"].(map[string]any)["id"].(string)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^TestSearchJobWorkerProcess$")
	cmd.Env = append(os.Environ(), "SEARCH_WORKER_CHILD=1", "SEARCH_WORKER_DATA="+s.Data, "SEARCH_WORKER_USER="+u.Username)
	if raw, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("independent worker: %v %s", err, raw)
	}
	out, _ = call(t, s, u.Username, "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
	if out["success"] != true {
		t.Fatal(out)
	}
	page := out["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if page["results"].([]any)[0].(map[string]any)["title"] != "From independent worker" {
		t.Fatal(out)
	}
}
func TestSearchJobsArtifactsExpireAndDoNotMixWorkerAttempts(t *testing.T) {
	s, src := asyncSearchFixture(t)
	id := startSearchJob(t, s, src, "artifact-expiry")
	runSearchJob(t, s)
	path := filepath.Join(s.searchPagesDir(), id+".json")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	out, _ := call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
	if errorCode(out) != "search_result_expired" {
		t.Fatal(out)
	}
	// A retained data file can never be read as the output of another attempt.
	artifact := searchArtifact{StartedAt: time.Unix(1, 0), Pages: []search.SourcePage{}}
	if err := binaries.Atomic(path, artifact); err != nil {
		t.Fatal(err)
	}
	out, _ = call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
	if errorCode(out) != "search_result_unavailable" {
		t.Fatal(out)
	}
}

func TestSearchJobArtifactsEnforceCountAndCapacity(t *testing.T) {
	s, _, _ := fixture(t)
	for i := 0; i < 65; i++ {
		if err := s.writeSearchPages(context.Background(), fmt.Sprintf("bounded-%d", i), time.Now(), []search.SourcePage{}); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(s.searchPagesDir())
	if err != nil || len(entries) != 64 {
		t.Fatal(len(entries), err)
	}
	// A sparse retained file exercises total capacity without filling the disk.
	large := filepath.Join(s.searchPagesDir(), "sparse.json")
	f, err := os.OpenFile(large, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxSearchArtifacts); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	if err := s.writeSearchPages(context.Background(), "newest", time.Now(), []search.SourcePage{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(large); !os.IsNotExist(err) {
		t.Fatal("capacity was not enforced", err)
	}
}

// The chosen category travels with the job to the helper; unknown ones are refused.
func TestSearchJobCarriesTheCategory(t *testing.T) {
	s, src := asyncSearchFixture(t)
	var got []int
	s.Search.Stream = func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		for _, x := range in.Searches {
			got = append(got, x.Categories...)
		}
		got = append(got, in.Categories...)
		return emit(0, search.Reply{})
	}
	out, _ := call(t, s, "normal1", "search.jobs.start", map[string]any{"key": "cat", "sourceIds": []string{src}, "query": "ubuntu", "page": 1, "category": "movie"})
	if out["success"] != true {
		t.Fatal(out)
	}
	runSearchJob(t, s)
	if len(got) != 1 || got[0] != 2000 {
		t.Fatalf("%v", got)
	}
	out, _ = call(t, s, "normal1", "search.jobs.start", map[string]any{"key": "bad", "sourceIds": []string{src}, "query": "ubuntu", "page": 1, "category": "adult"})
	if out["success"] == true {
		t.Fatal(out)
	}
}
