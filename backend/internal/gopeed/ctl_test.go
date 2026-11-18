package gopeed

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCtlSourcesProjectOnlyNonCredentialFields(t *testing.T) {
	s := searchFixture(t)
	out, _ := call(t, s, "normal1", "search.sources.save", map[string]any{
		"kind": "rss", "name": "https://private.example/synthetic-name-secret",
		"url":     "https://example.org/shortsecret/feed?custom=synthetic-query-secret",
		"enabled": true, "values": map[string]string{"token": "synthetic-token-secret"},
	})
	if out["success"] != true {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal1", "search.sources", map[string]any{})
	if out["success"] != true {
		t.Fatal(out)
	}
	rows := out["data"].([]any)
	if len(rows) != 1 {
		t.Fatal(out)
	}
	row := rows[0].(map[string]any)
	if len(row) != 5 || row["kind"] != "rss" || row["enabled"] != true || row["configured"] != true {
		t.Fatal("unexpected source projection", out)
	}
	for _, field := range []string{"id", "kind", "enabled", "configured", "supported"} {
		if _, ok := row[field]; !ok {
			t.Fatal("missing source field", field)
		}
	}
	raw, _ := json.Marshal(out)
	for _, sensitive := range []string{"secret", "url", "name", "values", "settings"} {
		if strings.Contains(string(raw), sensitive) {
			t.Fatal("source details leaked")
		}
	}
	for _, user := range []string{"normal2", "admin"} {
		out, _ = call(t, s, user, "search.sources", map[string]any{})
		if out["success"] != true || len(out["data"].([]any)) != 0 {
			t.Fatal("foreign sources leaked", user)
		}
	}
}

func TestCtlTasksProjectOnlySafeFieldsAndEnforceOwner(t *testing.T) {
	s, _, _ := fixture(t)
	s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["mine"] = Job{Owner: "uid:normal1", Filename: "https://private.example/file?passkey=secret", Preparation: &Preparation{URL: "https://private.example/file?passkey=secret", Status: "queued"}}
		st.Jobs["other"] = Job{Owner: "uid:normal2"}
		return nil, nil
	})
	out, _ := call(t, s, "normal1", "tasks.list", map[string]any{})
	if out["success"] != true {
		t.Fatal(out)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "secret") || strings.Contains(string(b), "private.example") || strings.Contains(string(b), "other") {
		t.Fatal("source/owner leaked", string(b))
	}
	admin, _ := call(t, s, "admin", "tasks.list", map[string]any{})
	if admin["success"] != true || len(admin["data"].(map[string]any)["tasks"].([]any)) != 0 {
		t.Fatal("administrator saw other owners' tasks", admin)
	}
	for _, method := range []string{"tasks.pause", "tasks.resume"} {
		for _, other := range []string{"normal2", "admin"} {
			out, _ = call(t, s, other, method, map[string]any{"id": "mine"})
			if out["success"] != false {
				t.Fatal("foreign task allowed", out)
			}
		}
		out, _ = call(t, s, "normal1", method, map[string]any{"id": "mine"})
		if out["success"] != true {
			t.Fatal(out)
		}
	}
	for _, params := range []map[string]any{{"id": "mine/../other"}, {"id": "mine", "path": "/api/v1/tasks/other"}} {
		out, _ = call(t, s, "normal1", "tasks.pause", params)
		if out["success"] != false {
			t.Fatal(out)
		}
	}
}

