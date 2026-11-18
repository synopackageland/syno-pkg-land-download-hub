package gopeed

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOfficialCatalogMetadataRejectsUntrustedURLs(t *testing.T) {
	s, _, _ := fixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Api-Token") != "" || r.Header.Get("X-SYNO-TOKEN") != "" {
			t.Error("credentials leaked to registry")
		}
		w.Write([]byte(`{"data":[{"id":"gopeed@sample","title":"Sample","description":"Official sample","version":"1.0.0","repoUrl":"https://github.com/GopeedLab/samples","directory":"sample"},{"id":"evil","repoUrl":"https://evil.invalid/repo"}],"pagination":{"hasNext":false}}`))
	}))
	defer upstream.Close()
	s.CatalogURL = upstream.URL
	out := adminCall(t, s, "gopeed.plugins.catalog", map[string]any{})
	b, _ := json.Marshal(out)
	if out["success"] != true || !strings.Contains(string(b), "Official sample") || strings.Contains(string(b), "evil.invalid") || strings.Contains(string(b), "permissions") {
		t.Fatal(string(b))
	}
}

// A store icon is shown on the Recommended cards: https on GitHub's raw host
// only, so the App never loads an image from an arbitrary host.
func TestCatalogIconOnlyFromGitHubRaw(t *testing.T) {
	s, _, _ := fixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[` +
			`{"id":"a@one","title":"One","repoUrl":"https://github.com/a/one","icon":"https://raw.githubusercontent.com/a/one/main/icon.png"},` +
			`{"id":"b@two","title":"Two","repoUrl":"https://github.com/b/two","icon":"http://raw.githubusercontent.com/b/two/main/icon.png"},` +
			`{"id":"c@three","title":"Three","repoUrl":"https://github.com/c/three","icon":"https://tracker.invalid/pixel.png"},` +
			`{"id":"d@four","title":"Four","repoUrl":"https://github.com/d/four","icon":null}` +
			`],"pagination":{"hasNext":false}}`))
	}))
	defer upstream.Close()
	s.CatalogURL = upstream.URL
	out := adminCall(t, s, "gopeed.plugins.catalog", map[string]any{})
	b, _ := json.Marshal(out["data"])
	var entries []CatalogEntry
	if err := json.Unmarshal(b, &entries); err != nil || len(entries) != 4 {
		t.Fatal(string(b))
	}
	want := []string{"https://raw.githubusercontent.com/a/one/main/icon.png", "", "", ""}
	for i, e := range entries {
		if e.Icon != want[i] {
			t.Fatalf("%s icon %q", e.ID, e.Icon)
		}
	}
}
