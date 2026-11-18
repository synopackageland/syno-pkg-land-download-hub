package search

import "context"

// starterSites are the general-purpose public sites a user starts with, so a
// first search works before anything is set up. The set is small on purpose:
// the catalog holds hundreds of sites, many of them adult or dead, and a
// search must stay quick. Each is public, needs no input, is not adult-only
// and answered common queries when probed; 1337x, thepiratebay and eztv are
// required even though Cloudflare often fronts them. Each user gets their own
// rows, which they may turn off or delete.
var starterSites = []string{
	"1337x", "thepiratebay", "eztv", "yts", "kickasstorrents-ws",
	"torrentkitty", "rutor", "noname-club", "zamundalife", "internetarchive",
	"nyaasi", "mikan", "dmhy", "shanaproject", "acgrip",
}

// starterVersion counts the revisions of the set above; state.StarterVersion
// is the one a user has been given.
const starterVersion = 2

// legacyStarterSites are the rows the first version of the set gave (it
// then skipped 1337x, thepiratebay and yts for their text settings, and eztv
// for its unsupported filter). An untouched user still has exactly the usable
// ones, which is how migrateStarter tells them from a user who changed things.
var legacyStarterSites = []string{
	"nyaasi", "limetorrents", "linuxtracker", "kickasstorrents-ws",
	"torrentdownloads", "torrentkitty", "internetarchive", "uindex",
}

// starterPick returns the starter sites this catalog can run without sign-in.
func starterPick(sites []Site) []Site { return pick(sites, starterSites) }
func pick(sites []Site, ids []string) []Site {
	byID := map[string]Site{}
	for _, site := range sites {
		byID[site.ID] = site
	}
	out := []Site{}
	for _, id := range ids {
		site, ok := byID[id]
		if !ok || !site.Supported || site.Type != "public" {
			continue
		}
		needsInput := false
		for _, setting := range site.Settings {
			if !setting.Optional && (setting.Type == "text" || setting.Type == "password") {
				needsInput = true
			}
		}
		if !needsInput {
			out = append(out, site)
		}
	}
	return out
}

// seedStarter gives a user who has never had sources the starter sites, once.
// A user who already has sources keeps exactly them; either way the user is
// marked, so a deleted starter site never comes back. A catalog that cannot
// be read leaves the user unmarked, to try again on the next listing.
func (s *Service) seedStarter(ctx context.Context, owner string) error {
	done, migrate := false, false
	if err := s.withState(ctx, owner, func(st *state) error {
		done = st.Starter
		migrate = st.Starter && st.StarterVersion < starterVersion
		return nil
	}); err != nil {
		return err
	}
	if migrate {
		return s.migrateStarter(ctx, owner)
	}
	if done {
		return nil
	}
	var picks []Site
	empty := false
	if err := s.withState(ctx, owner, func(st *state) error {
		empty = len(st.Sources) == 0
		return nil
	}); err != nil {
		return err
	}
	if empty {
		catalog, err := s.Catalog(ctx)
		if err != nil {
			return nil
		}
		picks = starterPick(catalog.Sites)
	}
	return s.withState(ctx, owner, func(st *state) error {
		if st.Starter {
			return nil
		}
		if len(st.Sources) == 0 {
			for _, site := range picks {
				src := Source{Automatic: true, SourceInput: SourceInput{ID: id(), Kind: "builtin", Name: site.Name, Enabled: true, DefinitionID: site.ID}, Revision: id()}
				st.Sources[src.ID] = src
			}
		}
		st.Starter = true
		st.StarterVersion = starterVersion
		return nil
	})
}

// untouched reports whether sources are still exactly what the first starter
// version gave: automatic (any edit, toggle or save makes a row explicit),
// enabled, built-in, and every usable site of that set present, so none was
// deleted. Anything else is a user who set things up their way.
func untouched(sources map[string]Source, catalog []Site) bool {
	want := map[string]bool{}
	for _, site := range pick(catalog, legacyStarterSites) {
		want[site.ID] = true
	}
	have := map[string]bool{}
	for _, src := range sources {
		if !src.Automatic || !src.Enabled || src.Kind != "builtin" || !want[src.DefinitionID] {
			return false
		}
		have[src.DefinitionID] = true
	}
	return len(have) == len(want) && len(want) > 0
}

// migrateStarter adds the starter sites a user lacks, once, but only to a
// user who never changed the first set (see untouched). Everyone else keeps
// their sources and is only marked. An unreadable catalog leaves the user
// unmarked to try again on the next listing.
func (s *Service) migrateStarter(ctx context.Context, owner string) error {
	catalog, err := s.Catalog(ctx)
	if err != nil {
		return nil
	}
	return s.withState(ctx, owner, func(st *state) error {
		if st.StarterVersion >= starterVersion {
			return nil
		}
		if untouched(st.Sources, catalog.Sites) {
			have := map[string]bool{}
			for _, src := range st.Sources {
				have[src.DefinitionID] = true
			}
			for _, site := range starterPick(catalog.Sites) {
				if !have[site.ID] {
					src := Source{Automatic: true, SourceInput: SourceInput{ID: id(), Kind: "builtin", Name: site.Name, Enabled: true, DefinitionID: site.ID}, Revision: id()}
					st.Sources[src.ID] = src
				}
			}
		}
		st.StarterVersion = starterVersion
		return nil
	})
}
