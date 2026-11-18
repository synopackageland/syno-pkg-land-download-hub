package gopeed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// btFixture fakes the official engine's BT REST surface: config, resolve,
// create, patch, task list and task detail.
type btFixture struct {
	mu        sync.Mutex
	hash      string
	resolves  []map[string]any
	posts     int
	patches   []map[string]any
	cfgWrites int
	cfg       map[string]any
	task      map[string]any
	stage     string
	// stall, when set before the first request, holds every resolve until closed.
	stall chan struct{}
	files []any
}

func newBTFixture(t *testing.T) (*Service, *btFixture) {
	t.Helper()
	s, _, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	s.checkURL = func(string) bool { return true }
	_, hash := fixtureTorrent()
	f := &btFixture{hash: hash, cfg: upstreamBT()}
	f.files = []any{map[string]any{"name": "a.flac", "path": "cd1", "size": 3}, map[string]any{"name": "cover.jpg", "path": "", "size": 4}}
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/resolve" && f.stall != nil {
			<-f.stall
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		reply := func(data any) { json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}) }
		switch {
		case r.URL.Path == "/api/v1/config":
			if r.Method == "PUT" {
				f.cfgWrites++
				json.NewDecoder(r.Body).Decode(&f.cfg)
			}
			reply(f.cfg)
		case r.URL.Path == "/api/v1/resolve":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.resolves = append(f.resolves, body)
			f.stage = body["opts"].(map[string]any)["path"].(string)
			reply(map[string]any{"id": fmt.Sprintf("rid-%d", len(f.resolves)), "res": map[string]any{"name": "Album", "hash": f.hash, "size": 7, "files": f.files}})
		case r.URL.Path == "/api/v1/tasks" && r.Method == "POST":
			f.posts++
			f.task = map[string]any{"id": "bt-task", "protocol": "bt", "status": "running", "uploading": false, "meta": map[string]any{"opts": map[string]any{"path": f.stage}, "res": map[string]any{"name": "Album", "hash": f.hash, "files": f.files}}}
			reply("bt-task")
		case r.URL.Path == "/api/v1/tasks/bt-task" && r.Method == "PATCH":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.patches = append(f.patches, body)
			if opts, ok := body["opts"].(map[string]any); ok {
				f.task["meta"].(map[string]any)["opts"].(map[string]any)["selectFiles"] = opts["selectFiles"]
			}
			reply(nil)
		case r.URL.Path == "/api/v1/tasks/bt-task" && r.Method == "GET":
			reply(f.task)
		case r.URL.Path == "/api/v1/tasks":
			if f.task == nil {
				reply([]any{})
			} else {
				reply([]any{f.task})
			}
		default:
			reply(nil)
		}
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	return s, f
}

func btResolve(t *testing.T, s *Service, user, url string) map[string]any {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"req": map[string]any{"url": url}, "opts": map[string]any{"path": "/home"}})
	out, _ := call(t, s, user, "test.resolve", json.RawMessage(body))
	return out
}

func btCreate(t *testing.T, s *Service, user, rid string) map[string]any {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"rid": rid})
	out, _ := call(t, s, user, "test.create", json.RawMessage(raw))
	return out
}

func torrentDataURI() string {
	raw, _ := fixtureTorrent()
	return torrentDataPrefix + base64.StdEncoding.EncodeToString([]byte(raw))
}

