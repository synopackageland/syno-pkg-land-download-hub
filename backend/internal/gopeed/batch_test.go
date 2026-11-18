package gopeed

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

type batchItem struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

func createMany(t *testing.T, s *Service, user string, items ...batchItem) []map[string]any {
	t.Helper()
	out, _ := call(t, s, user, "downloads.create_many", map[string]any{"path": "/home", "items": items})
	if out["success"] != true {
		t.Fatal(out)
	}
	return batchResults(t, out)
}

func batchResults(t *testing.T, out map[string]any) []map[string]any {
	t.Helper()
	raw, _ := out["data"].(map[string]any)["results"].([]any)
	results := make([]map[string]any, len(raw))
	for i, r := range raw {
		results[i] = r.(map[string]any)
	}
	return results
}

func item(n int, url string) batchItem { return batchItem{Key: fmt.Sprintf("key-%04d", n), URL: url} }

func processPreparation(t *testing.T, s *Service, id string) {
	t.Helper()
	j := state(t, s).Jobs[id]
	(&PreparationRunner{Service: s}).prepare(context.Background(), id, j)
}
func admissionID(r map[string]any) string { return r["ids"].([]any)[0].(string) }

func TestCreateManyQueuesEachLinkAndReportsResolutionFailureInTheList(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(u string) (int, any) {
		if strings.Contains(u, "broken") {
			return 1, nil
		}
		return 0, map[string]any{"id": "rid-file", "res": map[string]any{"files": []any{map[string]any{"name": "file.bin", "size": 8}}}}
	}
	result := createMany(t, s, "alice", item(1, "https://example.com/file.bin"), item(2, "file:///etc/passwd"), item(3, "https://example.com/broken.bin"))
	if result[0]["status"] != "created" || result[1]["status"] != "failed" || result[2]["status"] != "created" || len(f.resolves) != 0 {
		t.Fatal(result, f.resolves)
	}
	id := admissionID(result[0])
	processPreparation(t, s, id)
	if j := state(t, s).Jobs["task-1"]; j.Owner != "uid:alice" || j.BatchKey != "key-0001" || j.Filename != "file.bin" {
		t.Fatal(j)
	}
	id = admissionID(result[2])
	processPreparation(t, s, id)
	if j := state(t, s).Jobs[id]; j.Preparation == nil || j.Preparation.Status != "error" || j.Preparation.Error != "source_unreachable" {
		t.Fatal(j)
	}
}
func TestCreateManyRepeatedKeyAnswersWithSameAdmissionThenAcceptedTask(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, map[string]any{"id": "rid-file", "res": map[string]any{"files": []any{map[string]any{"name": "file.bin", "size": 8}}}}
	}
	first := createMany(t, s, "alice", item(1, "https://example.com/file.bin"))
	again := createMany(t, s, "alice", item(1, "https://example.com/file.bin"))
	if fmt.Sprint(first) != fmt.Sprint(again) || len(f.resolves) != 0 {
		t.Fatal(first, again)
	}
	processPreparation(t, s, admissionID(first[0]))
	again = createMany(t, s, "alice", item(1, "https://example.com/file.bin"))
	if admissionID(again[0]) != "task-1" || len(f.creates) != 1 {
		t.Fatal(again, f.creates)
	}
	other := createMany(t, s, "bob", item(1, "https://example.com/file.bin"))
	if admissionID(other[0]) == "task-1" {
		t.Fatal(other)
	}
}
func TestCreateManyMagnetPreparationSurvivesMetadataWait(t *testing.T) {
	s, f := newBTFixture(t)
	s.metadataWait = 50 * time.Millisecond
	f.stall = make(chan struct{})
	result := createMany(t, s, "admin", item(1, "magnet:?xt=urn:btih:"+f.hash))
	id := admissionID(result[0])
	if result[0]["status"] != "created" || f.posts != 0 {
		t.Fatal(result)
	}
	processPreparation(t, s, id)
	if state(t, s).Jobs[id].Preparation.Status != "queued" {
		t.Fatal(state(t, s))
	}
	close(f.stall)
	processPreparation(t, s, id)
	if j := state(t, s).Jobs["bt-task"]; j.FileCount != 2 || j.BatchKey != "key-0001" || f.posts != 1 {
		t.Fatal(j, f.posts)
	}
}
func TestCreateManyMediaProbeRunsOnlyInDaemon(t *testing.T) {
	s, runs := mediaFixture(t, bunny, "")
	result := createMany(t, s, "alice", item(1, "https://www.youtube.com/watch?v=1"))
	id := admissionID(result[0])
	if *runs != 0 {
		t.Fatal("inline media probe")
	}
	processPreparation(t, s, id)
	st := state(t, s)
	if len(st.Jobs) != 1 {
		t.Fatal(st)
	}
	for _, j := range st.Jobs {
		if j.Media == nil || j.Media.Format != "best" || j.Media.Status != "queued" {
			t.Fatal(j)
		}
	}
}
func TestCreateManyAdmitsAllLinksWithoutSourceBudget(t *testing.T) {
	s, f := newEngineFixture(t)
	results := createMany(t, s, "alice", item(1, "https://example.com/a"), item(2, "https://example.com/b"))
	if results[0]["status"] != "created" || results[1]["status"] != "created" || len(f.resolves) != 0 {
		t.Fatal(results)
	}
}

