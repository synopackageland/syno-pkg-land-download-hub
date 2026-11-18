package media

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
	"github.com/ulikunitz/xz"
)

func elfBody(machine byte) []byte {
	b := make([]byte, 64)
	copy(b, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1})
	b[16], b[18], b[20], b[52] = 2, machine, 1, 64
	return b
}

func digest(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

type asset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}
type release struct {
	Tag        string  `json:"tag_name"`
	Draft      bool    `json:"draft"`
	Prerelease bool    `json:"prerelease"`
	Assets     []asset `json:"assets"`
}

// fixture serves GitHub-shaped releases from one server: the releases/latest
// redirect and download paths of github.com, and the release API.
type fixture struct {
	srv      *httptest.Server
	files    map[string][]byte
	latest   map[string]release
	releases map[string][]release
	// api counts release API calls; pages counts release pages fetched.
	api, pages int
	limit      bool
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{files: map[string][]byte{}, latest: map[string]release{}, releases: map[string][]release{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.limit {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1791943382")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		if b, ok := f.files[r.URL.Path]; ok {
			w.Write(b)
			return
		}
		if strings.Contains(r.URL.Path, "/releases/tag/") {
			f.pages++
		}
		if repo, ok := strings.CutPrefix(r.URL.Path, "/repos/"); ok {
			f.api++
			if strings.HasSuffix(repo, "/releases/latest") {
				json.NewEncoder(w).Encode(f.latest[strings.TrimSuffix(repo, "/releases/latest")])
				return
			}
			if strings.HasSuffix(repo, "/releases") {
				json.NewEncoder(w).Encode(f.releases[strings.TrimSuffix(repo, "/releases")])
				return
			}
		}
		if repo, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/releases/latest"); ok {
			if rel, ok := f.latest[repo]; ok {
				http.Redirect(w, r, "/"+repo+"/releases/tag/"+rel.Tag, http.StatusFound)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) asset(repo, tag, name string, body []byte) asset {
	p := "/" + repo + "/releases/download/" + tag + "/" + name
	f.files[p] = body
	return asset{Name: name, URL: f.srv.URL + p, Digest: "sha256:" + digest(body), Size: len(body)}
}

func (f *fixture) tools(t *testing.T, arch string) Tools {
	u, _ := url.Parse(f.srv.URL)
	data, _ := filepath.EvalSymlinks(t.TempDir())
	return Tools{Home: data, API: f.srv.URL, Web: f.srv.URL, Hosts: []string{u.Host}, Arch: arch, HTTP: f.srv.Client(), Check: func(context.Context, string, string, Release) error { return nil }}
}

func (f *fixture) ytdlp(tag string, body []byte, sums string) {
	a := f.asset("yt-dlp/yt-dlp", tag, "yt-dlp_linux", body)
	if sums == "" {
		sums = digest(body) + "  yt-dlp_linux\n" + digest([]byte("x")) + "  yt-dlp_linux_aarch64\n"
	}
	s := f.asset("yt-dlp/yt-dlp", tag, "SHA2-256SUMS", []byte(sums))
	f.latest["yt-dlp/yt-dlp"] = release{Tag: tag, Assets: []asset{a, s}}
}

func TestLatestYtDlpReadsTheRedirectAndChecksumFile(t *testing.T) {
	f := newFixture(t)
	body := elfBody(62)
	f.ytdlp("2026.08.19", body, "")
	tools := f.tools(t, "amd64")
	r, err := tools.Latest(context.Background(), "yt-dlp")
	if err != nil || r.Version != "2026.08.19" || r.SHA256 != digest(body) || r.Asset != "yt-dlp_linux" || r.Tool != "yt-dlp" || r.URL != f.srv.URL+"/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp_linux" {
		t.Fatalf("%+v %v", r, err)
	}
	if f.api != 0 || f.pages != 0 {
		t.Fatalf("api calls %d, release pages %d", f.api, f.pages)
	}
	f.ytdlp("2026.08.19", body, digest([]byte("other"))+"  yt-dlp_linux\n")
	if r, err = tools.Latest(context.Background(), "yt-dlp"); err != nil {
		t.Fatal(err)
	}
	if _, err = tools.Install(context.Background(), r); err == nil {
		t.Fatal("asset that disagrees with the checksum file accepted")
	}
	f.ytdlp("2026.08.19", body, digest(body)+"  yt-dlp_linux_aarch64\n")
	if _, err = tools.Latest(context.Background(), "yt-dlp"); err == nil {
		t.Fatal("asset missing from the checksum file accepted")
	}
	f.ytdlp("nightly", body, "")
	if _, err = tools.Latest(context.Background(), "yt-dlp"); err == nil {
		t.Fatal("unversioned tag accepted")
	}
	if _, err = tools.Latest(context.Background(), "curl"); err == nil {
		t.Fatal("unknown tool accepted")
	}
}

// GitHub's releases/latest skips drafts and prereleases, so a newer
// prerelease never replaces the stable release the redirect names.
func TestLatestIgnoresANewerPrerelease(t *testing.T) {
	f := newFixture(t)
	stable := elfBody(62)
	f.ytdlp("2026.08.19", stable, "")
	pre := f.asset("yt-dlp/yt-dlp", "2026.09.01", "yt-dlp_linux", append(elfBody(62), 9))
	f.releases["yt-dlp/yt-dlp"] = []release{{Tag: "2026.09.01", Prerelease: true, Assets: []asset{pre}}, f.latest["yt-dlp/yt-dlp"]}
	if r, err := f.tools(t, "amd64").Latest(context.Background(), "yt-dlp"); err != nil || r.Version != "2026.08.19" || r.SHA256 != digest(stable) {
		t.Fatalf("%+v %v", r, err)
	}
	arm := tarXZ(t, map[string][]byte{"x/bin/ffmpeg": elfBody(183), "x/bin/ffprobe": elfBody(183)}, "")
	r := f.ffmpeg(t, "autobuild-2026-10-10-18-21", "N-1-gabcdef1", arm)
	name := "ffmpeg-N-2-gdef0123-linuxarm64-gpl.tar.xz"
	newer := release{Tag: "autobuild-2026-10-11-00-00", Prerelease: true, Assets: []asset{f.asset("yt-dlp/FFmpeg-Builds", "autobuild-2026-10-11-00-00", name, arm), f.asset("yt-dlp/FFmpeg-Builds", "autobuild-2026-10-11-00-00", "checksums.sha256", []byte(digest(arm)+"  "+name+"\n"))}}
	f.releases["yt-dlp/FFmpeg-Builds"] = append([]release{newer}, f.releases["yt-dlp/FFmpeg-Builds"]...)
	if got, err := f.tools(t, "arm64").Latest(context.Background(), "ffmpeg"); err != nil || got.Version != r.Version {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLatestSaysWhenGitHubLimitsOrIsUnreachable(t *testing.T) {
	f := newFixture(t)
	f.ytdlp("2026.08.19", elfBody(62), "")
	tools := f.tools(t, "amd64")
	f.limit = true
	for _, tool := range []string{"yt-dlp", "ffmpeg"} {
		var rl *binaries.RateLimitError
		if _, err := tools.Latest(context.Background(), tool); !errors.As(err, &rl) || rl.Reset.Unix() != 1791943382 {
			t.Fatal(tool, err)
		}
	}
	f.srv.Close()
	for _, tool := range []string{"yt-dlp", "ffmpeg"} {
		if _, err := tools.Latest(context.Background(), tool); !errors.Is(err, binaries.ErrUnavailable) {
			t.Fatal(tool, err)
		}
	}
}

func tarXZ(t *testing.T, entries map[string][]byte, link string) []byte {
	var b bytes.Buffer
	x, _ := xz.NewWriter(&b)
	w := tar.NewWriter(x)
	for name, body := range entries {
		w.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		w.Write(body)
	}
	if link != "" {
		w.WriteHeader(&tar.Header{Name: link, Linkname: "/etc/passwd", Typeflag: tar.TypeSymlink})
	}
	w.Close()
	x.Close()
	return b.Bytes()
}

func (f *fixture) ffmpeg(t *testing.T, tag, build string, body []byte) Release {
	name := "ffmpeg-" + build + "-linuxarm64-gpl.tar.xz"
	a := f.asset("yt-dlp/FFmpeg-Builds", tag, name, body)
	s := f.asset("yt-dlp/FFmpeg-Builds", tag, "checksums.sha256", []byte(digest(body)+"  "+name+"\n"))
	f.releases["yt-dlp/FFmpeg-Builds"] = []release{{Tag: "latest", Assets: []asset{f.asset("yt-dlp/FFmpeg-Builds", "latest", "ffmpeg-master-latest-linuxarm64-gpl.tar.xz", body)}}, {Tag: tag, Assets: []asset{a, s}}}
	r, err := f.tools(t, "arm64").Latest(context.Background(), "ffmpeg")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFFmpegInstallsOnlyItsExecutables(t *testing.T) {
	f := newFixture(t)
	arm := elfBody(183)
	good := tarXZ(t, map[string][]byte{"ffmpeg-N-1-gabcdef1/bin/ffmpeg": arm, "ffmpeg-N-1-gabcdef1/bin/ffprobe": arm, "ffmpeg-N-1-gabcdef1/bin/ffplay": arm, "ffmpeg-N-1-gabcdef1/doc/x.html": []byte("doc")}, "")
	r := f.ffmpeg(t, "autobuild-2026-10-10-18-21", "N-1-gabcdef1", good)
	if r.Version != "2026-10-10-18-21" || r.Expect != "N-1-gabcdef1" {
		t.Fatalf("%+v", r)
	}
	tools := f.tools(t, "arm64")
	got, err := tools.Install(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(got.Dir)
	if len(entries) != 2 || got.Files["ffmpeg"] != digest(arm) || got.Files["ffprobe"] != digest(arm) {
		t.Fatalf("%v %+v", entries, got)
	}
	if !strings.HasPrefix(got.Dir, filepath.Join(tools.Home, "tools", "ffmpeg")+"/") {
		t.Fatal(got.Dir)
	}
	for name, body := range map[string][]byte{
		"symlink":  tarXZ(t, map[string][]byte{"x/bin/ffmpeg": arm, "x/bin/ffprobe": arm}, "x/bin/evil"),
		"traverse": tarXZ(t, map[string][]byte{"../bin/ffmpeg": arm, "x/bin/ffprobe": arm}, ""),
		"missing":  tarXZ(t, map[string][]byte{"x/bin/ffmpeg": arm}, ""),
		"x86":      tarXZ(t, map[string][]byte{"x/bin/ffmpeg": elfBody(62), "x/bin/ffprobe": elfBody(62)}, ""),
		"dup":      tarXZ(t, map[string][]byte{"x/bin/ffmpeg": arm, "y/bin/ffmpeg": arm, "x/bin/ffprobe": arm}, ""),
	} {
		r := f.ffmpeg(t, "autobuild-2026-10-11-00-00", "N-2-gdef0123", body)
		if _, err := tools.Install(context.Background(), r); err == nil {
			t.Fatalf("%s archive accepted", name)
		}
	}
}

func TestInstallRefusesTamperedDownloadAndWrongArch(t *testing.T) {
	f := newFixture(t)
	body := elfBody(62)
	f.ytdlp("2026.08.19", body, "")
	tools := f.tools(t, "amd64")
	r, _ := tools.Latest(context.Background(), "yt-dlp")
	f.files["/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp_linux"] = elfBody(63)
	if _, err := tools.Install(context.Background(), r); err == nil {
		t.Fatal("tampered download accepted")
	}
	f.files["/yt-dlp/yt-dlp/releases/download/2026.08.19/yt-dlp_linux"] = body
	r.Arch = "arm64"
	if _, err := tools.Install(context.Background(), r); err == nil {
		t.Fatal("wrong architecture accepted")
	}
	r.Arch = "amd64"
	r.URL = "https://evil.example/yt-dlp_linux"
	if _, err := tools.Install(context.Background(), r); err == nil {
		t.Fatal("foreign host accepted")
	}
	r, _ = tools.Latest(context.Background(), "yt-dlp")
	tools.Check = func(context.Context, string, string, Release) error { return errors.New("version") }
	if _, err := tools.Install(context.Background(), r); err == nil {
		t.Fatal("version mismatch accepted")
	}
	if matches, _ := filepath.Glob(filepath.Join(tools.Home, "tools", "*", "*")); len(matches) != 0 {
		t.Fatalf("refused candidates left behind: %v", matches)
	}
}

func TestProcessInstallsActivatesAndRollsBack(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	ctx := context.Background()
	first := append(elfBody(62), 1)
	f.ytdlp("2026.08.19", first, "")
	r1, _ := tools.Latest(ctx, "yt-dlp")
	if err := tools.Submit(ToolRequest{ID: "1", Tool: "yt-dlp", Op: "install", Release: &r1}); err != nil {
		t.Fatal(err)
	}
	if !tools.Pending() {
		t.Fatal("request not pending")
	}
	tools.ProcessPending(ctx)
	st := tools.State()
	if tools.Pending() || st.Request != "1" || st.Error != "" || st.Tools["yt-dlp"].Active.Version != "2026.08.19" {
		t.Fatalf("%+v", st)
	}
	env := tools.Env()
	if env.YtDlp != filepath.Join(st.Tools["yt-dlp"].Active.Dir, "yt-dlp") || env.FFmpegDir != "" || env.Deno != "" || env.Home != tools.Home {
		t.Fatalf("%+v", env)
	}
	second := append(elfBody(62), 2)
	f.ytdlp("2026.09.01", second, "")
	r2, _ := tools.Latest(ctx, "yt-dlp")
	tools.Submit(ToolRequest{ID: "2", Tool: "yt-dlp", Op: "install", Release: &r2})
	tools.ProcessPending(ctx)
	st = tools.State()
	if st.Tools["yt-dlp"].Active.Version != "2026.09.01" || st.Tools["yt-dlp"].Previous.Version != "2026.08.19" {
		t.Fatalf("%+v", st.Tools["yt-dlp"])
	}
	tools.Submit(ToolRequest{ID: "3", Tool: "yt-dlp", Op: "rollback"})
	tools.ProcessPending(ctx)
	st = tools.State()
	if st.Request != "3" || st.Tools["yt-dlp"].Active.Version != "2026.08.19" || st.Tools["yt-dlp"].Previous.Version != "2026.09.01" {
		t.Fatalf("%+v", st.Tools["yt-dlp"])
	}
	tamper(t, filepath.Join(st.Tools["yt-dlp"].Previous.Dir, "yt-dlp"), "tampered")
	tools.Submit(ToolRequest{ID: "4", Tool: "yt-dlp", Op: "rollback"})
	tools.ProcessPending(ctx)
	st = tools.State()
	if st.Error == "" || st.Tools["yt-dlp"].Active.Version != "2026.08.19" {
		t.Fatalf("tampered rollback: %+v", st)
	}
	tamper(t, filepath.Join(st.Tools["yt-dlp"].Active.Dir, "yt-dlp"), "tampered-active")
	if tools.Env().YtDlp != "" {
		t.Fatal("changed active tool used")
	}
}

func TestSubmitValidatesRequest(t *testing.T) {
	tools := Tools{Home: t.TempDir()}
	for i, req := range []ToolRequest{{ID: "", Tool: "yt-dlp", Op: "rollback"}, {ID: "1", Tool: "sh", Op: "rollback"}, {ID: "1", Tool: "yt-dlp", Op: "exec"}, {ID: "1", Tool: "yt-dlp", Op: "install"}} {
		if tools.Submit(req) == nil {
			t.Fatal(i, req)
		}
	}
	if err := tools.Submit(ToolRequest{ID: "1", Tool: "deno", Op: "rollback"}); err != nil {
		t.Fatal(err)
	}
	if err := tools.Submit(ToolRequest{ID: "2", Tool: "deno", Op: "rollback"}); err == nil {
		t.Fatal("second request while one is pending")
	}
}

func TestDenoZip(t *testing.T) {
	f := newFixture(t)
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	w, _ := z.Create("deno")
	w.Write(elfBody(62))
	z.Close()
	name := "deno-x86_64-unknown-linux-gnu.zip"
	f.latest["denoland/deno"] = release{Tag: "v2.5.4", Assets: []asset{f.asset("denoland/deno", "v2.5.4", name, b.Bytes())}}
	f.asset("denoland/deno", "v2.5.4", name+".sha256sum", []byte(digest(b.Bytes())+"  "+name+"\n"))
	tools := f.tools(t, "amd64")
	r, err := tools.Latest(context.Background(), "deno")
	if err != nil || r.Version != "2.5.4" || r.Expect != "deno 2.5.4" || r.SHA256 != digest(b.Bytes()) || f.api != 0 {
		t.Fatalf("%+v %v api=%d", r, err, f.api)
	}
	got, err := tools.Install(context.Background(), r)
	if err != nil || got.Files["deno"] != digest(elfBody(62)) {
		t.Fatal(got, err)
	}
	_ = fmt.Sprint
}

func tamper(t *testing.T, p, body string) {
	os.Chmod(p, 0700)
	if err := os.WriteFile(p, []byte(body), 0700); err != nil {
		t.Fatal(err)
	}
}

func installedYtDlp(t *testing.T, f *fixture, tools *Tools, body []byte) {
	f.ytdlp("2026.08.19", body, "")
	r, _ := tools.Latest(context.Background(), "yt-dlp")
	tools.Submit(ToolRequest{ID: "install", Tool: "yt-dlp", Op: "install", Release: &r})
	tools.ProcessPending(context.Background())
	if tools.State().Tools["yt-dlp"].Active == nil {
		t.Fatal("not installed")
	}
}

func TestCheckUpdateReportsCurrentAndLatest(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	ctx := context.Background()
	old := append(elfBody(62), 1)
	installedYtDlp(t, f, &tools, old)
	c, err := tools.CheckUpdate(ctx, "yt-dlp")
	if err != nil || c.Current != "2026.08.19" || c.Latest.Version != "2026.08.19" || c.Update {
		t.Fatalf("%+v %v", c, err)
	}
	f.ytdlp("2026.09.01", append(elfBody(62), 2), "")
	if c, err = tools.CheckUpdate(ctx, "yt-dlp"); err != nil || c.Latest.Version != "2026.08.19" {
		t.Fatalf("a fresh check is not shared: %+v %v", c, err)
	}
	expire := func() { os.Remove(filepath.Join(tools.Home, "tools", "checks.json")) }
	expire()
	if c, err = tools.CheckUpdate(ctx, "yt-dlp"); err != nil || !c.Update || c.Latest.Version != "2026.09.01" {
		t.Fatalf("%+v %v", c, err)
	}
	expire()
	f.srv.Close()
	if _, err = tools.CheckUpdate(ctx, "yt-dlp"); err == nil {
		t.Fatal("network failure hidden")
	}
}

func TestYtDlpUpdateUsesVerifiedDownloadWithoutLaunchingOldUpdater(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	old := append(elfBody(62), 1)
	installedYtDlp(t, f, &tools, old)
	before := *tools.State().Tools["yt-dlp"].Active
	// The old binary cannot query the rate-limited API (or even execute).
	// Updating must use the package's already pinned official release instead.
	os.Chmod(filepath.Join(before.Dir, "yt-dlp"), 0400)
	newer := append(elfBody(62), 2)
	f.ytdlp("2026.10.26", newer, "")
	r, err := tools.Latest(ctx, "yt-dlp")
	if err != nil {
		t.Fatal(err)
	}
	if err = tools.Submit(ToolRequest{ID: "update", Tool: "yt-dlp", Op: "update", Release: &r}); err != nil {
		t.Fatal(err)
	}
	tools.ProcessPending(ctx)
	got := tools.State().Tools["yt-dlp"]
	if got.Job.Status != "succeeded" || got.Active.Version != r.Version || got.Previous.Dir != before.Dir {
		t.Fatalf("update failed without the old updater: %+v", got)
	}
	if b, _ := os.ReadFile(filepath.Join(before.Dir, "yt-dlp")); !bytes.Equal(b, old) {
		t.Fatal("old executable changed")
	}
	// A tampered download must retain the active version and report verification.
	f.files["/yt-dlp/yt-dlp/releases/download/2026.10.26/yt-dlp_linux"] = append(newer, 3)
	r.Version = "2026.10.27"
	tools.Submit(ToolRequest{ID: "bad", Tool: "yt-dlp", Op: "update", Release: &r})
	tools.ProcessPending(ctx)
	got = tools.State().Tools["yt-dlp"]
	if got.Job.Error != "tool_checksum_failed" || got.Active.Version != "2026.10.26" {
		t.Fatalf("%+v", got)
	}
}

func TestUpdateRequiresInstalledYtDlp(t *testing.T) {
	tools := Tools{Home: t.TempDir()}
	r := Release{Tool: "ffmpeg"}
	if tools.Submit(ToolRequest{ID: "1", Tool: "ffmpeg", Op: "update", Release: &r}) == nil {
		t.Fatal("ffmpeg has no self-update")
	}
}

func TestOnlyActiveAndPreviousVersionsAreKept(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	ctx := context.Background()
	var dirs []string
	for i, tag := range []string{"2026.08.19", "2026.09.01", "2026.10.01"} {
		f.ytdlp(tag, append(elfBody(62), byte(i)), "")
		r, _ := tools.Latest(ctx, "yt-dlp")
		tools.Submit(ToolRequest{ID: tag, Tool: "yt-dlp", Op: "install", Release: &r})
		tools.ProcessPending(ctx)
		dirs = append(dirs, tools.State().Tools["yt-dlp"].Active.Dir)
	}
	if _, err := os.Stat(dirs[0]); !os.IsNotExist(err) {
		t.Fatal("oldest version kept", err)
	}
	for _, dir := range dirs[1:] {
		if _, err := os.Stat(filepath.Join(dir, "yt-dlp")); err != nil {
			t.Fatal(err)
		}
	}
}

// DSM's /tmp is noexec, so PyInstaller tools need the package's own TMPDIR to
// exist before their first run (B3).
func TestVersionCheckRunsWithExistingPrivateTmpdir(t *testing.T) {
	tools := Tools{Home: t.TempDir()}
	out := filepath.Join(tools.Home, "tools", "yt-dlp", ".candidate-1", "bin")
	if err := os.MkdirAll(out, 0700); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(out, "yt-dlp")
	script := "#!/bin/sh\nset -e\necho \"$TMPDIR\" > \"$TMPDIR/seen\"\necho 2026.08.19\n"
	if err := os.WriteFile(exe, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := tools.versionCheck(context.Background(), "yt-dlp", exe, Release{Expect: "2026.08.19"}); err != nil {
		t.Fatal(err)
	}
	seen, err := os.ReadFile(filepath.Join(tools.Home, "tmp", "seen"))
	if err != nil || strings.TrimSpace(string(seen)) != filepath.Join(tools.Home, "tmp") {
		t.Fatalf("TMPDIR %q: %v", seen, err)
	}
}

type trip func(*http.Request) (*http.Response, error)

func (f trip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLatestSendsTheSavedTokenToGitHubAndSaysWhenItIsRejected(t *testing.T) {
	data := t.TempDir()
	store := githubtoken.At(data)
	token := "github_pat_" + strings.Repeat("B", 40)
	if err := store.Save(token); err != nil {
		t.Fatal(err)
	}
	var seen []string
	tools := Tools{Home: filepath.Join(data, "media"), Arch: "amd64", GitHub: store, HTTP: &http.Client{Transport: trip(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Host+" "+r.Header.Get("Authorization"))
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			h := http.Header{}
			h.Set("Location", "/denoland/deno/releases/tag/v2.4.0")
			return &http.Response{StatusCode: 302, Header: h, Body: http.NoBody, Request: r}, nil
		}
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})}}
	if _, err := tools.Latest(context.Background(), "deno"); !errors.Is(err, githubtoken.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if len(seen) != 2 || seen[0] != "github.com Bearer "+token || seen[1] != "github.com Bearer "+token {
		t.Fatalf("requests = %q", seen)
	}
}

func TestProcessNamesAGitHubLimitAndWhenItResets(t *testing.T) {
	f := newFixture(t)
	tools := f.tools(t, "amd64")
	ctx := context.Background()
	f.ytdlp("2026.08.19", append(elfBody(62), 1), "")
	r, _ := tools.Latest(ctx, "yt-dlp")
	f.limit = true
	tools.Submit(ToolRequest{ID: "1", Tool: "yt-dlp", Op: "install", Release: &r})
	tools.ProcessPending(ctx)
	if st := tools.State(); st.Error != "tool_source_rate_limited" || st.ResetAt != "2026-10-14T02:03:02Z" {
		t.Fatalf("%+v", st)
	}
	f.limit = false
	tools.Submit(ToolRequest{ID: "2", Tool: "yt-dlp", Op: "install", Release: &r})
	tools.ProcessPending(ctx)
	if st := tools.State(); st.Error != "" || st.ResetAt != "" {
		t.Fatalf("%+v", st)
	}
}