func TestCtlAddUsesCurrentGrantDeduplicatesAndWaitsOnlyForPreparation(t *testing.T) {
	s, f := newEngineFixture(t)
	f.resolve = func(string) (int, any) {
		return 0, map[string]any{"id": "rid-file", "res": map[string]any{"files": []any{map[string]any{"name": "file.bin", "size": 8}}}}
	}
	params := map[string]any{"key": "ctl-test-0001", "url": "https://example.com/file.bin"}
	first, _ := call(t, s, "normal1", "downloads.add", params)
	if first["success"] != true || first["data"].(map[string]any)["accepted"] != true || len(f.resolves) != 0 {
		t.Fatal(first)
	}
	again, _ := call(t, s, "normal1", "downloads.add", params)
	if again["success"] != true || len(state(t, s).Jobs) != 1 {
		t.Fatal(again)
	}
	poll := func(user string) map[string]any {
		out, _ := call(t, s, user, "downloads.status", map[string]any{"job_id": "ctl-test-0001"})
		return out
	}
	if poll("normal2")["success"] != false || poll("normal1")["data"].(map[string]any)["state"] != "pending" {
		t.Fatal("owner/pending contract")
	}
	for id := range state(t, s).Jobs {
		processPreparation(t, s, id)
	}
	result := poll("normal1")
	if result["success"] != true || result["data"].(map[string]any)["state"] != "prepared" || result["data"].(map[string]any)["completion"] != "not_checked" {
		t.Fatal(result)
	}
	s.DestinationDelegate = nil
	out, _ := call(t, s, "normal1", "downloads.add", map[string]any{"key": "ctl-test-0002", "url": "https://example.com/file.bin"})
	if out["success"] != false || len(state(t, s).Jobs) != 1 {
		t.Fatal("missing grant accepted", out)
	}
}

func TestCtlTorrentAdmissionValidatesMetainfoWithoutReturningTrackers(t *testing.T) {
	s, f := newBTFixture(t)
	params := map[string]any{"key": "ctl-torrent-0001", "torrent": strings.TrimPrefix(torrentDataURI(), torrentDataPrefix)}
	out, _ := call(t, s, "normal1", "downloads.add_torrent", params)
	if out["success"] != true || len(f.resolves) != 0 || f.posts != 0 {
		t.Fatal(out)
	}
	b, _ := json.Marshal(out)
	if strings.Contains(string(b), "torrent\"") || strings.Contains(string(b), "data:") {
		t.Fatal("metainfo echoed", string(b))
	}
	for _, value := range []string{"/volume1/private/file.torrent", "!!!", "ZGU="} {
		out, _ = call(t, s, "normal1", "downloads.add_torrent", map[string]any{"key": "ctl-torrent-0002", "torrent": value})
		if out["success"] != false || len(state(t, s).Jobs) != 1 {
			t.Fatal("invalid metainfo accepted", out)
		}
	}
}

func TestCtlStatusReportsFailedPreparationAndRejectsUnknownParams(t *testing.T) {
	s, _ := newEngineFixture(t)
	for _, params := range []map[string]any{{"key": "bad", "url": "https://example.com/a"}, {"key": "ctl-test-0001", "url": "https://example.com/a", "owner": "normal2"}} {
		out, _ := call(t, s, "normal1", "downloads.add", params)
		if out["success"] != false {
			t.Fatal(out)
		}
	}
	s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["failed"] = Job{Owner: "uid:normal1", BatchKey: "ctl-test-0001", Preparation: &Preparation{Status: "error", Error: "source_unreachable"}}
		return nil, nil
	})
	out, _ := call(t, s, "normal1", "downloads.status", map[string]any{"job_id": "ctl-test-0001"})
	if out["success"] != true || out["data"].(map[string]any)["state"] != "failed" {
		t.Fatal(out)
	}
}

