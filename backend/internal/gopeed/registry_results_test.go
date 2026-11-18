package gopeed

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"dsmapp/internal/binaries"
	"dsmapp/internal/search"
)

// Fill gaps in the existing regression suite with successful handler calls.
// The Node contract gate validates their complete JSON, including nullable fields.
func TestRegistryHandlerResults(t *testing.T) {
	check := func(s *Service, name string, input any) map[string]any {
		out, _ := call(t, s, "admin", name, input)
		if out["success"] != true {
			t.Fatalf("%s: %v", name, out)
		}
		return out
	}
	t.Run("tasks-destination-settings", func(t *testing.T) {
		s, _ := newEngineFixture(t)
		for _, method := range []string{"gopeed.session", "downloads.destination", "gopeed.settings.get", "media.tools.status"} {
			check(s, method, map[string]any{})
		}
		check(s, "gopeed.destination", map[string]any{"key": "save", "path": "/home"})
		if err := binaries.Atomic(filepath.Join(s.Data, "runtime.json"), binaries.Runtime{Active: binaries.Selection{Path: "bin/gopeed", Version: "1.9.3", SHA256: "fixture-digest"}, Ready: true}); err != nil {
			t.Fatal(err)
		}
		check(s, "binaries.status", map[string]any{})
		_, err := s.withState(context.Background(), func(st *State) (any, error) {
			st.Jobs["saved-fixture"] = Job{Owner: "uid:admin", Published: true, Destination: "/home", Filename: "example.bin"}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, method := range []string{"gopeed.publish", "gopeed.reveal"} {
			check(s, method, map[string]string{"id": "saved-fixture"})
		}
	})
	t.Run("extensions", func(t *testing.T) {
		s, _, _ := fixture(t)
		engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			data := any(nil)
			if r.Method == "GET" {
				data = map[string]any{"settings": []any{map[string]any{"name": "enabled", "type": "boolean"}}}
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
		}))
		defer engine.Close()
		s.Engine = engine.URL
		check(s, "gopeed.plugins.switch", map[string]any{"id": "fixture.extension", "enabled": true, "consent": true})
		check(s, "gopeed.plugins.switch", map[string]any{"id": "fixture.extension", "enabled": false, "consent": true})
		check(s, "gopeed.plugins.settings", map[string]any{"id": "fixture.extension", "settings": map[string]bool{"enabled": true}})
		check(s, "gopeed.plugins.delete", map[string]any{"id": "fixture.extension", "consent": true})
	})
	t.Run("sources-catalog-probe", func(t *testing.T) {
		s := searchFixture(t)
		src := check(s, "search.sources.save", map[string]any{"kind": "rss", "name": "Fixture", "url": "https://example.org/feed", "enabled": true})["data"].(map[string]any)["id"].(string)
		check(s, "search.sources.patch", map[string]any{"id": src, "name": "Renamed"})
		admitted := check(s, "search.sources.test.start", map[string]any{"key": "probe-fixture", "id": src})
		runSearchJob(t, s)
		id := admitted["data"].(map[string]any)["job"].(map[string]any)["id"].(string)
		check(s, "search.jobs.result", map[string]any{"id": id})
		check(s, "search.jobs.list", map[string]any{})
		check(s, "search.sources.delete", map[string]any{"id": src})
		bundle := search.Bundle{Commit: "fixture", Schema: 11, Definitions: map[string]string{"fixture": "id: fixture"}}
		raw, _ := json.Marshal(bundle)
		hash := fmt.Sprintf("%x", sha256.Sum256(raw))
		if err := os.MkdirAll(filepath.Join(s.Search.Data, "generations"), 0700); err != nil {
			t.Fatal(err)
		}
		if err := binaries.Atomic(filepath.Join(s.Search.Data, "generations", hash+".json"), map[string]any{"bundle": bundle, "hash": hash}); err != nil {
			t.Fatal(err)
		}
		if err := binaries.Atomic(filepath.Join(s.Search.Data, "catalog.json"), map[string]string{"current": hash, "previous": hash}); err != nil {
			t.Fatal(err)
		}
		s.Search.Run = func(context.Context, search.Request) (search.Reply, error) {
			return search.Reply{Sites: []search.Site{{ID: "fixture", Name: "Fixture", Type: "public", Supported: true, Reasons: []string{}, Settings: []search.Setting{}}}}, nil
		}
		check(s, "search.catalog", map[string]any{})
		check(s, "search.definitions.rollback", map[string]any{})
	})
}
