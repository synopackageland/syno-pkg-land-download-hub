package multisave

import (
	"context"
	"path"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

func TestRenameUsesSDKPolicyAndRetainsProtectedPublishedName(t *testing.T) {
	items := plan(t)
	ref := OperationID(batch.Task, 0, 0)
	m := &fakeMover{statuses: map[string]delegate.OperationResult{ref: {JobID: ref, State: "committed", Receipt: &delegate.Receipt{Path: "/vol/dest/Album - a (1).flac", Size: 3}}}}
	items = Advance(context.Background(), m, batch, items)
	if m.moves[0].Conflict != "rename" || items[0].State != "committed" || items[0].SavedName != "Album - a (1).flac" {
		t.Fatalf("%+v %+v", m.moves, items)
	}
	before := len(m.moves)
	items = Advance(context.Background(), m, batch, items)
	if len(m.moves) != before {
		t.Fatal("committed rename replayed")
	}
}

func TestLegacyMovePolicyRemainsPinned(t *testing.T) {
	items := plan(t)
	items[0].Conflict = ""
	items[0].State = "uncertain"
	m := &fakeMover{statuses: map[string]delegate.OperationResult{OperationID(batch.Task, 0, 0): {JobID: OperationID(batch.Task, 0, 0), State: "uncertain"}}}
	items = Advance(context.Background(), m, batch, items)
	if m.moves[0].Conflict != "" || items[0].State != "uncertain" {
		t.Fatal("changed uncertain tuple", m.moves, items)
	}
}

func TestCommittedReceiptAcceptsOnlyOriginalOrSDKRenameCandidates(t *testing.T) {
	for _, name := range []string{"report.csv", ".hidden", "archive.tar.gz"} {
		p := delegate.MoveParams{JobID: "job", Name: "Books/" + name, Size: 8, Conflict: "rename"}
		ext := path.Ext(name)
		if ext == name {
			ext = ""
		}
		stem := strings.TrimSuffix(name, ext)
		for _, tc := range []struct {
			leaf string
			ok   bool
		}{{name, true}, {stem + " (1)" + ext, true}, {stem + " (99)" + ext, true}, {stem + " (100)" + ext, false}, {"other.csv", false}} {
			res := delegate.OperationResult{JobID: p.JobID, State: "committed", Receipt: &delegate.Receipt{Path: "/home/Books/" + tc.leaf, Size: 8}}
			if got := CommittedReceipt("/home", p, res); got != tc.ok {
				t.Fatal(name, tc, got)
			}
			if tc.leaf != name {
				p.Conflict = ""
				if CommittedReceipt("/home", p, res) {
					t.Fatal("legacy policy accepted rename")
				}
				p.Conflict = "rename"
			}
		}
		for _, res := range []delegate.OperationResult{
			{JobID: "other", Receipt: &delegate.Receipt{Path: "/home/Books/" + name, Size: 8}},
			{JobID: "job", Receipt: &delegate.Receipt{Path: "/other/Books/" + name, Size: 8}},
			{JobID: "job", Receipt: &delegate.Receipt{Path: "/home/Books/" + name, Size: 9}},
			{JobID: "job"},
		} {
			if CommittedReceipt("/home", p, res) {
				t.Fatal("invalid protected receipt", res)
			}
		}
	}
}

func TestSafeFilenameTreatsSuggestionAsOneLeaf(t *testing.T) {
	for raw, want := range map[string]string{"手機/驗收?.csv": "手機_驗收_.csv", "../report.csv": ".._report.csv", "report.csv": "report.csv", "a\\b.csv": "a_b.csv", "\x00.csv": "_.csv"} {
		if got := SafeFilename(raw); got != want {
			t.Fatal(raw, got, want)
		}
	}
	got := SafeFilename(strings.Repeat("界", 100) + "?.csv")
	if len(got) > 255 || !utf8.ValidString(got) || !strings.HasSuffix(got, ".csv") {
		t.Fatal(got)
	}
}
