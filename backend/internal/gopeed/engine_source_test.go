package gopeed

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
)

// engineFixture fakes the engine's resolve and create API for addresses the
// App leaves to the engine and its extensions.
type engineFixture struct {
	mu       sync.Mutex
	resolves []map[string]any
	creates  []map[string]any
	tasks    []map[string]any
	// resolve answers one resolve request for the given URL.
	resolve func(url string) (code int, data any)
	// onCreate runs before the engine accepts a create request.
	onCreate func()
	// extensions is the engine's installed extension list.
	extensions []any
	cfg        map[string]any
}

func newEngineFixture(t *testing.T) (*Service, *engineFixture) {
	t.Helper()
	s, _, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	f := &engineFixture{cfg: upstreamProtocols(), resolve: func(string) (int, any) {
		return 0, map[string]any{"id": "rid-1", "res": map[string]any{"files": []any{map[string]any{"name": "file.bin", "path": "", "size": 8}}}}
	}}
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		// Upstream puts a refusal's error text in msg.
		reply := func(code int, data any) {
			if msg, ok := data.(string); ok && code != 0 {
				json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": msg})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": code, "data": data})
		}
		switch {
		case r.URL.Path == "/api/v1/config":
			if r.Method == "PUT" {
				f.cfg = body
			}
			reply(0, f.cfg)
		case r.URL.Path == "/api/v1/resolve":
			f.resolves = append(f.resolves, body)
			code, data := f.resolve(body["req"].(map[string]any)["url"].(string))
			reply(code, data)
		case r.URL.Path == "/api/v1/tasks" && r.Method == "POST":
			if f.onCreate != nil {
				f.onCreate()
			}
			f.creates = append(f.creates, body)
			id := fmt.Sprintf("task-%d", len(f.creates))
			opts, _ := body["opts"].(map[string]any)
			f.tasks = append(f.tasks, map[string]any{"id": id, "status": "running", "meta": map[string]any{"opts": opts}})
			reply(0, id)
		case r.URL.Path == "/api/v1/extensions":
			reply(0, f.extensions)
		case r.URL.Path == "/api/v1/tasks":
			reply(0, f.tasks)
		default:
			reply(0, nil)
		}
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	return s, f
}

// replyData is the engine reply the App returned to the browser.
func replyData(out map[string]any) map[string]any {
	data, _ := out["data"].(map[string]any)
	inner, _ := data["data"].(map[string]any)
	return inner
}

func errorCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	code, _ := e["code"].(string)
	return code
}

func TestEngineAddressesOfAnySchemeReachTheEngine(t *testing.T) {
	s, f := newEngineFixture(t)
	for raw, sent := range map[string]string{
		ed2kLink: ed2kLink,
		"magnet:?xt=urn:ed2k:0123456789abcdef0123456789abcdef&xl=1048576&dn=ubuntu.iso": "ed2k://|file|ubuntu.iso|1048576|0123456789ABCDEF0123456789ABCDEF|/",
		"pan-share:abc123":               "pan-share:abc123",
		"ftp://example.com/pub/file.bin": "ftp://example.com/pub/file.bin",
	} {
		if out := btResolve(t, s, "admin", raw); out["success"] != true {
			t.Fatalf("%s: %v", raw, out)
		}
		f.mu.Lock()
		got := f.resolves[len(f.resolves)-1]["req"].(map[string]any)["url"]
		f.mu.Unlock()
		if got != sent {
			t.Fatalf("%s: engine received %v, want %s", raw, got, sent)
		}
	}
	st := state(t, s)
	if job := st.Resolved["rid-1"]; job.Owner != "uid:admin" || job.Filename != "file.bin" || job.DelegationID != "grant-admin" {
		t.Fatalf("%+v", job)
	}
}

func TestAddressTheEngineCannotResolveIsReportedAsUnsupported(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) { return 1000, "unsupported protocol" }
	if out := btResolve(t, s, "admin", "pan-share:unknown"); errorCode(out) != "source_unsupported" {
		t.Fatal(out)
	}
}

// a supported address whose server fails (EOF, an HTTP error) is
// unreachable, not unsupported; no extension would help.
func TestAddressWhoseServerFailsIsReportedAsUnreachable(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) { return 1000, "EOF" }
	if out := btResolve(t, s, "admin", "https://files.example/100MB.bin"); errorCode(out) != "source_unreachable" {
		t.Fatal(out)
	}
}

func TestEngineAddressesKeepTheLocalAndPrivateFloor(t *testing.T) {
	s, f := newEngineFixture(t)
	for _, raw := range []string{"ftp://127.0.0.1/x", "ftp://user:pw@example.com/x", "http://10.0.0.2/x", "file:///etc/passwd", "/var/packages/gopeed/var/x.torrent", "data:text/html,x"} {
		if out := btResolve(t, s, "admin", raw); errorCode(out) != "url_denied" {
			t.Fatalf("%s: %v", raw, out)
		}
	}
	if len(f.resolves) != 0 {
		t.Fatalf("engine reached: %v", f.resolves)
	}
}

// galleryResult is an extension's onResolve result: no resolve ID, and every
// file carries the request the engine downloads it with.
func galleryResult(urls ...string) map[string]any {
	files := []any{}
	for i, u := range urls {
		files = append(files, map[string]any{"name": fmt.Sprintf("%d.jpg", i+1), "path": "set", "size": 3,
			"req": map[string]any{"url": u, "extra": map[string]any{"header": map[string]any{"Referer": "https://pan.example/"}}}})
	}
	return map[string]any{"id": "", "res": map[string]any{"name": "Gallery", "files": files}}
}