func state(t *testing.T, s *Service) State {
	t.Helper()
	st, err := s.readState()
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestUploadedTorrentResolvesIntoAPrivateHashStageWithoutSeedingDefaults(t *testing.T) {
	s, f := newBTFixture(t)
	out := btResolve(t, s, "admin", torrentDataURI())
	if out["success"] != true {
		t.Fatal(out)
	}
	if f.cfgWrites != 1 || f.cfg["protocolConfig"].(map[string]any)["bt"].(map[string]any)["seedTime"] != float64(1) {
		t.Fatal("BT config not aligned before resolve", f.cfg)
	}
	req := f.resolves[0]["req"].(map[string]any)
	stored := filepath.Join(s.Data, "torrents", f.hash+".torrent")
	raw, _ := fixtureTorrent()
	if req["url"] != stored || f.stage != filepath.Join(s.StagingDir, "bt-"+f.hash) {
		t.Fatal(f.resolves[0])
	}
	if b, err := os.ReadFile(stored); err != nil || string(b) != raw {
		t.Fatal("torrent not stored privately", err)
	}
	job := state(t, s).Resolved["rid-1"]
	if job.Kind != string(SourceTorrentFile) || job.Hash != f.hash || job.Owner != "uid:admin" || job.Filename != "Album" || job.Stage != f.stage || job.DelegationID != "grant-admin" {
		t.Fatalf("%+v", job)
	}
}

func TestMagnetResolvesWithItsOwnHashAndRefusesMismatchedMetadata(t *testing.T) {
	s, f := newBTFixture(t)
	magnet := "magnet:?xt=urn:btih:" + f.hash + "&dn=Album"
	if out := btResolve(t, s, "admin", magnet); out["success"] != true || f.resolves[0]["req"].(map[string]any)["url"] != magnet {
		t.Fatal(out, f.resolves)
	}
	f.hash = strings.Repeat("0", 40)
	if out := btResolve(t, s, "admin", "magnet:?xt=urn:btih:"+strings.Repeat("1", 40)); out["success"] != false {
		t.Fatal("accepted engine metadata for another hash", out)
	}
}

func TestInvalidTorrentNeverReachesTheEngine(t *testing.T) {
	s, f := newBTFixture(t)
	for _, bad := range []string{torrentDataPrefix + "!!!", torrentDataPrefix + base64.StdEncoding.EncodeToString([]byte("de")), torrentDataPrefix + base64.StdEncoding.EncodeToString(make([]byte, maxTorrentBytes+1))} {
		if out := btResolve(t, s, "admin", bad); out["success"] != false {
			t.Fatal(out)
		}
	}
	if len(f.resolves) != 0 {
		t.Fatal("invalid metadata reached the engine")
	}
}

func TestTorrentURLIsFetchedBoundedAndContentTypeRoutesToBT(t *testing.T) {
	s, f := newBTFixture(t)
	raw, _ := fixtureTorrent()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-bittorrent")
		if r.Method == "HEAD" {
			return
		}
		fmt.Fprint(w, raw)
	}))
	defer source.Close()
	s.fetch = source.Client()
	for _, u := range []string{source.URL + "/dist/album.torrent", source.URL + "/download?id=7"} {
		f.resolves = nil
		if out := btResolve(t, s, "admin", u); out["success"] != true || len(f.resolves) != 1 || f.resolves[0]["req"].(map[string]any)["url"] != filepath.Join(s.Data, "torrents", f.hash+".torrent") {
			t.Fatal(u, out, f.resolves)
		}
	}
	if job := state(t, s).Resolved["rid-1"]; job.Kind != string(SourceTorrentURL) {
		t.Fatalf("%+v", job)
	}
}

func TestOneTorrentBelongsToOneActiveTaskAcrossUsers(t *testing.T) {
	s, _ := newBTFixture(t)
	btResolve(t, s, "admin", torrentDataURI())
	if out := btResolve(t, s, "normal1", torrentDataURI()); out["success"] != true {
		t.Fatal("previews may share the hash stage", out)
	}
	if out := btCreate(t, s, "admin", "rid-1"); out["success"] != true {
		t.Fatal(out)
	}
	if out := btCreate(t, s, "normal1", "rid-2"); out["success"] != false || out["error"].(map[string]any)["code"] != "torrent_in_use" {
		t.Fatal(out)
	}
	if out := btResolve(t, s, "normal1", torrentDataURI()); out["success"] != false || out["error"].(map[string]any)["code"] != "torrent_in_use" {
		t.Fatal(out)
	}
}

func TestOwnerPreviewIsReplacedByItsNextResolveOfTheSameTorrent(t *testing.T) {
	s, _ := newBTFixture(t)
	btResolve(t, s, "admin", torrentDataURI())
	btResolve(t, s, "admin", torrentDataURI())
	st := state(t, s)
	if _, old := st.Resolved["rid-1"]; old || st.Resolved["rid-2"].Owner != "uid:admin" {
		t.Fatalf("%+v", st.Resolved)
	}
}

func TestMediaAddressesWaitForTheMediaDownloader(t *testing.T) {
	s, f := newBTFixture(t)
	s.Media = func(*url.URL) bool { return true }
	if out := btResolve(t, s, "admin", "https://video.example/watch?v=1"); out["success"] != false || out["error"].(map[string]any)["code"] != "media_probe_required" {
		t.Fatal(out)
	}
	if len(f.resolves) != 0 {
		t.Fatal("media address reached the HTTP engine")
	}
}

func TestLostBTCreateIsReconciledByStageAndHash(t *testing.T) {
	stage := "/stage/bt-" + strings.Repeat("a", 40)
	st := State{Jobs: map[string]Job{}, Resolved: map[string]Job{"rid": {Kind: string(SourceMagnet), Hash: strings.Repeat("a", 40), Owner: "uid:admin", Stage: stage, Filename: "Album", DelegationID: "g", SubmissionRID: "rid", FileCount: 2}}}
	list, _ := json.Marshal([]any{
		map[string]any{"id": "other", "meta": map[string]any{"opts": map[string]any{"path": stage}, "res": map[string]any{"name": "Album", "hash": strings.Repeat("b", 40)}}},
		map[string]any{"id": "bt-task", "meta": map[string]any{"opts": map[string]any{"path": stage}, "res": map[string]any{"name": "Album", "hash": strings.Repeat("a", 40)}}},
	})
	if err := reconcileTaskList(&st, list); err != nil {
		t.Fatal(err)
	}
	if job, ok := st.Jobs["bt-task"]; !ok || job.Hash != strings.Repeat("a", 40) || len(st.Resolved) != 0 || len(st.Jobs) != 1 {
		t.Fatalf("%+v", st)
	}
}

