package binaries

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStartupResumesOwnedInterruptedTasksOnly(t *testing.T) {
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
	os.WriteFile(filepath.Join(data, "jobs.json"), []byte(`{"jobs":{"interrupted":{},"manual":{"manualPaused":true},"finished":{},"failed":{},"deleting":{"deleteRequested":true}}}`), 0600)
	var resumed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Token") != strings.Repeat("a", 64) || r.Header.Get("Cookie") != "" {
			t.Error("credential boundary")
		}
		if r.Method == "GET" {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]string{{"id": "interrupted", "status": "pause"}, {"id": "manual", "status": "pause"}, {"id": "deleting", "status": "pause"}, {"id": "foreign", "status": "pause"}, {"id": "finished", "status": "done"}, {"id": "failed", "status": "error"}}})
			return
		}
		resumed = append(resumed, r.URL.Path)
		w.Write([]byte(`{"code":0}`))
	}))
	defer server.Close()
	if e := resumeInterrupted(context.Background(), data, server.URL); e != nil {
		t.Fatal(e)
	}
	if len(resumed) != 1 || resumed[0] != "/api/v1/tasks/interrupted/continue" {
		t.Fatal("not restored safely", resumed)
	}
}
func TestStartupRefusesCorruptJournal(t *testing.T) {
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "jobs.json"), []byte(`{`), 0600)
	if resumeInterrupted(context.Background(), data, "http://127.0.0.1:1") == nil {
		t.Fatal("corrupt journal accepted")
	}
}
func TestStartupReadsJournalsWithPerFileRecords(t *testing.T) {
	data := t.TempDir()
	os.WriteFile(filepath.Join(data, "engine.token"), []byte(strings.Repeat("a", 64)), 0600)
	pad := strings.Repeat("x", 3<<20)
	os.WriteFile(filepath.Join(data, "jobs.json"), []byte(`{"jobs":{"bt":{"filename":"`+pad+`"}}}`), 0600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"data":[]}`))
	}))
	defer server.Close()
	if e := resumeInterrupted(context.Background(), data, server.URL); e != nil {
		t.Fatal(e)
	}
}
