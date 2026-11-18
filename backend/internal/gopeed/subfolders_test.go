package gopeed

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

func TestRelativeSubfolderValidation(t *testing.T) {
	for _, p := range []string{"", "Downloads", "Videos/YouTube/Channel", "相簿/照片", strings.Repeat("x", 255)} {
		if !validSubfolder(p) {
			t.Error("refused", p)
		}
	}
	for _, p := range []string{"/home", "..", "a/../b", "a//b", "a/", "./a", `a\b`, "a:b", "a*b", "a?b", "a|b", "a\nb", "a\x00b", "a/ ", "a.", "@eaDir", "a/#recycle", strings.Repeat("界", 86), strings.Repeat("a/", 17) + "b", string([]byte{255})} {
		if validSubfolder(p) {
			t.Errorf("accepted %q", p)
		}
	}
}

type folderDestination struct {
	root, scope string
	picks       int
	refusal     error
	lostReply   bool
}

func (d *folderDestination) Destination(_ context.Context, c api.Caller, _ *http.Request, key string) (delegate.DestinationState, error) {
	root := "/home"
	if d.scope != "home" {
		root = "/media"
	}
	out := delegate.DestinationState{Key: key, Status: "missing", Selection: &delegate.ScopeSelection{Scope: d.scope, Root: root, Generation: 1}}
	if d.root != "" {
		out.Status = "active"
		out.Destination = &delegate.DestinationGrant{ID: "grant-" + c.User(), Key: key, Path: d.root, Username: c.User(), Lifetime: "until-revoked"}
	}
	return out, nil
}
func (d *folderDestination) SelectDestination(ctx context.Context, c api.Caller, r *http.Request, in delegate.SelectDestinationParams) (delegate.DestinationGrant, error) {
	d.picks++
	if d.refusal != nil {
		return delegate.DestinationGrant{}, d.refusal
	}
	d.root = in.Path
	if d.lostReply {
		d.lostReply = false
		return delegate.DestinationGrant{}, refused("EXECUTOR_TIMEOUT")
	}
	out, _ := d.Destination(ctx, c, r, in.Key)
	return *out.Destination, nil
}
func (d *folderDestination) CheckDestination(_ context.Context, _ api.Caller, _ *http.Request, _ string, p string) error {
	if p != d.root {
		return refused("background_destination_required")
	}
	return nil
}