func TestCtlSourceConfigurationAndOwnerBoundMaskedExport(t *testing.T) {
	s := searchFixture(t)
	out, _ := call(t, s, "normal1", "search.sources.save", map[string]any{"kind": "rss", "name": "Feed", "url": "https://example.org/feed", "enabled": true})
	id := out["data"].(map[string]any)["id"].(string)
	for _, key := range []string{"apikey", "passkey", "cookie", "customCredential"} {
		out, _ = call(t, s, "normal1", "search.sources.field", map[string]any{"id": id, "field": key, "value": "synthetic-secret"})
		if out["success"] != true {
			t.Fatal(out)
		}
	}
	for _, user := range []string{"normal2", "admin"} {
		for _, method := range []string{"search.sources.field", "search.sources.config"} {
			p := map[string]any{"id": id}
			if method == "search.sources.field" {
				p["field"] = "cookie"
				p["value"] = "no"
			}
			out, _ = call(t, s, user, method, p)
			if out["success"] == true {
				t.Fatal("foreign source", user, method)
			}
		}
	}
	out, _ = call(t, s, "normal1", "search.sources.config", map[string]any{"id": id})
	raw, _ := json.Marshal(out)
	if out["success"] != true || strings.Contains(string(raw), "synthetic-secret") || !strings.Contains(string(raw), "***") {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal1", "search.solver.set", map[string]any{"url": "http://solver.lan:8191"})
	if out["success"] != true {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal2", "search.solver.get", map[string]any{})
	if out["success"] != true || out["data"].(map[string]any)["url"] != "" {
		t.Fatal("foreign setting", out)
	}
}

func TestCtlTaskGetProgressAndDeletionReuseOwnerPipeline(t *testing.T) {
	s, _ := newEngineFixture(t)
	s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["mine"] = Job{Owner: "uid:normal1", Preparation: &Preparation{URL: "https://private.example/secret", Status: "queued"}}
		return nil, nil
	})
	for _, user := range []string{"normal2", "admin"} {
		for _, method := range []string{"tasks.get", "tasks.delete", "tasks.removal"} {
			out, _ := call(t, s, user, method, map[string]any{"id": "mine"})
			if out["success"] != false {
				t.Fatal("foreign task", user, method, out)
			}
		}
	}
	out, _ := call(t, s, "normal1", "tasks.get", map[string]any{"id": "mine"})
	if out["success"] != true {
		t.Fatal(out)
	}
	row := out["data"].(map[string]any)
	if row["status"] != "preparing" || row["downloaded"] != nil || row["total"] != nil {
		t.Fatal("unknown progress must remain null", out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "secret") {
		t.Fatal("source leaked", out)
	}
	out, _ = call(t, s, "normal1", "tasks.delete", map[string]any{"id": "mine"})
	if out["success"] != true || out["data"].(map[string]any)["deleting"] != true || !state(t, s).Jobs["mine"].DeleteRequested {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal1", "tasks.removal", map[string]any{"id": "mine"})
	if out["success"] != true || out["data"].(map[string]any)["state"] != "deleting" {
		t.Fatal(out)
	}
	s.withState(context.Background(), func(st *State) (any, error) {
		delete(st.Jobs, "mine")
		st.Removals = append(st.Removals, Removal{ID: "mine", Owner: "uid:normal1", FileKept: true})
		return nil, nil
	})
	out, _ = call(t, s, "normal1", "tasks.removal", map[string]any{"id": "mine"})
	if out["success"] != true || out["data"].(map[string]any)["state"] != "removed" || out["data"].(map[string]any)["fileKept"] != true {
		t.Fatal(out)
	}
	for _, method := range []string{"tasks.get", "tasks.delete", "tasks.removal"} {
		for _, params := range []map[string]any{{"id": "../mine"}, {"id": "mine", "owner": "normal2"}} {
			out, _ = call(t, s, "normal1", method, params)
			if out["success"] != false {
				t.Fatal("invalid request", out)
			}
		}
	}
}

func TestCtlTaskNumericProgressNeverReturnsRawMetadata(t *testing.T) {
	s, f := newEngineFixture(t)
	s.withState(context.Background(), func(st *State) (any, error) { st.Jobs["progress"] = Job{Owner: "uid:normal1"}; return nil, nil })
	f.tasks = []map[string]any{{"id": "progress", "status": "running", "meta": map[string]any{"req": map[string]any{"url": "https://private/secret"}, "res": map[string]any{"size": 100}}, "progress": map[string]any{"downloaded": 25, "speed": 5, "secret": "hidden"}}}
	out, _ := call(t, s, "normal1", "tasks.get", map[string]any{"id": "progress"})
	if out["success"] != true {
		t.Fatal(out)
	}
	row := out["data"].(map[string]any)
	if row["downloaded"] != float64(25) || row["total"] != float64(100) || row["speed"] != float64(5) {
		t.Fatal(out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "secret") || strings.Contains(string(raw), "hidden") {
		t.Fatal(out)
	}
}
