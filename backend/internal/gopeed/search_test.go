package gopeed

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsmapp/internal/search"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
)

func searchFixture(t *testing.T) *Service {
	s, _, _ := fixture(t)
	s.SearchIdentity = func(name string) (background.Submitter, error) {
		uid := "1201"
		if name == "normal2" {
			uid = "1202"
		}
		if name == "admin" {
			uid = "1203"
		}
		return background.Submitter{User: name, UID: uid}, nil
	}
	s.Search = &search.Service{Data: filepath.Join(s.Data, "search"), Run: func(_ context.Context, in search.Request) (search.Reply, error) {
		if in.Op == "validate" {
			return search.Reply{Sites: []search.Site{{ID: "test", Name: "Test", Supported: true}}}, nil
		}
		return search.Reply{Results: []search.Hit{{Title: "Ubuntu", Link: "https://example.org/test.torrent"}}}, nil
	}}
	s.Search.Stream = func(ctx context.Context, in search.Request, emit func(int, search.Reply) error) error {
		rep, err := s.Search.Run(ctx, search.Request{Op: "batch", Searches: in.Searches})
		if err != nil {
			return err
		}
		for i := range in.Searches {
			item := rep
			if len(rep.Batch) > i {
				item = rep.Batch[i]
			}
			if err := emit(i, item); err != nil {
				return err
			}
		}
		return nil
	}
	return s
}
func TestSearchAPIUserIsolationAndPermissions(t *testing.T) {
	s := searchFixture(t)
	out, _ := call(t, s, "normal1", "search.sources.save", map[string]any{"kind": "rss", "name": "Mine", "url": "https://example.org/feed?private=secret", "enabled": true, "values": map[string]string{"cookie": "secret"}})
	if out["success"] != true {
		t.Fatal(out)
	}
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), "secret") {
		t.Fatal("credential leak", out)
	}
	id := out["data"].(map[string]any)["id"].(string)
	out, _ = call(t, s, "normal2", "search.sources.list", map[string]any{})
	if len(out["data"].([]any)) != 0 {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal2", "search.jobs.start", map[string]any{"key": "isolation", "sourceIds": []string{id}, "query": "ubuntu", "page": 1})
	if out["success"] == true {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal1", "search.jobs.start", map[string]any{"key": "isolation", "sourceIds": []string{id}, "query": "ubuntu", "page": 1})
	if out["success"] != true {
		t.Fatal(out)
	}
	jobID := out["data"].(map[string]any)["job"].(map[string]any)["id"].(string)
	runSearchJob(t, s)
	out, _ = call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": jobID, "offset": 0, "limit": 1})
	encoded, _ = json.Marshal(out)
	if strings.Contains(string(encoded), "example.org") {
		t.Fatal("raw result link exposed", out)
	}
	for _, method := range []string{"search.definitions.update", "search.definitions.rollback"} {
		out, _ = call(t, s, "normal1", method, map[string]any{})
		if out["success"] == true {
			t.Fatal("non-admin global mutation", out)
		}
	}
	out, _ = call(t, s, "normal1", "search.sources.enabled", map[string]any{"id": id, "enabled": false})
	if out["success"] != true {
		t.Fatal(out)
	}
	jobID = startSearchJob(t, s, id, "disabled-source")
	runSearchJob(t, s)
	out, _ = call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": jobID, "offset": 0, "limit": 1})
	if out["data"].(map[string]any)["items"].([]any)[0].(map[string]any)["error"].(map[string]any)["code"] != "search_source_disabled" {
		t.Fatal(out)
	}
}
func TestSearchVideoPaginationAndCanonicalIdentity(t *testing.T) {
	s, _ := mediaFixture(t, `{"id":"abc","title":"Fixture","url":"https://www.youtube.com/watch?v=abc"}`+"\n", "")
	rep, err := s.searchVideo(context.Background(), search.Request{Kind: "ytsearch", Query: "fixture", Page: 2})
	if err != nil || len(rep.Results) != 1 {
		t.Fatalf("%+v %v", rep, err)
	}
	if videoIdentity("https://youtu.be/abc") != videoIdentity("https://www.youtube.com/watch?v=abc&feature=test") {
		t.Fatal("video identity differs")
	}
}

func TestSearchTaskPresentationHidesTrackerCredentials(t *testing.T) {
	s := searchFixture(t)
	task := map[string]any{"meta": map[string]any{"req": map[string]any{"url": "data:torrent-with-passkey"}, "res": map[string]any{"name": "Ubuntu", "extra": map[string]any{"tracker": "secret"}}}}
	s.present(task, Job{BatchKey: "search-any"})
	raw, _ := json.Marshal(task)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "passkey") {
		t.Fatal(string(raw))
	}
}

