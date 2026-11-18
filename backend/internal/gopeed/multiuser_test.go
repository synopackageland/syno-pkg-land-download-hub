package gopeed

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThreeUsersCannotInspectOrMutateForeignDownloads(t *testing.T) {
	s, files, calls := fixture(t)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		for _, user := range []string{"admin", "normal1", "normal2"} {
			id := "mine"
			if user != "normal1" {
				id = "other" + user
			}
			st.Jobs[id] = Job{Owner: "uid:" + user, Destination: "/home/" + user, Filename: user + ".bin", Published: true}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"admin", "normal1", "normal2"} {
		own := "mine"
		if user != "normal1" {
			own = "other" + user
		}
		for _, foreign := range []string{"mine", "otheradmin", "othernormal2"} {
			if foreign == own {
				continue
			}
			before := *calls
			for _, in := range []Input{
				{Method: "GET", Path: "/api/v1/tasks/" + foreign},
				{Method: "GET", Path: "/api/v1/tasks/" + foreign + "/stats"},
				{Method: "PUT", Path: "/api/v1/tasks/" + foreign + "/pause"},
				{Method: "PUT", Path: "/api/v1/tasks/" + foreign + "/continue"},
				{Method: "DELETE", Path: "/api/v1/tasks/" + foreign},
			} {
				out, _ := call(t, s, user, "gopeed.request", in)
				if out["success"] != false {
					t.Fatal(user, foreign, out)
				}
			}
			for _, method := range []string{"gopeed.publish", "gopeed.reveal"} {
				out, _ := call(t, s, user, method, map[string]string{"id": foreign})
				if out["success"] != false {
					t.Fatal(user, foreign, out)
				}
			}
			if *calls != before || files.uploaded != "" {
				t.Fatal("foreign action reached engine or writer")
			}
		}
	}
	out, _ := call(t, s, "normal1", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	raw, _ := json.Marshal(out)
	if out["success"] != true || !strings.Contains(string(raw), "normal1.bin") || strings.Contains(string(raw), "other") {
		t.Fatal(string(raw))
	}
	for _, user := range []string{"normal1", "normal2"} {
		for _, method := range []string{"gopeed.settings.get", "gopeed.settings.set", "gopeed.plugins.list", "gopeed.plugins.install", "binaries.status", "binaries.update"} {
			out, _ := call(t, s, user, method, map[string]any{})
			if out["success"] != false || out["error"].(map[string]any)["code"] != "admin_required" {
				t.Fatal(method, out)
			}
		}
	}
}

func TestPersonalConfigurationDoesNotExposeGlobalEngineSettings(t *testing.T) {
	s, _, calls := fixture(t)
	_, _ = s.withState(context.Background(), func(st *State) (any, error) { st.Folders["uid:normal1"] = "/home/Downloads"; return nil, nil })
	out, _ := call(t, s, "normal1", "gopeed.request", Input{Method: "GET", Path: "/api/v1/config"})
	raw, _ := json.Marshal(out)
	if out["success"] != true || *calls != 0 || !strings.Contains(string(raw), "/home/Downloads") || strings.Contains(string(raw), "status") {
		t.Fatal(out, *calls)
	}
}

func TestNewDownloadsNeverUseSessionOnlyDestination(t *testing.T) {
	s, f, calls := fixture(t)
	for _, user := range []string{"admin", "normal1", "normal2"} {
		out, _ := call(t, s, user, "test.resolve", json.RawMessage(`{"req":{"url":"https://example.com/file.bin"},"opts":{"path":"/home"}}`))
		if out["success"] != false || *calls != 0 || f.uploaded != "" {
			t.Fatal(out, *calls)
		}
	}
}

func TestUpgrade021PreservesAdminSevenHistoriesSettingsAndUncertainWork(t *testing.T) {
	s, _, _ := fixture(t)
	// Exact pre-delegate shape: UID:username owner, no grants/save fields.
	jobs := map[string]any{}
	for i, id := range []string{"history1", "history2", "history3", "history4", "history5", "history6", "history7", "mine"} {
		jobs[id] = map[string]any{"owner": "uid:admin", "stage": filepath.Join(s.Data, "staging", id), "destination": "/home/Downloads", "filename": id + ".bin", "published": i < 7, "uncertain": id == "mine"}
	}
	raw, _ := json.Marshal(map[string]any{"jobs": jobs, "resolved": map[string]any{}, "folders": map[string]string{"uid:admin": "/home/Downloads"}})
	path := filepath.Join(s.Data, "jobs.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	global := []byte(`{"maxRunning":3,"connections":8,"extensions":["original"]}`)
	os.WriteFile(filepath.Join(s.Data, "settings.json"), global, 0600)
	for _, user := range []string{"normal1", "normal2"} {
		out, _ := call(t, s, user, "gopeed.pending", map[string]any{})
		if out["success"] != true || len(out["data"].([]any)) != 0 {
			t.Fatal(out)
		}
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(raw) {
		t.Fatal("read migrated or reassigned old history")
	}
	got, _ = os.ReadFile(filepath.Join(s.Data, "settings.json"))
	if string(got) != string(global) {
		t.Fatal("global settings changed")
	}
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		count := 0
		for _, j := range st.Jobs {
			if j.Owner != "uid:admin" {
				t.Fatal("reassigned owner")
			}
			if j.Published {
				count++
			}
		}
		if count != 7 || !st.Jobs["mine"].Uncertain || st.Folders["uid:admin"] != "/home/Downloads" {
			t.Fatal(st)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
