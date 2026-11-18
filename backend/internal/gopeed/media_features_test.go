package gopeed

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"dsmapp/internal/media"
)

func mediaUserFixture(t *testing.T) *Service {
	s, _, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	s.Owner = func(name string) (string, error) {
		uid := map[string]string{"normal1": "1001", "normal2": "1002", "admin": "1003"}[name]
		return uid + ":" + name, nil
	}
	return s
}
func TestMediaDefaultsCookiesAndRulesAreOwnerScoped(t *testing.T) {
	s := mediaUserFixture(t)
	out, _ := call(t, s, "normal1", "media.settings.save", map[string]any{"format": "720", "nfo": true})
	if out["success"] != true {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal2", "media.settings.get", map[string]any{})
	if out["data"].(map[string]any)["defaults"].(map[string]any)["format"] != "best" {
		t.Fatal(out)
	}
	text := "# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tsession\tnever-return-this-secret\n"
	out, _ = call(t, s, "normal1", "media.cookies.import", map[string]string{"text": text})
	if out["success"] != true {
		t.Fatal(out)
	}
	for _, actor := range []string{"normal1", "normal2", "admin"} {
		out, _ = call(t, s, actor, "media.cookies.status", map[string]any{})
		b, _ := json.Marshal(out)
		if strings.Contains(string(b), "never-return") || strings.Contains(string(b), "session") {
			t.Fatal(out)
		}
		n := len(out["data"].(map[string]any)["domains"].([]any))
		if actor == "normal1" && n != 1 || actor != "normal1" && n != 0 {
			t.Fatal(actor, out)
		}
	}
	out, _ = call(t, s, "admin", "media.cookies.status", map[string]string{"owner": "1001:normal1"})
	if out["success"] == true {
		t.Fatal("owner selector accepted")
	}
	out, _ = call(t, s, "normal1", "media.rules.save", map[string]any{"host": "video.example", "path": "Video", "options": map[string]any{"format": "1080"}})
	if out["success"] != true {
		t.Fatal(out)
	}
	out, _ = call(t, s, "normal2", "media.settings.get", map[string]any{})
	if len(out["data"].(map[string]any)["rules"].([]any)) != 0 {
		t.Fatal(out)
	}
}
func TestMediaAdmissionSnapshotsRuleAndDownloadOverride(t *testing.T) {
	s := mediaUserFixture(t)
	ctx := context.Background()
	call(t, s, "normal1", "media.rules.save", map[string]any{"host": "video.example", "path": "Video", "options": map[string]any{"format": "1080"}})
	for _, override := range []bool{false, true} {
		in := map[string]any{"path": "/home", "items": []map[string]string{{"key": randomID(), "url": "https://video.example/watch"}}}
		if override {
			in["mediaOptions"] = map[string]any{"format": "480"}
		}
		out, _ := call(t, s, "normal1", "downloads.create_many", in)
		if out["success"] != true {
			t.Fatal(out)
		}
		ids := out["data"].(map[string]any)["results"].([]any)[0].(map[string]any)["ids"].([]any)
		st, _ := s.readState()
		j := st.Jobs[ids[0].(string)]
		want := "1080"
		if override {
			want = "480"
		}
		if j.Destination != "/home" || j.Subfolder != "Video" || j.DelegationID != "grant-normal1" || j.Preparation.MediaOptions.Format != want {
			t.Fatal(j)
		}
		if _, e := s.withState(ctx, func(st *State) (any, error) {
			p := preferences(st, "1001:normal1")
			p.Defaults.Format = "2160"
			setPreferences(st, "1001:normal1", p)
			return nil, nil
		}); e != nil {
			t.Fatal(e)
		}
		st, _ = s.readState()
		if st.Jobs[ids[0].(string)].Preparation.MediaOptions.Format != want {
			t.Fatal("queued options changed")
		}
	}
}
func TestFollowIdleAndDurableDeduplication(t *testing.T) {
	s := mediaUserFixture(t)
	r := &FollowRunner{Service: s}
	r.Tick(context.Background(), State{})
	if r.done != nil {
		t.Fatal("idle runner started work")
	}
	if _, e := os.Stat(s.mediaHome()); !os.IsNotExist(e) {
		t.Fatal("idle touched media store", e)
	}
	out, _ := call(t, s, "normal1", "media.follows.add", map[string]any{"url": "https://video.example/channel", "path": "", "consent": true})
	if out["success"] != true {
		t.Fatal(out)
	}
	st, _ := s.readState()
	f := preferences(&st, "1001:normal1").Follows[0]
	if f.IntervalHours != 6 {
		t.Fatal(f)
	}
	urls := []string{"https://video.example/watch/one", "https://video.example/watch/two"}
	r.complete(context.Background(), "1001:normal1", f, urls, "")
	r.complete(context.Background(), "1001:normal1", f, urls, "")
	st, _ = s.readState()
	if len(st.Jobs) != 2 {
		t.Fatal("duplicate admission", len(st.Jobs))
	}
	p := preferences(&st, "1001:normal1")
	if len(p.Follows[0].Seen) != 2 || time.Until(p.Follows[0].NextCheck) < 5*time.Hour {
		t.Fatal(p.Follows)
	}
	for _, j := range st.Jobs {
		if j.Owner != "1001:normal1" || j.Preparation == nil || j.Preparation.FollowID != f.ID {
			t.Fatal(j)
		}
	}
	call(t, s, "normal1", "media.follows.delete", map[string]string{"id": f.ID})
	r.complete(context.Background(), "1001:normal1", f, []string{"https://video.example/late"}, "")
	st, _ = s.readState()
	if len(st.Jobs) != 2 {
		t.Fatal("removed subscription admitted late result")
	}
}
func TestRunnerCookieCopyRemovedOnExitAndLaunchFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "exit", true: "start-error"}[fail], func(t *testing.T) {
			s := mediaUserFixture(t)
			home := filepath.Join(s.Data, "media")
			s.MediaEnv = func() media.Env { return media.Env{YtDlp: "/tools/yt-dlp", Home: home} }
			_, e := s.cookies().Import("1001:normal1", "# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tsession\tsecret\n.other.example\tTRUE\t/\tTRUE\t0\tid\tforeign\n")
			if e != nil {
				t.Fatal(e)
			}
			j := Job{Owner: "1001:normal1", Stage: filepath.Join(t.TempDir(), "stage"), Media: &MediaJob{URL: "https://video.example/watch", Format: "best", Status: "running", RunID: "run"}}
			s.withState(context.Background(), func(st *State) (any, error) { st.Jobs["media-test"] = j; return nil, nil })
			var copyPath string
			proc := &fakeProc{stopped: make(chan struct{}), exit: make(chan error, 1)}
			r := &MediaRunner{Service: s, Start: func(_ string, args, env []string, _ func(string)) (MediaProc, error) {
				i := slices.Index(args, "--cookies")
				if i < 0 {
					t.Fatal(args)
				}
				copyPath = args[i+1]
				b, e := os.ReadFile(copyPath)
				if e != nil || !strings.Contains(string(b), "secret") || strings.Contains(string(b), "foreign") {
					t.Fatal(string(b), e)
				}
				if !slices.Contains(args, "--no-cookies-from-browser") {
					t.Fatal(args)
				}
				if fail {
					return nil, os.ErrInvalid
				}
				return proc, nil
			}, running: map[string]*mediaRun{}}
			e = r.launch(context.Background(), s.mediaEnv(), "media-test", j)
			if !fail {
				if e != nil {
					t.Fatal(e)
				}
				proc.exit <- nil
				r.wait()
			}
			if _, e = os.Stat(copyPath); !os.IsNotExist(e) {
				t.Fatal("cookie copy retained", e)
			}
			r.Close()
		})
	}
}
func TestPurgeMediaUserKeepsOriginalSDKReferences(t *testing.T) {
	s := mediaUserFixture(t)
	s.cookies().Import("1001:normal1", "# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tid\tsecret\n")
	s.withState(context.Background(), func(st *State) (any, error) {
		setPreferences(st, "1001:normal1", MediaPreferences{})
		st.Jobs["job"] = Job{Owner: "1001:normal1", DelegationID: "original", SaveJobID: "original-job"}
		return nil, nil
	})
	if e := s.PurgeMediaUser(context.Background(), "1001:normal1"); e != nil {
		t.Fatal(e)
	}
	st, _ := s.readState()
	j := st.Jobs["job"]
	if !j.DeleteRequested || j.DelegationID != "original" || j.SaveJobID != "original-job" {
		t.Fatal(j)
	}
	if _, e := os.Stat(filepath.Join(s.cookies().Home, "1001")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}

func TestFollowCheckUsesOnlyTheOwnersHostFilteredCookies(t *testing.T) {
	s := mediaUserFixture(t)
	home := s.mediaHome()
	s.MediaEnv = func() media.Env { return media.Env{YtDlp: "/tools/yt-dlp", Home: home} }
	s.cookies().Import("1001:normal1", "# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tid\towner-cookie\n.other.example\tTRUE\t/\tTRUE\t0\tid\tunrelated-cookie\n")
	s.cookies().Import("1002:normal2", "# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tid\tforeign-cookie\n")
	out, _ := call(t, s, "normal1", "media.follows.add", map[string]any{"url": "https://video.example/channel", "path": "", "consent": true})
	if out["success"] != true {
		t.Fatal(out)
	}
	st, _ := s.readState()
	f := preferences(&st, "1001:normal1").Follows[0]
	var cookiePath string
	s.MediaExec = func(_ context.Context, _ string, args, env []string, _ int64) ([]byte, []byte, error) {
		i := slices.Index(args, "--cookies")
		if i < 0 {
			t.Fatal(args)
		}
		cookiePath = args[i+1]
		b, e := os.ReadFile(cookiePath)
		if e != nil || !strings.Contains(string(b), "owner-cookie") || strings.Contains(string(b), "unrelated-cookie") || strings.Contains(string(b), "foreign-cookie") {
			t.Fatal(string(b), e)
		}
		if !slices.Contains(args, "--no-cookies-from-browser") || !slices.Contains(args, "--proxy") {
			t.Fatal(args)
		}
		return []byte(`{"entries":[{"url":"https://video.example/watch/one"}]}`), nil, nil
	}
	r := &FollowRunner{Service: s}
	r.check(context.Background(), "1001:normal1", f)
	if _, e := os.Stat(cookiePath); !os.IsNotExist(e) {
		t.Fatal("follow cookie copy retained", e)
	}
	st, _ = s.readState()
	if len(st.Jobs) != 1 {
		t.Fatal(st)
	}
}
func TestAccountDeletionReapsPrivateMediaData(t *testing.T) {
	s := mediaUserFixture(t)
	owner := "1001:missing-media-account-" + randomID()
	s.cookies().Import(owner, "# Netscape HTTP Cookie File\n.video.example\tTRUE\t/\tTRUE\t0\tid\tsecret\n")
	s.withState(context.Background(), func(st *State) (any, error) { setPreferences(st, owner, MediaPreferences{}); return nil, nil })
	if e := s.ReapDeletedMediaUsers(context.Background()); e != nil {
		t.Fatal(e)
	}
	st, _ := s.readState()
	if _, ok := st.MediaPreferences[owner]; ok {
		t.Fatal("deleted account retained")
	}
	if _, e := os.Stat(filepath.Join(s.cookies().Home, "1001")); !os.IsNotExist(e) {
		t.Fatal("cookies retained", e)
	}
}
