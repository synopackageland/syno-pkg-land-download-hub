package main

import (
	"os"
	"regexp"
	"testing"
)

// The backend seeds a starter site only when this helper's catalog calls it
// supported and public, and it needs no text or password (starterPick). Check
// every site of backend's starterSites against the real bundled definitions,
// so none of the fifteen is silently left out of a new user's sources.
func TestEveryStarterSiteIsSeededFromTheRealCatalog(t *testing.T) {
	src, err := os.ReadFile("../backend/internal/search/starter.go")
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)var starterSites = \[\]string\{(.*?)\}`).FindSubmatch(src)
	if block == nil {
		t.Fatal("starterSites not found")
	}
	ids := regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(block[1], -1)
	if len(ids) != 15 {
		t.Fatalf("%d starter sites", len(ids))
	}
	for _, m := range ids {
		id := string(m[1])
		_, site := inspect(bundled(t, id+".yml"))
		if site.ID != id || !site.Supported || site.Type != "public" {
			t.Errorf("%s: id %q type %q supported %v reasons %v", id, site.ID, site.Type, site.Supported, site.Reasons)
		}
		for _, s := range site.Settings {
			if !s.Optional && (s.Type == "text" || s.Type == "password") {
				t.Errorf("%s needs %s", id, s.Name)
			}
		}
	}
}
