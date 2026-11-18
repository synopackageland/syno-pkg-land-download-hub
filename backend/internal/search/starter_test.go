package search

import (
	"context"
	"testing"
)

// The catalog the helper reports: three usable public sites (two with text
// settings that have a default or filter nothing), a private one, a public one
// that needs input, an unsupported one and one outside the set.
func starterService(t *testing.T) (*Service, *int) {
	t.Helper()
	s := fixtureService(t)
	catalogCalls := 0
	sites := []Site{
		{ID: "1337x", Name: "1337x", Type: "public", Supported: true, Reasons: []string{}, Settings: []Setting{{Name: "uploader", Type: "text", Optional: true}}},
		{ID: "eztv", Name: "EZTV", Type: "public", Supported: true, Reasons: []string{}, Settings: []Setting{}},
		{ID: "thepiratebay", Name: "The Pirate Bay", Type: "public", Supported: true, Reasons: []string{}, Settings: []Setting{{Name: "apiurl", Type: "text", Optional: true}, {Name: "top100", Type: "select"}}},
		{ID: "yts", Name: "YTS", Type: "public", Supported: false, Reasons: []string{"unsupported_filter"}, Settings: []Setting{}},
		{ID: "nyaasi", Name: "Nyaa.si", Type: "private", Supported: true, Reasons: []string{}, Settings: []Setting{}},
		{ID: "rutor", Name: "RuTor", Type: "public", Supported: true, Reasons: []string{}, Settings: []Setting{{Name: "username", Type: "text"}}},
		{ID: "elsewhere", Name: "Elsewhere", Type: "public", Supported: true, Reasons: []string{}, Settings: []Setting{}},
	}
	// Catalog insists on one answer per definition of the bundle.
	bundle, err := s.bundle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for len(sites) < len(bundle.Definitions) {
		sites = append(sites, Site{ID: "filler", Name: "Filler", Type: "private", Reasons: []string{}, Settings: []Setting{}})
	}
	s.Run = func(ctx context.Context, in Request) (Reply, error) {
		switch in.Op {
		case "catalog":
			catalogCalls++
			return Reply{Sites: sites}, nil
		case "validate":
			return Reply{Sites: []Site{{ID: "x", Name: "x", Supported: true, Reasons: []string{}, Settings: []Setting{}}}}, nil
		}
		return Reply{}, nil
	}
	return s, &catalogCalls
}
func TestFreshUserStartsWithUsablePublicSites(t *testing.T) {
	s, _ := starterService(t)
	ctx := context.Background()
	list, err := s.Sources(ctx, "1:fresh")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, x := range list {
		if x.Kind != "builtin" || !x.Enabled {
			t.Fatalf("starter source %+v", x)
		}
		names[x.Name] = true
	}
	if len(list) != 3 || !names["1337x"] || !names["EZTV"] || !names["The Pirate Bay"] {
		t.Fatalf("starter set %v", names)
	}
}
func TestStarterSitesAreSeededOnceAndStayDeleted(t *testing.T) {
	s, calls := starterService(t)
	ctx := context.Background()
	list, _ := s.Sources(ctx, "1:a")
	if err := s.DeleteSource(ctx, "1:a", list[0].ID); err != nil {
		t.Fatal(err)
	}
	again, err := s.Sources(ctx, "1:a")
	if err != nil || len(again) != 2 {
		t.Fatalf("%+v %v", again, err)
	}
	if *calls != 1 {
		t.Fatalf("catalog read %d times", *calls)
	}
	other, _ := s.Sources(ctx, "2:b")
	if len(other) != 3 {
		t.Fatalf("another user's starter set: %+v", other)
	}
}
func TestUserWithOwnSourcesKeepsExactlyThem(t *testing.T) {
	s, _ := starterService(t)
	ctx := context.Background()
	if _, err := s.SaveSource(ctx, "1:own", SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	list, err := s.Sources(ctx, "1:own")
	if err != nil || len(list) != 1 || list[0].Name != "Feed" {
		t.Fatalf("%+v %v", list, err)
	}
}
func TestStarterWaitsWhenCatalogCannotBeRead(t *testing.T) {
	s, _ := starterService(t)
	ctx := context.Background()
	run := s.Run
	s.Run = func(ctx context.Context, in Request) (Reply, error) {
		if in.Op == "catalog" {
			return Reply{}, fail("search_helper")
		}
		return run(ctx, in)
	}
	if list, err := s.Sources(ctx, "1:late"); err != nil || len(list) != 0 {
		t.Fatalf("%+v %v", list, err)
	}
	s.Run = run
	if list, err := s.Sources(ctx, "1:late"); err != nil || len(list) != 3 {
		t.Fatalf("after recovery: %+v %v", list, err)
	}
}

// The curated set never contains an adult-only, private or unreachable site by
// construction; this pins its size and the three sites the product requires.
func TestStarterSetIsFifteenAndHasTheRequiredSites(t *testing.T) {
	if len(starterSites) != 15 {
		t.Fatalf("%d starter sites", len(starterSites))
	}
	seen := map[string]bool{}
	for _, id := range starterSites {
		if seen[id] {
			t.Fatalf("duplicate %s", id)
		}
		seen[id] = true
	}
	for _, id := range []string{"1337x", "thepiratebay", "eztv"} {
		if !seen[id] {
			t.Fatalf("%s missing", id)
		}
	}
}

// The catalog of a user seeded before the starter set grew: the eight sites
// that set had given them, all still usable, plus the sites added since.
func legacyService(t *testing.T) *Service {
	t.Helper()
	s := fixtureService(t)
	var sites []Site
	for _, id := range append(append([]string{}, starterSites...), legacyStarterSites...) {
		sites = append(sites, Site{ID: id, Name: id, Type: "public", Supported: true, Reasons: []string{}, Settings: []Setting{}})
	}
	bundle, err := s.bundle(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var unique []Site
	for _, site := range sites {
		if !seen[site.ID] {
			seen[site.ID] = true
			unique = append(unique, site)
		}
	}
	for len(unique) < len(bundle.Definitions) {
		unique = append(unique, Site{ID: "filler", Name: "Filler", Type: "private", Reasons: []string{}, Settings: []Setting{}})
	}
	s.Run = func(ctx context.Context, in Request) (Reply, error) {
		switch in.Op {
		case "catalog":
			return Reply{Sites: unique}, nil
		case "validate":
			return Reply{Sites: []Site{{ID: "x", Name: "x", Supported: true, Reasons: []string{}, Settings: []Setting{}}}}, nil
		}
		return Reply{}, nil
	}
	return s
}

// seedLegacy gives owner what the first starter version gave: automatic rows.
func seedLegacy(t *testing.T, s *Service, owner string) {
	t.Helper()
	err := s.withState(context.Background(), owner, func(st *state) error {
		for _, def := range legacyStarterSites {
			src := Source{Automatic: true, SourceInput: SourceInput{ID: id(), Kind: "builtin", Name: def, Enabled: true, DefinitionID: def}, Revision: id()}
			st.Sources[src.ID] = src
		}
		st.Starter = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func definitions(list []SourceView) map[string]bool {
	out := map[string]bool{}
	for _, x := range list {
		out[x.DefinitionID] = true
	}
	return out
}

func TestUntouchedStarterUserGetsTheNewSitesOnce(t *testing.T) {
	s := legacyService(t)
	ctx := context.Background()
	seedLegacy(t, s, "1:old")
	list, err := s.Sources(ctx, "1:old")
	if err != nil {
		t.Fatal(err)
	}
	have := definitions(list)
	for _, id := range append(append([]string{}, starterSites...), legacyStarterSites...) {
		if !have[id] {
			t.Fatalf("%s missing from %v", id, have)
		}
	}
	for _, x := range list {
		if x.Explicit || !x.Enabled {
			t.Fatalf("added rows must be automatic and on: %+v", x)
		}
	}
	// Once: a site the user drops afterwards stays dropped.
	var gone string
	for _, x := range list {
		if x.DefinitionID == "thepiratebay" {
			gone = x.ID
		}
	}
	if err := s.DeleteSource(ctx, "1:old", gone); err != nil {
		t.Fatal(err)
	}
	again, _ := s.Sources(ctx, "1:old")
	if definitions(again)["thepiratebay"] || len(again) != len(list)-1 {
		t.Fatalf("starter sites came back: %d", len(again))
	}
}

func TestCustomisedStarterUserIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	for name, touch := range map[string]func(*Service, string, []SourceView) error{
		"disabled": func(s *Service, o string, l []SourceView) error { return s.SetEnabled(ctx, o, l[0].ID, false) },
		"deleted":  func(s *Service, o string, l []SourceView) error { return s.DeleteSource(ctx, o, l[0].ID) },
		"own source": func(s *Service, o string, l []SourceView) error {
			_, err := s.SaveSource(ctx, o, SourceInput{Kind: "rss", Name: "Feed", URL: "https://example.org/feed", Enabled: true})
			return err
		},
		"edited": func(s *Service, o string, l []SourceView) error {
			_, err := s.SaveSource(ctx, o, SourceInput{ID: l[0].ID, Kind: "builtin", Name: l[0].Name, DefinitionID: l[0].DefinitionID, Enabled: true, Values: map[string]string{"k": "v"}})
			return err
		},
	} {
		s := legacyService(t)
		seedLegacy(t, s, "1:old")
		// The user's customisation happened before this version: take the listing out of the way.
		s.withState(ctx, "1:old", func(st *state) error { st.StarterVersion = starterVersion; return nil })
		list, _ := s.Sources(ctx, "1:old")
		s.withState(ctx, "1:old", func(st *state) error { st.StarterVersion = 0; return nil })
		if err := touch(s, "1:old", list); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		before, _ := s.Sources(ctx, "1:old")
		if definitions(before)["thepiratebay"] {
			t.Fatalf("%s: customised user received new sites", name)
		}
		// It is decided once: undoing the customisation later changes nothing.
		again, _ := s.Sources(ctx, "1:old")
		if len(again) != len(before) {
			t.Fatalf("%s: %d -> %d", name, len(before), len(again))
		}
	}
}

func TestStarterMigrationWaitsWhenCatalogCannotBeRead(t *testing.T) {
	s := legacyService(t)
	ctx := context.Background()
	seedLegacy(t, s, "1:old")
	run := s.Run
	s.Run = func(ctx context.Context, in Request) (Reply, error) {
		if in.Op == "catalog" {
			return Reply{}, fail("search_helper")
		}
		return run(ctx, in)
	}
	if list, err := s.Sources(ctx, "1:old"); err != nil || len(list) != len(legacyStarterSites) {
		t.Fatalf("%+v %v", list, err)
	}
	s.Run = run
	if list, _ := s.Sources(ctx, "1:old"); !definitions(list)["thepiratebay"] {
		t.Fatal("not retried after the catalog came back")
	}
}