func TestFirstFolderConfirmationAndExistingRootMigration(t *testing.T) {
	s, _, _ := fixture(t)
	d := &folderDestination{scope: "home"}
	s.DestinationDelegate = d
	out, _ := call(t, s, "alice", "downloads.location", map[string]any{})
	if out["success"] != true || out["data"].(map[string]any)["subfolder"] != "Downloads" || d.picks != 0 {
		t.Fatal(out, d)
	}
	out, _ = call(t, s, "alice", "downloads.folder.set", map[string]string{"subfolder": "Downloads/Nested"})
	if out["success"] != true || d.root != "/home" || d.picks != 1 {
		t.Fatal(out, d)
	}
	out, _ = call(t, s, "alice", "downloads.folder.set", map[string]string{"subfolder": ""})
	if out["success"] != true || d.picks != 1 {
		t.Fatal(out, d)
	}
	st, _ := s.readState()
	if st.Folders["uid:alice"] != "/home" || st.Grants["uid:alice"] != "grant-alice" {
		t.Fatal(st)
	}
	// Changing the subfolder never makes a physical directory in App data.
	if _, e := os.Stat(filepath.Join(s.Data, "Downloads")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	d.root = "/media/Existing"
	out, _ = call(t, s, "bob", "downloads.location", map[string]any{})
	if out["data"].(map[string]any)["subfolder"] != "" || d.picks != 1 {
		t.Fatal("migration reselected an existing root", out)
	}
}

func TestHomeScopeAndGrantRefusalsDoNotChooseSharedRoots(t *testing.T) {
	for _, scope := range []string{"home", "shared:media"} {
		s, _, _ := fixture(t)
		d := &folderDestination{scope: scope, refusal: refused("not-found")}
		s.DestinationDelegate = d
		out, _ := call(t, s, "alice", "downloads.folder.set", map[string]string{"subfolder": "Downloads"})
		want := "not-found"
		picks := 1
		if scope != "home" {
			want = "home_scope_required"
			picks = 0
		}
		if out["success"] != false || errorCode(out) != want || d.picks != picks || d.root != "" {
			t.Fatal(out, d)
		}
	}
}

func TestRuleSubfolderKeepsRootAndMainFolder(t *testing.T) {
	s, _, _ := fixture(t)
	d := &folderDestination{root: "/home", scope: "home"}
	s.DestinationDelegate = d
	call(t, s, "alice", "downloads.folder.set", map[string]string{"subfolder": "Downloads"})
	out, _ := call(t, s, "alice", "media.rules.save", map[string]any{"host": "gallery.example", "path": "Videos/Channel", "options": map[string]string{"format": "best"}})
	if out["success"] != true || d.picks != 0 {
		t.Fatal(out, d)
	}
	out, _ = call(t, s, "alice", "downloads.create_many", map[string]any{"path": "/home/Downloads", "items": []map[string]string{{"key": "relative-folder-1", "url": "https://gallery.example/post"}}})
	if out["success"] != true {
		t.Fatal(out)
	}
	st, _ := s.readState()
	for _, j := range st.Jobs {
		if j.Destination != "/home" || j.Subfolder != "Videos/Channel" || j.DelegationID != "grant-alice" {
			t.Fatal(j)
		}
	}
	if downloadSubfolder(&st, "uid:alice") != "Downloads" || st.Folders["uid:alice"] != "/home" {
		t.Fatal(st)
	}
}

func TestNestedSingleFilePublicationPinsNameAndReceipt(t *testing.T) {
	s, f := stagedDelegate(t)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		j := st.Jobs["mine"]
		j.Destination = "/home"
		j.Subfolder = "Videos/Channel"
		st.Jobs["mine"] = j
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	f.state = "committed"
	f.receipt = &delegate.Receipt{Path: "/home/Videos/Channel/file.bin", Size: 8}
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.input.Name != "Videos/Channel/file.bin" || !readJob(t, s).Published {
		t.Fatal(f.input, readJob(t, s))
	}
	// A root/subfolder setting change must not alter the already pinned tuple.
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if f.calls != 1 {
		t.Fatal("committed work resent", f.calls)
	}
}

func TestLegacyRuleMigrationStaysWithinObservedRoot(t *testing.T) {
	p := MediaPreferences{Rules: []MediaRule{{Path: "/media/Existing/Album", Grant: "legacy"}, {Path: "/other", Grant: "outside"}}, Follows: []MediaFollow{{Path: "/media/Existing", Grant: "legacy"}}}
	migrated := migrateMediaSubfolders(p, "/media/Existing")
	if migrated.Rules[0].Path != "Album" || migrated.Follows[0].Path != "" || migrated.Rules[0].Grant != "" {
		t.Fatal(migrated)
	}
	if validSubfolder(migrated.Rules[1].Path) || migrated.Rules[1].Grant != "outside" {
		t.Fatal("outside path reinterpreted", migrated)
	}
	if p.Rules[0].Path != "/media/Existing/Album" {
		t.Fatal("input snapshot changed")
	}
}

func TestGalleryStagingCountAndFileTypesAreBounded(t *testing.T) {
	stage := t.TempDir()
	if !galleryWithinLimits(stage) {
		t.Fatal("empty stage")
	}
	for i := 0; i < 257; i++ {
		p := filepath.Join(stage, fmt.Sprintf("%03d.jpg", i))
		if e := os.WriteFile(p, []byte("image"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if galleryWithinLimits(stage) {
		t.Fatal("gallery over count limit")
	}
	stage = t.TempDir()
	os.Symlink(t.TempDir(), filepath.Join(stage, "link"))
	if galleryWithinLimits(stage) {
		t.Fatal("gallery followed a symlink")
	}
}

func TestLostHomeGrantReplyKeepsConfirmedSubfolder(t *testing.T) {
	s, _, _ := fixture(t)
	d := &folderDestination{scope: "home", lostReply: true}
	s.DestinationDelegate = d
	out, _ := call(t, s, "alice", "downloads.folder.set", map[string]string{"subfolder": "Pictures/Chosen"})
	if out["success"] != false || d.root != "/home" {
		t.Fatal(out, d)
	}
	out, _ = call(t, s, "alice", "downloads.location", map[string]any{})
	if out["success"] != true || out["data"].(map[string]any)["subfolder"] != "Pictures/Chosen" || d.picks != 1 {
		t.Fatal(out, d)
	}
}
