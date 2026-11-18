package multisave

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func names(items []Item) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.Name)
	}
	return out
}

func TestPlanPlacesOneTopLevelFileDirectly(t *testing.T) {
	items, err := Plan("ignored title", "", []Source{{Path: "ubuntu.iso", Size: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Name != "ubuntu.iso" || items[0].Source != "ubuntu.iso" || items[0].Size != 7 || items[0].State != "" {
		t.Fatalf("%+v", items)
	}
}

func TestPlanPrefixesTaskFolderForSeveralFiles(t *testing.T) {
	items, err := Plan("Album", "Album", []Source{{Path: "cd1/01.flac", Size: 1}, {Path: "cover.jpg", Size: 2}})
	if err != nil {
		t.Fatal(err)
	}
	got := names(items)
	if strings.Join(got, "|") != "Album - cd1 - 01.flac|Album - cover.jpg" {
		t.Fatalf("%q", got)
	}
	if items[0].Source != "Album/cd1/01.flac" || items[0].Rel != "Album/cd1/01.flac" || items[1].Rel != "Album/cover.jpg" {
		t.Fatalf("%+v", items)
	}
}

func TestPlanUsesTaskFolderForOneNestedFile(t *testing.T) {
	items, err := Plan("Pack", "", []Source{{Path: "docs/readme.txt", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Name != "Pack - docs - readme.txt" || items[0].Rel != "Pack/docs/readme.txt" {
		t.Fatalf("%+v", items)
	}
}

func TestPlanRefusesEscapingAndInvalidSources(t *testing.T) {
	for _, bad := range [][]Source{
		nil,
		{{Path: "../x", Size: 1}},
		{{Path: "/abs", Size: 1}},
		{{Path: "a/./b", Size: 1}},
		{{Path: "a//b", Size: 1}},
		{{Path: "a\\b", Size: 1}},
		{{Path: "a\x00", Size: 1}},
		{{Path: "a", Size: -1}},
		{{Path: "a", Size: MaxFileBytes + 1}},
		{{Path: "a", Size: 1}, {Path: "a", Size: 1}},
	} {
		if _, err := Plan("t", "", bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if _, err := Plan("t", "../root", []Source{{Path: "a", Size: 1}}); err == nil {
		t.Fatal("accepted escaping root")
	}
	many := make([]Source, MaxFiles+1)
	for i := range many {
		many[i] = Source{Path: strings.Repeat("x", i%7+1) + "/" + string(rune('a'+i%26)) + strings.Repeat("y", i/26), Size: 1}
	}
	if _, err := Plan("t", "", many); err == nil {
		t.Fatal("accepted too many files")
	}
}

func TestPlanSanitizesNamesForDSM(t *testing.T) {
	items, err := Plan(`a:b`, "", []Source{{Path: "x,\"y\".txt", Size: 1}, {Path: "q?/ z .", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names(items) {
		if strings.ContainsAny(n, "/\\,\":*?<>|\x00\r\n") || strings.HasSuffix(n, ".") || strings.HasSuffix(n, " ") {
			t.Fatalf("unsafe %q", n)
		}
	}
}

func TestPlanBoundsLongNamesKeepingExtensionAndUniqueness(t *testing.T) {
	long := strings.Repeat("長", 84)
	items, err := Plan("T", "", []Source{{Path: long + "/a.mkv", Size: 1}, {Path: long + "/b.mkv", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if items[0].Name == items[1].Name {
		t.Fatal("duplicate names")
	}
	for _, n := range names(items) {
		if len(n) > MaxNameBytes || !utf8.ValidString(n) || !strings.HasSuffix(n, ".mkv") {
			t.Fatalf("bad %q (%d)", n, len(n))
		}
	}
}

func TestPlanDisambiguatesCaseInsensitiveCollisions(t *testing.T) {
	items, err := Plan("T", "", []Source{{Path: "d/A.txt", Size: 1}, {Path: "d/a.txt", Size: 1}, {Path: "d,a.txt", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, n := range names(items) {
		key := strings.ToLower(n)
		if seen[key] {
			t.Fatalf("collision %q", names(items))
		}
		seen[key] = true
	}
}