func TestCreateManyRefusesMalformedBatches(t *testing.T) {
	s, f := newEngineFixture(t)
	many := make([]batchItem, maxBatchItems+1)
	for i := range many {
		many[i] = item(i, "https://example.com/file.bin")
	}
	for name, p := range map[string]any{
		"empty":     map[string]any{"path": "/home", "items": []batchItem{}},
		"too many":  map[string]any{"path": "/home", "items": many},
		"duplicate": map[string]any{"path": "/home", "items": []batchItem{item(1, "https://a.example/x"), item(1, "https://b.example/y")}},
		"bad key":   map[string]any{"path": "/home", "items": []batchItem{{Key: "../x", URL: "https://a.example/x"}}},
		"no url":    map[string]any{"path": "/home", "items": []batchItem{{Key: "key-0001"}}},
		"extra":     map[string]any{"path": "/home", "items": []batchItem{item(1, "https://a.example/x")}, "name": "x"},
	} {
		if out, _ := call(t, s, "alice", "downloads.create_many", p); out["success"] != false || errorCode(out) != "invalid_request" {
			t.Fatal(name, out)
		}
	}
	if len(f.resolves) != 0 {
		t.Fatal("malformed batch reached the engine")
	}
}

func TestSingleSubmissionRoutesAreGone(t *testing.T) {
	s, f := newEngineFixture(t)
	body := json.RawMessage(`{"req":{"url":"https://example.com/file.bin"},"opts":{"path":"/home"}}`)
	for _, path := range []string{"/api/v1/resolve", "/api/v1/tasks"} {
		if out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "POST", Path: path, Data: body}); out["success"] != false {
			t.Fatal(path, out)
		}
	}
	for _, method := range []string{"gopeed.media.probe", "gopeed.media.create"} {
		if out, _ := call(t, s, "alice", method, map[string]string{"url": "https://example.com/v", "path": "/home"}); out["success"] != false {
			t.Fatal(method, out)
		}
	}
	if len(f.resolves)+len(f.creates) != 0 {
		t.Fatal("removed route reached the engine")
	}
}

func TestClassifyWithoutToolNamesProtocolsAndExtensions(t *testing.T) {
	s, f := newEngineFixture(t)
	f.extensions = []any{
		map[string]any{"identity": "pan", "disabled": false, "scripts": []any{map[string]any{"event": "onResolve", "match": map[string]any{"urls": []any{"*://*.pan.example/*"}}}}},
		map[string]any{"identity": "off", "disabled": true, "scripts": []any{map[string]any{"event": "onResolve", "match": map[string]any{"urls": []any{"*://off.example/*"}}}}},
	}
	urls := map[string]string{
		"https://example.com/a.zip":                      "file",
		"https://example.com/a.torrent":                  "torrent",
		"magnet:?xt=urn:btih:" + strings.Repeat("a", 40): "torrent",
		ed2kLink:                            "ed2k",
		"https://www.youtube.com/watch?v=1": "file",
		"https://share.pan.example/s/1":     "extension",
		"https://off.example/s/1":           "file",
		"http://127.0.0.1:5000/a.zip":       "blocked",
		"file:///etc/passwd":                "unsupported",
		"not a link":                        "unsupported",
	}
	list := []string{}
	for u := range urls {
		list = append(list, u)
	}
	out, _ := call(t, s, "alice", "downloads.classify", map[string]any{"urls": list})
	if out["success"] != true {
		t.Fatal(out)
	}
	results := batchResults(t, out)
	for i, u := range list {
		if results[i]["url"] != u || results[i]["kind"] != urls[u] {
			t.Fatalf("%s: %v", u, results[i])
		}
	}
	if len(f.resolves)+len(f.creates) != 0 {
		t.Fatal("classify resolved a link")
	}
}
