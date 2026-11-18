package search

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyStoredLinuxDefinitionReceivesOnlyThePinnedCorrection(t *testing.T) {
	var current Bundle
	if err := json.Unmarshal(baseline, &current); err != nil {
		t.Fatal(err)
	}
	raw := current.Definitions["linuxtracker.yml"]
	legacy := strings.Replace(raw, "  error:\n    - selector: .empty-state:contains(\"Torrent data is unavailable\")\n      message:\n        text: search_no_data\n\n", "", 1)
	legacy = strings.Replace(legacy, "    download:\n      selector: a.torrent-name\n      attribute: href\n", "", 1)
	legacy = strings.ReplaceAll(legacy, "selector: td.seeds strong", "selector: td.seeds")
	legacy = strings.ReplaceAll(legacy, "selector: td.leeches strong", "selector: td.leeches")
	legacy = strings.ReplaceAll(legacy, "selector: td:last-child strong", "selector: td:last-child")
	legacy = strings.TrimSuffix(legacy, "\ndownload:\n  selectors:\n    - selector: a[href^=\"magnet:\"]\n      attribute: href\n")
	src := Source{SourceInput: SourceInput{Kind: "builtin", DefinitionID: "linuxtracker"}}
	b := Bundle{Definitions: map[string]string{"linuxtracker.yml": legacy}}
	got, err := definitionIn(b, src)
	if err != nil || got != raw {
		t.Fatal("cached legacy definition was not corrected", err)
	}
	b.Definitions["linuxtracker.yml"] = legacy + "# newer upstream definition\n"
	got, err = definitionIn(b, src)
	if err != nil || got != b.Definitions["linuxtracker.yml"] {
		t.Fatal("a newer upstream definition was replaced", err)
	}
}