func TestExtensionResultIsPreviewedWithoutItsRequests(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, galleryResult("https://cdn.example/1.jpg", "https://cdn.example/2.jpg")
	}
	out := btResolve(t, s, "admin", "pan-share:abc")
	data := replyData(out)
	rid, _ := data["id"].(string)
	if out["success"] != true || rid == "" {
		t.Fatal(out)
	}
	files := data["res"].(map[string]any)["files"].([]any)
	if len(files) != 2 || files[0].(map[string]any)["req"] != nil || files[1].(map[string]any)["path"] != "set" {
		t.Fatalf("preview exposes requests or loses layout: %v", files)
	}
	if job := state(t, s).Resolved[rid]; job.Kind != "extension" || job.Owner != "uid:admin" || len(job.Requests) != 2 || job.FileCount != 2 {
		t.Fatalf("%+v", job)
	}
}

func TestEachExtensionFileBecomesItsOwnEngineTask(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, galleryResult("https://cdn.example/1.jpg", "https://cdn.example/2.jpg", "https://cdn.example/3.jpg")
	}
	rid := replyData(btResolve(t, s, "admin", "pan-share:abc"))["id"].(string)
	if out := btCreate(t, s, "admin", rid); out["success"] != true {
		t.Fatal(out)
	}
	if len(f.creates) != 3 {
		t.Fatalf("creates: %v", f.creates)
	}
	st := state(t, s)
	stages := map[string]bool{}
	for i, want := range []string{"https://cdn.example/1.jpg", "https://cdn.example/2.jpg", "https://cdn.example/3.jpg"} {
		c := f.creates[i]
		req, opts := c["req"].(map[string]any), c["opts"].(map[string]any)
		if req["url"] != want || req["extra"] == nil || req["skipVerifyCert"] != false {
			t.Fatalf("request %d: %v", i, req)
		}
		job, ok := st.Jobs[fmt.Sprintf("task-%d", i+1)]
		if !ok || job.Owner != "uid:admin" || job.DelegationID != "grant-admin" || job.Stage != opts["path"] || job.Filename != opts["name"] || job.Kind != "" {
			t.Fatalf("job %d: %+v for %v", i, job, opts)
		}
		stages[job.Stage] = true
	}
	if len(stages) != 3 || f.creates[0]["opts"].(map[string]any)["name"] != "Gallery - set - 1.jpg" {
		t.Fatalf("stages %v, first %v", stages, f.creates[0]["opts"])
	}
	if len(st.Resolved) != 0 {
		t.Fatalf("leftover previews: %v", st.Resolved)
	}
}

func TestExtensionIntentsAreDurableBeforeTheFirstCreate(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, galleryResult("https://cdn.example/1.jpg", "https://cdn.example/2.jpg")
	}
	rid := replyData(btResolve(t, s, "admin", "pan-share:abc"))["id"].(string)
	f.onCreate = func() {
		st := state(t, s)
		marked := 0
		for key, job := range st.Resolved {
			if job.SubmissionRID == key && job.Requests == nil {
				marked++
			}
		}
		if marked+len(st.Jobs) != 2 {
			t.Errorf("intent not durable before create: %+v", st)
		}
	}
	btCreate(t, s, "admin", rid)
	f.onCreate = nil
	before := len(f.creates)
	if out := btCreate(t, s, "admin", rid); out["success"] != true || len(f.creates) != before {
		t.Fatalf("retry reposted: %v, %d creates", out, len(f.creates))
	}
}

func TestSingleExtensionFileKeepsTheChosenName(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) { return 0, galleryResult("https://cdn.example/1.jpg") }
	body, _ := json.Marshal(map[string]any{"req": map[string]any{"url": "pan-share:abc"}, "opts": map[string]any{"path": "/home", "name": "photo.jpg"}})
	out, _ := call(t, s, "admin", "test.resolve", json.RawMessage(body))
	rid := replyData(out)["id"].(string)
	if out := btCreate(t, s, "admin", rid); out["success"] != true || f.creates[0]["opts"].(map[string]any)["name"] != "photo.jpg" {
		t.Fatalf("%v %v", out, f.creates)
	}
}

func TestExtensionRequestsKeepTheLocalAndPrivateFloor(t *testing.T) {
	for _, bad := range []string{"file:///etc/passwd", "http://127.0.0.1:19893/api/v1/config", "/var/packages/gopeed/var/x.torrent", "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"} {
		s, f := newEngineFixture(t)
		f.resolve = func(string) (int, any) { return 0, galleryResult("https://cdn.example/1.jpg", bad) }
		if out := btResolve(t, s, "admin", "pan-share:abc"); errorCode(out) != "url_denied" {
			t.Fatalf("%s: %v", bad, out)
		}
		if len(state(t, s).Resolved) != 0 {
			t.Fatalf("%s kept a preview", bad)
		}
	}
}

func TestExtensionFilesMayNotBeTakenByAnotherUser(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) { return 0, galleryResult("https://cdn.example/1.jpg") }
	rid := replyData(btResolve(t, s, "admin", "pan-share:abc"))["id"].(string)
	if out := btCreate(t, s, "normal1", rid); errorCode(out) != "task_denied" || len(f.creates) != 0 {
		t.Fatal(out)
	}
}