func TestSearchDownloadAdmitsWithoutResolvingAndDeduplicates(t *testing.T) {
	s, f := newBTFixture(t)
	s.Search = &search.Service{Data: filepath.Join(s.Data, "search"), Run: func(_ context.Context, in search.Request) (search.Reply, error) {
		if in.Op == "search" {
			return search.Reply{Results: []search.Hit{{Title: "Ubuntu", Link: "magnet:?xt=urn:btih:" + f.hash}}}, nil
		}
		t.Fatal("click performed tracker work", in.Op)
		return search.Reply{}, nil
	}}
	src, err := s.Search.SaveSource(context.Background(), "uid:admin", search.SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Search.Search(context.Background(), "uid:admin", search.Query{SourceID: src.ID, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := call(t, s, "admin", "search.download", map[string]string{"resultId": page.Results[0].ResultID})
	if first["success"] != true {
		t.Fatal(first)
	}
	rows := batchResults(t, first)
	if len(rows) != 1 || rows[0]["status"] != "created" || len(rows[0]["ids"].([]any)) != 1 {
		t.Fatal(first)
	}
	if f.posts != 0 {
		t.Fatal("click resolved and created torrent instead of returning its preparation task")
	}
	again, _ := call(t, s, "admin", "search.download", map[string]string{"resultId": page.Results[0].ResultID})
	b1, _ := json.Marshal(rows[0]["ids"])
	b2, _ := json.Marshal(batchResults(t, again)[0]["ids"])
	if string(b1) != string(b2) {
		t.Fatal("repeated result made duplicate tasks", first, again)
	}
	denied, _ := call(t, s, "normal1", "search.download", map[string]string{"resultId": page.Results[0].ResultID})
	if denied["success"] == true {
		t.Fatal("result crossed owners")
	}
}

func TestSearchTorznabWorkerHandsTorrentBytesToExistingEngineWithoutKeyLeak(t *testing.T) {
	s, f := newBTFixture(t)
	secret := "PRIVATE-TORZNAB-APIKEY"
	fetches := 0
	s.Search = &search.Service{Data: filepath.Join(s.Data, "search"), Run: func(_ context.Context, in search.Request) (search.Reply, error) {
		if in.Op == "search" {
			return search.Reply{Results: []search.Hit{{Title: "Ubuntu", Link: "https://jackett.example/dl?apikey=" + secret, Hash: f.hash}}}, nil
		}
		if in.Op != "download" || in.Values["apikey"] != secret || strings.Contains(in.URL, secret) || !strings.Contains(in.URL, "apikey=***") {
			t.Fatalf("unsafe helper invocation %+v", in)
		}
		fetches++
		raw, _ := fixtureTorrent()
		return search.Reply{Torrent: base64.StdEncoding.EncodeToString([]byte(raw))}, nil
	}}
	src, err := s.Search.SaveSource(context.Background(), "uid:admin", search.SourceInput{Kind: "torznab", Name: "Jackett", URL: "https://jackett.example/api/v2.0/indexers/all/results/torznab", Values: map[string]string{"apikey": secret}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	page, err := s.Search.Search(context.Background(), "uid:admin", search.Query{SourceID: src.ID, Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := call(t, s, "admin", "search.download", map[string]string{"resultId": page.Results[0].ResultID})
	rows := batchResults(t, out)
	id := admissionID(rows[0])
	if fetches != 0 || f.posts != 0 {
		t.Fatal("click waited for a source")
	}
	processPreparation(t, s, id)
	if fetches != 1 || f.posts != 1 || len(f.resolves) != 1 {
		t.Fatalf("worker did not use existing intake: fetches=%d posts=%d journal=%+v", fetches, f.posts, state(t, s))
	}
	raw, _ := json.Marshal(f.resolves)
	if strings.Contains(string(raw), secret) || strings.Contains(string(raw), "jackett.example") {
		t.Fatal("authenticated URL reached engine")
	}
	raw, _ = os.ReadFile(filepath.Join(s.Data, "jobs.json"))
	if strings.Contains(string(raw), secret) {
		t.Fatal("download journal leaked key")
	}
	again, _ := call(t, s, "admin", "search.download", map[string]string{"resultId": page.Results[0].ResultID})
	ids := batchResults(t, again)[0]["ids"].([]any)
	if len(ids) != 1 || ids[0] != "bt-task" || f.posts != 1 {
		t.Fatal("duplicate did not return existing task", again)
	}
}

func TestSearchTorrentWithoutAdvertisedHashReusesExistingTaskAfterPrivateFetch(t *testing.T) {
	s, f := newBTFixture(t)
	searches := 0
	s.Search = &search.Service{Data: filepath.Join(s.Data, "search"), Run: func(_ context.Context, in search.Request) (search.Reply, error) {
		if in.Op == "search" {
			searches++
			return search.Reply{Results: []search.Hit{{Title: "Same torrent", Link: fmt.Sprintf("https://jackett.example/dl/%d?apikey=secret", searches)}}}, nil
		}
		raw, _ := fixtureTorrent()
		return search.Reply{Torrent: base64.StdEncoding.EncodeToString([]byte(raw))}, nil
	}}
	src, err := s.Search.SaveSource(context.Background(), "uid:admin", search.SourceInput{Kind: "torznab", Name: "Jackett", URL: "https://jackett.example/api", Values: map[string]string{"apikey": "secret"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		page, err := s.Search.Search(context.Background(), "uid:admin", search.Query{SourceID: src.ID, Page: 1})
		if err != nil {
			t.Fatal(err)
		}
		out, _ := call(t, s, "admin", "search.download", map[string]string{"resultId": page.Results[0].ResultID})
		id := admissionID(batchResults(t, out)[0])
		processPreparation(t, s, id)
		if _, exists := state(t, s).Jobs[id]; exists {
			t.Fatalf("duplicate left failed preparation: %+v", state(t, s).Jobs[id])
		}
	}
	if f.posts != 1 {
		t.Fatal("torrent was submitted twice", f.posts)
	}
}

// search.batch answers each of the caller's sources on its own; another
// user's source is missing for them.
func TestSearchBatchRouteAnswersEachOwnSource(t *testing.T) {
	s := searchFixture(t)
	s.Search.Run = func(_ context.Context, in search.Request) (search.Reply, error) {
		if in.Op == "validate" {
			return search.Reply{Sites: []search.Site{{ID: "test", Name: "Test", Supported: true}}}, nil
		}
		out := search.Reply{}
		for range in.Searches {
			out.Batch = append(out.Batch, search.Reply{Results: []search.Hit{{Title: "Ubuntu", Link: "https://example.org/test.torrent"}}})
		}
		return out, nil
	}
	mine, _ := call(t, s, "normal1", "search.sources.save", map[string]any{"kind": "rss", "name": "Mine", "url": "https://example.org/feed", "enabled": true})
	theirs, _ := call(t, s, "normal2", "search.sources.save", map[string]any{"kind": "rss", "name": "Theirs", "url": "https://example.org/feed", "enabled": true})
	ids := []string{mine["data"].(map[string]any)["id"].(string), theirs["data"].(map[string]any)["id"].(string)}
	out, _ := call(t, s, "normal1", "search.jobs.start", map[string]any{"key": "foreign-batch", "sourceIds": ids, "query": "ubuntu", "page": 1})
	if out["success"] != false {
		t.Fatal("foreign source admitted", out)
	}
	id := startSearchJob(t, s, ids[0], "own-batch")
	runSearchJob(t, s)
	out, _ = call(t, s, "normal1", "search.jobs.pages", map[string]any{"id": id, "offset": 0, "limit": 1})
	first := out["data"].(map[string]any)["items"].([]any)[0].(map[string]any)
	if len(first["results"].([]any)) != 1 || first["error"] != nil {
		t.Fatal(out)
	}
	encoded, _ := json.Marshal(out)
	if strings.Contains(string(encoded), "example.org") {
		t.Fatal("raw result link exposed", out)
	}
}