// Only a recorded resolve can be created; an address alone never reaches the engine.
func TestCreateWithoutAResolveIsRefused(t *testing.T) {
	s, f := newBTFixture(t)
	body, _ := json.Marshal(map[string]any{"req": map[string]any{"url": "magnet:?xt=urn:btih:" + f.hash}, "opts": map[string]any{"path": "/home"}})
	out, _ := call(t, s, "admin", "test.create", json.RawMessage(body))
	if out["success"] != false || out["error"].(map[string]any)["code"] != "task_denied" || f.posts != 0 {
		t.Fatal(out)
	}
}

func TestTorrentFetchRefusesPrivateAddresses(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "d") }))
	defer local.Close()
	if _, err := publicClient().Get(local.URL); err == nil {
		t.Fatal("reached a loopback address")
	}
}

// The file storage marks pieces complete from full-size files already in the
// stage, so a re-added torrent must not see a previous task's leftovers (B7).
func TestReaddedTorrentStartsFromAnEmptyStage(t *testing.T) {
	s, f := newBTFixture(t)
	stage := filepath.Join(s.StagingDir, "bt-"+f.hash)
	leftover := filepath.Join(stage, "Album", "cover.jpg")
	if err := os.MkdirAll(filepath.Dir(leftover), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(leftover, []byte("old!"), 0600); err != nil {
		t.Fatal(err)
	}
	if out := btResolve(t, s, "admin", torrentDataURI()); out["success"] != true {
		t.Fatal(out)
	}
	if f.stage != stage {
		t.Fatal(f.stage)
	}
	if entries, err := os.ReadDir(stage); err != nil || len(entries) != 0 {
		t.Fatalf("stage kept %v: %v", entries, err)
	}
}

// Upstream keeps waiting for magnet metadata after this request gives up, and
// a later resolve of the same hash answers once peers sent it. Each resolve
// therefore stops inside the CGI request limit and asks the client to poll.
func TestSlowMagnetMetadataAsksTheClientToPollWithinTheRequestLimit(t *testing.T) {
	if btResolveBudget >= api.DefaultRequestTimeout-5*time.Second {
		t.Fatal("metadata wait leaves no room inside the CGI request limit", btResolveBudget)
	}
	s, f := newBTFixture(t)
	s.metadataWait = 50 * time.Millisecond
	f.stall = make(chan struct{})
	defer close(f.stall)
	out := btResolve(t, s, "admin", "magnet:?xt=urn:btih:"+f.hash)
	if out["success"] != false || out["error"].(map[string]any)["code"] != "metadata_pending" {
		t.Fatal(out)
	}
	if len(state(t, s).Resolved) != 0 {
		t.Fatal("a pending resolve kept a preview record")
	}
}

// Peers have no deadline: a magnet still waiting after any time stays a
// waiting request, never a failure, and returns the worker between attempts.
func TestSlowMagnetPreparationKeepsWaitingAndReleasesWorker(t *testing.T) {
	s, f := newBTFixture(t)
	s.metadataWait = 20 * time.Millisecond
	f.stall = make(chan struct{})
	defer close(f.stall)
	result := createMany(t, s, "admin", item(1, "magnet:?xt=urn:btih:"+f.hash))
	id := admissionID(result[0])
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs[id]
		j.Preparation.Started = time.Now().Add(-24 * time.Hour)
		st.Jobs[id] = j
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	r := &PreparationRunner{Service: s}
	defer r.Close()
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-r.done
	j := state(t, s).Jobs[id]
	if j.Preparation.Status == "error" || j.Preparation.Error != "" || !j.Preparation.Waiting || j.Preparation.Attempted.IsZero() {
		t.Fatal(j.Preparation)
	}
	view := s.presentPreparation(id, j)
	if view["status"] != "preparing" || view["waiting"] != true {
		t.Fatal(view)
	}
	// The next tick releases the finished attempt and starts another wait.
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := r.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.done == nil {
		t.Fatal("a waiting magnet was not tried again")
	}
	<-r.done
}

// A request never tried goes before one that keeps waiting for peers.
func TestWaitingPreparationDoesNotStarveNewRequests(t *testing.T) {
	r := &PreparationRunner{}
	now := time.Now()
	got := r.order(map[string]Job{
		"a": {Owner: "u", Preparation: &Preparation{Attempted: now}},
		"b": {Owner: "u", Preparation: &Preparation{Attempted: now.Add(-time.Minute)}},
		"c": {Owner: "u", Preparation: &Preparation{}},
	})
	if strings.Join(got, ",") != "c,b,a" {
		t.Fatal(got)
	}
}
