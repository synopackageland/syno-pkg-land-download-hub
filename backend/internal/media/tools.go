package media

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
	"github.com/ulikunitz/xz"
)

// Spec describes one official upstream release channel. Members are the only
// archive entries kept; the first is the executable the downloader runs.
// Sums names the release's checksum file; AssetSums is the suffix of a
// per-asset checksum file. List channels are read from the release API.
type Spec struct {
	Repo      string
	Assets    map[string]*regexp.Regexp
	Sums      string
	AssetSums string
	Archive   string
	Members   []string
	Max       int64
	Tag       *regexp.Regexp
	List      bool
}

var Specs = map[string]Spec{
	"gallery-dl": {Repo: "mikf/gallery-dl", Assets: map[string]*regexp.Regexp{"amd64": regexp.MustCompile(`^gallery-dl\.bin$`)}, Archive: "raw", Members: []string{"gallery-dl"}, Max: 96 << 20, Tag: regexp.MustCompile(`^v([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})$`), List: true},
	"yt-dlp":     {Repo: "yt-dlp/yt-dlp", Assets: map[string]*regexp.Regexp{"amd64": regexp.MustCompile(`^yt-dlp_linux$`), "arm64": regexp.MustCompile(`^yt-dlp_linux_aarch64$`)}, Sums: "SHA2-256SUMS", Archive: "raw", Members: []string{"yt-dlp"}, Max: 96 << 20, Tag: regexp.MustCompile(`^([0-9]{4}\.[0-9]{2}\.[0-9]{2}(?:\.[0-9]{1,6})?)$`)},
	// Immutable autobuild-* tags pin one build; the rolling "latest" tag is ignored.
	// GitHub's releases/latest names that rolling tag, so this list stays on the API.
	"ffmpeg": {Repo: "yt-dlp/FFmpeg-Builds", Assets: map[string]*regexp.Regexp{"amd64": regexp.MustCompile(`^ffmpeg-(N-[0-9]{1,9}-g[0-9a-f]{6,40})-linux64-gpl\.tar\.xz$`), "arm64": regexp.MustCompile(`^ffmpeg-(N-[0-9]{1,9}-g[0-9a-f]{6,40})-linuxarm64-gpl\.tar\.xz$`)}, Sums: "checksums.sha256", Archive: "tar.xz", Members: []string{"ffmpeg", "ffprobe"}, Max: 384 << 20, Tag: regexp.MustCompile(`^autobuild-([0-9]{4}-[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{2})$`), List: true},
	"deno":   {Repo: "denoland/deno", Assets: map[string]*regexp.Regexp{"amd64": regexp.MustCompile(`^deno-x86_64-unknown-linux-gnu\.zip$`), "arm64": regexp.MustCompile(`^deno-aarch64-unknown-linux-gnu\.zip$`)}, AssetSums: ".sha256sum", Archive: "zip", Members: []string{"deno"}, Max: 192 << 20, Tag: regexp.MustCompile(`^v([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})$`)},
}

type Release struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Asset   string `json:"asset"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Size    int64  `json:"size"`
	Arch    string `json:"arch"`
	// Expect is the text the installed executable must report for its version.
	Expect string `json:"expect"`
}

type Installed struct {
	Version string            `json:"version"`
	Dir     string            `json:"dir"`
	Files   map[string]string `json:"files"`
	Sizes   map[string]int64  `json:"sizes"`
}
type ToolState struct {
	Active   *Installed `json:"active,omitempty"`
	Previous *Installed `json:"previous,omitempty"`
	Job      *ToolJob   `json:"job,omitempty"`
}
type ToolsState struct {
	Tools   map[string]*ToolState `json:"tools"`
	Request string                `json:"request,omitempty"`
	Tool    string                `json:"tool,omitempty"`
	Error   string                `json:"error,omitempty"`
	// ResetAt is when GitHub lifts the limit named by Error, RFC 3339.
	ResetAt string `json:"resetAt,omitempty"`
}
type ToolRequest struct {
	ID      string   `json:"id"`
	Status  string   `json:"status,omitempty"`
	Tool    string   `json:"tool"`
	Op      string   `json:"op"`
	Release *Release `json:"release,omitempty"`
}

// Tools manages administrator-approved executables under the package-owned
// media home. Installs run in the daemon; the CGI only submits requests.
type Tools struct {
	Home  string
	API   string
	Web   string
	Hosts []string
	Arch  string
	HTTP  *http.Client
	// Check runs the candidate's version command; tests replace it.
	Check func(ctx context.Context, tool, exe string, r Release) error
	// GitHub holds the administrator's optional token; zero is anonymous.
	GitHub githubtoken.Store
}

var githubHosts = []string{"api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com"}

func (t Tools) dir() string { return filepath.Join(t.Home, "tools") }
func (t Tools) Supported(tool string) bool {
	spec, ok := Specs[tool]
	return ok && spec.Assets[t.arch()] != nil
}

func (t Tools) arch() string {
	if t.Arch != "" {
		return t.Arch
	}
	return runtime.GOARCH
}
func (t Tools) allowed(u *url.URL) bool {
	hosts := t.Hosts
	if hosts == nil {
		hosts = githubHosts
	}
	return u.User == nil && (u.Scheme == "https" || t.Hosts != nil) && slices.Contains(hosts, u.Host)
}
func (t Tools) client() *http.Client {
	if t.HTTP != nil {
		return t.HTTP
	}
	return &http.Client{Timeout: 30 * time.Minute, Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !t.allowed(r.URL) {
			return errors.New("redirect refused")
		}
		githubtoken.Redirect(r)
		return nil
	}}
}

func (t Tools) open(ctx context.Context, raw string) (*http.Response, error) {
	u, err := url.Parse(raw)
	if err != nil || !t.allowed(u) {
		return nil, errors.New("source refused")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", raw, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	githubtoken.Authorize(req, t.GitHub.Load())
	resp, err := t.client().Do(req)
	if err != nil {
		return nil, binaries.ErrUnavailable
	}
	if resp.StatusCode != 200 {
		defer resp.Body.Close()
		return nil, binaries.SourceError(resp)
	}
	return resp, nil
}

func (t Tools) get(ctx context.Context, raw string, max int64) ([]byte, error) {
	resp, err := t.open(ctx, raw)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, errors.New("source size limit")
	}
	return b, nil
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"assets"`
}

func (t Tools) api() string {
	if t.API != "" {
		return t.API
	}
	return "https://api.github.com"
}

func (t Tools) web() string {
	if t.Web != "" {
		return t.Web
	}
	return "https://github.com"
}

// Release is Latest shared through the check cache, so every tool, user and
// page refresh within an hour costs GitHub one call.
func (t Tools) Release(ctx context.Context, tool string) (Release, error) {
	return binaries.Cached(filepath.Join(t.dir(), "checks.json"), binaries.TokenKey(tool+"-"+t.arch(), t.GitHub.Load()), func() (Release, error) { return t.Latest(ctx, tool) })
}

// Latest returns the newest stable official asset for this architecture.
// Its digest must be listed in the project's own checksum file.
func (t Tools) Latest(ctx context.Context, tool string) (Release, error) {
	spec, ok := Specs[tool]
	if !ok {
		return Release{}, errors.New("unknown tool")
	}
	pattern := spec.Assets[t.arch()]
	if pattern == nil {
		return Release{}, errors.New("unsupported architecture")
	}
	if !spec.List {
		return t.latestTagged(ctx, tool, spec, pattern)
	}
	var candidates []ghRelease
	pageSize := "10"
	if tool == "gallery-dl" {
		pageSize = "100"
	}
	b, err := t.get(ctx, t.api()+"/repos/"+spec.Repo+"/releases?per_page="+pageSize, 4<<20)
	if err != nil {
		return Release{}, err
	}
	if json.Unmarshal(b, &candidates) != nil {
		return Release{}, errors.New("release metadata unavailable")
	}
	for _, rel := range candidates {
		tag := spec.Tag.FindStringSubmatch(rel.Tag)
		if rel.Draft || rel.Prerelease || tag == nil {
			continue
		}
		out := Release{Tool: tool, Version: tag[1], Arch: t.arch()}
		var sums string
		for _, a := range rel.Assets {
			if m := pattern.FindStringSubmatch(a.Name); m != nil {
				out.Asset, out.URL, out.SHA256, out.Size = a.Name, a.URL, strings.TrimPrefix(a.Digest, "sha256:"), a.Size
				if len(m) > 1 {
					out.Expect = m[1]
				}
			}
			if spec.Sums != "" && a.Name == spec.Sums {
				sums = a.URL
			}
		}
		if out.Asset == "" {
			continue
		}
		if b, err := hex.DecodeString(out.SHA256); err != nil || len(b) != 32 || out.Size <= 0 || out.Size > spec.Max {
			return Release{}, errors.New("official asset or digest unavailable")
		}
		if tool == "gallery-dl" {
			out.Expect = out.Version
			return out, nil
		}
		b, err := t.get(ctx, sums, 64<<10)
		if err != nil {
			return Release{}, err
		}
		if !strings.EqualFold(sumOf(b, out.Asset), out.SHA256) {
			return Release{}, errors.New("checksum file mismatch")
		}
		return out, nil
	}
	return Release{}, errors.New("no stable release")
}

// latestTagged reads the stable tag from GitHub's releases/latest redirect
// and the digest from the release's checksum file, both outside the API
// quota. The asset name is fixed, so no release metadata is needed.
func (t Tools) latestTagged(ctx context.Context, tool string, spec Spec, pattern *regexp.Regexp) (Release, error) {
	name, fixed := pattern.LiteralPrefix()
	if !fixed {
		return Release{}, errors.New("asset name not fixed")
	}
	tag, err := binaries.LatestTag(ctx, t.client(), t.web(), spec.Repo, t.GitHub.Load())
	if err != nil {
		return Release{}, err
	}
	m := spec.Tag.FindStringSubmatch(tag)
	if m == nil {
		return Release{}, errors.New("no stable release")
	}
	dl := t.web() + "/" + spec.Repo + "/releases/download/" + url.PathEscape(tag) + "/"
	out := Release{Tool: tool, Version: m[1], Arch: t.arch(), Asset: name, URL: dl + name}
	sums := dl + spec.Sums
	if spec.AssetSums != "" {
		sums = dl + name + spec.AssetSums
	}
	b, err := t.get(ctx, sums, 64<<10)
	if err != nil {
		return Release{}, err
	}
	out.SHA256 = strings.ToLower(sumOf(b, name))
	if b, err := hex.DecodeString(out.SHA256); err != nil || len(b) != 32 {
		return Release{}, errors.New("official asset or digest unavailable")
	}
	switch tool {
	case "yt-dlp":
		out.Expect = out.Version
	case "deno":
		out.Expect = "deno " + out.Version
	}
	return out, nil
}

// sumOf returns name's digest from a sha256sum-style checksum file.
func sumOf(sums []byte, name string) string {
	s := bufio.NewScanner(bytes.NewReader(sums))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return f[0]
		}
	}
	return ""
}

// Install downloads, verifies and unpacks a release into a new immutable
// directory. It never changes which version is active.
func (t Tools) Install(ctx context.Context, r Release) (Installed, error) {
	spec, ok := Specs[r.Tool]
	if !ok || r.Arch != t.arch() || spec.Assets[r.Arch] == nil || !spec.Assets[r.Arch].MatchString(r.Asset) || len(r.SHA256) != 64 {
		return Installed{}, errors.New("invalid release")
	}
	root := filepath.Join(t.dir(), r.Tool)
	if err := os.MkdirAll(root, 0700); err != nil {
		return Installed{}, err
	}
	tmp, err := os.MkdirTemp(root, ".candidate-*")
	if err != nil {
		return Installed{}, err
	}
	defer os.RemoveAll(tmp)
	archive := filepath.Join(tmp, ".download")
	if err = t.download(ctx, r, spec.Max, archive); err != nil {
		return Installed{}, jobFailure("tool_download_failed", err)
	}
	out := filepath.Join(tmp, "bin")
	if err = os.Mkdir(out, 0700); err != nil {
		return Installed{}, err
	}
	if err = unpack(spec, archive, out); err != nil {
		return Installed{}, jobFailure("tool_archive_invalid", err)
	}
	return t.admit(ctx, r, spec, root, out)
}

// admit checks every member of a candidate folder and moves it to its
// immutable versioned location. Refused candidates are left for removal.
func (t Tools) admit(ctx context.Context, r Release, spec Spec, root, out string) (Installed, error) {
	got := Installed{Version: r.Version, Files: map[string]string{}, Sizes: map[string]int64{}}
	var err error
	for _, name := range spec.Members {
		p := filepath.Join(out, name)
		if err = checkELF(p, r.Arch); err != nil {
			return Installed{}, jobFailure("tool_binary_invalid", err)
		}
		if got.Files[name], err = binaries.Hash(p); err != nil {
			return Installed{}, err
		}
		info, _ := os.Stat(p)
		got.Sizes[name] = info.Size()
		if err = os.Chmod(p, 0500); err != nil {
			return Installed{}, err
		}
	}
	if got.Files[spec.Members[0]] != r.SHA256 && spec.Archive == "raw" {
		return Installed{}, jobFailure("tool_checksum_failed", errors.New("checksum mismatch"))
	}
	check := t.Check
	if check == nil {
		check = t.versionCheck
	}
	if err = check(ctx, r.Tool, filepath.Join(out, spec.Members[0]), r); err != nil {
		return Installed{}, jobFailure("tool_version_failed", err)
	}
	got.Dir = filepath.Join(root, r.Version+"-"+got.Files[spec.Members[0]][:16])
	if err = os.Rename(out, got.Dir); err != nil && verify(got, spec) != nil {
		return Installed{}, jobFailure("tool_activate_failed", err)
	}
	return got, verify(got, spec)
}

// Check is the administrator's update view for one tool.
type Check struct {
	Current string  `json:"current,omitempty"`
	Latest  Release `json:"latest"`
	Update  bool    `json:"update"`
}

func (t Tools) CheckUpdate(ctx context.Context, tool string) (Check, error) {
	latest, err := t.Release(ctx, tool)
	if err != nil {
		return Check{}, err
	}
	c := Check{Latest: latest, Update: true}
	if a := t.State().Tools[tool].Active; a != nil {
		c.Current = a.Version
		c.Update = a.Files[Specs[tool].Members[0]] != latest.SHA256 && a.Version != latest.Version
	}
	return c, nil
}

func (t Tools) download(ctx context.Context, r Release, max int64, dest string) error {
	resp, err := t.open(ctx, r.URL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, max+1))
	if err != nil {
		return err
	}
	if n > max {
		return errors.New("download too large")
	}
	if hex.EncodeToString(h.Sum(nil)) != r.SHA256 {
		return jobFailure("tool_checksum_failed", errors.New("download checksum mismatch"))
	}
	return f.Sync()
}

func unpack(spec Spec, archive, out string) error {
	want := map[string]bool{}
	for _, m := range spec.Members {
		want[m] = true
	}
	found := map[string]bool{}
	take := func(name string, r io.Reader) error {
		if !want[name] || found[name] {
			return errors.New("unexpected executable entry")
		}
		found[name] = true
		f, err := os.OpenFile(filepath.Join(out, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
		if err != nil {
			return err
		}
		n, err := io.Copy(f, io.LimitReader(r, spec.Max+1))
		if err == nil && n > spec.Max {
			err = errors.New("executable too large")
		}
		if err == nil {
			err = f.Sync()
		}
		if ce := f.Close(); err == nil {
			err = ce
		}
		return err
	}
	src, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer src.Close()
	switch spec.Archive {
	case "raw":
		err = take(spec.Members[0], src)
	case "zip":
		info, _ := src.Stat()
		z, e := zip.NewReader(src, info.Size())
		if e != nil || len(z.File) > 64 {
			return errors.New("invalid archive")
		}
		for _, f := range z.File {
			if !safeEntry(f.Name) || f.Mode()&os.ModeType != 0 {
				return errors.New("unsafe archive")
			}
			r, e := f.Open()
			if e != nil {
				return e
			}
			e = take(path.Base(f.Name), r)
			r.Close()
			if e != nil {
				return e
			}
		}
	case "tar.xz":
		x, e := xz.NewReader(bufio.NewReader(src))
		if e != nil {
			return errors.New("invalid archive")
		}
		tr := tar.NewReader(x)
		for entries := 0; ; entries++ {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil || entries > 4096 || !safeEntry(h.Name) {
				return errors.New("unsafe archive")
			}
			switch h.Typeflag {
			case tar.TypeDir:
			case tar.TypeReg:
				// Only bin/<member> is an executable; docs and other tools are skipped.
				dir, name := path.Split(strings.TrimSuffix(h.Name, "/"))
				if path.Base(path.Clean(dir)) == "bin" && want[name] {
					if e = take(name, tr); e != nil {
						return e
					}
				}
			default:
				return errors.New("unsafe archive")
			}
		}
	default:
		return errors.New("unknown archive")
	}
	if err != nil {
		return err
	}
	if len(found) != len(want) {
		return errors.New("missing executable")
	}
	return nil
}

func safeEntry(n string) bool {
	n = strings.TrimSuffix(n, "/")
	return n != "" && path.Clean(n) == n && !path.IsAbs(n) && n != ".." && !strings.HasPrefix(n, "../") && !strings.Contains(n, "\\")
}

func checkELF(p, arch string) error {
	f, err := elf.Open(p)
	if err != nil {
		return errors.New("invalid ELF executable")
	}
	defer f.Close()
	want := map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64}[arch]
	if want == 0 || f.Machine != want || f.Class != elf.ELFCLASS64 || f.Data != elf.ELFDATA2LSB {
		return errors.New("ELF architecture mismatch")
	}
	return nil
}

func (t Tools) versionCheck(ctx context.Context, tool, exe string, r Release) error {
	flag := "--version"
	if tool == "ffmpeg" {
		flag = "-version"
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	out, _, err := t.run(ctx, exe, []string{flag}, 64<<10)
	if err != nil || r.Expect == "" || !strings.Contains(string(out), r.Expect) {
		return errors.New("version mismatch")
	}
	return nil
}

// run gives a tool the package's private home. DSM's /tmp is noexec, so
// PyInstaller builds need that TMPDIR to exist before they start.
func (t Tools) run(ctx context.Context, exe string, args []string, max int64) ([]byte, []byte, error) {
	if err := PrepareHome(t.Home); err != nil {
		return nil, nil, err
	}
	return Run(ctx, exe, args, Environ(t.Home), max)
}

// verify re-hashes every member; it runs before activation and rollback.
func verify(i Installed, spec Spec) error {
	for _, name := range spec.Members {
		p := filepath.Join(i.Dir, name)
		if real, err := filepath.EvalSymlinks(p); err != nil || real != p {
			return errors.New("unsafe tool path")
		}
		if h, err := binaries.Hash(p); err != nil || h != i.Files[name] {
			return errors.New("tool verification failed")
		}
	}
	return nil
}

func (t Tools) readState() ToolsState {
	st := ToolsState{Tools: map[string]*ToolState{}}
	if b, err := os.ReadFile(filepath.Join(t.dir(), "state.json")); err == nil && len(b) <= 64<<10 {
		json.Unmarshal(b, &st)
	}
	if st.Tools == nil {
		st.Tools = map[string]*ToolState{}
	}
	for name := range Specs {
		if st.Tools[name] == nil {
			st.Tools[name] = &ToolState{}
		}
	}
	if cur := st.Tools[st.Tool]; cur != nil && cur.Job == nil && st.Request != "" {
		status := "succeeded"
		if st.Error != "" {
			status = "failed"
		}
		cur.Job = &ToolJob{Request: st.Request, Status: status, Error: st.Error, ResetAt: st.ResetAt}
	}
	return st
}

// prune removes versions that are neither active nor previous. A download
// started two updates ago may lose ffmpeg; yt-dlp itself keeps running.
func (t Tools) prune(tool string, keep *ToolState) {
	root := filepath.Join(t.dir(), tool)
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if strings.HasPrefix(e.Name(), ".") || keep == nil || keep.Active != nil && keep.Active.Dir == dir || keep.Previous != nil && keep.Previous.Dir == dir {
			continue
		}
		os.Chmod(dir, 0700)
		os.RemoveAll(dir)
	}
}

// Env returns the active tools whose files still match the recorded size.
// Full digests are checked at install and rollback; a changed file is dropped.
func (t Tools) Env() Env {
	env := Env{Home: t.Home}
	st := t.State()
	path := func(tool string) string {
		a := st.Tools[tool].Active
		if a == nil || !strings.HasPrefix(a.Dir, filepath.Join(t.dir(), tool)+string(os.PathSeparator)) {
			return ""
		}
		for _, name := range Specs[tool].Members {
			info, err := os.Lstat(filepath.Join(a.Dir, name))
			if err != nil || !info.Mode().IsRegular() || info.Size() != a.Sizes[name] {
				return ""
			}
		}
		return a.Dir
	}
	if t.Supported("gallery-dl") {
		if d := path("gallery-dl"); d != "" {
			env.GalleryDlp = filepath.Join(d, "gallery-dl")
		}
	}
	if d := path("yt-dlp"); d != "" {
		env.YtDlp = filepath.Join(d, "yt-dlp")
	}
	env.FFmpegDir = path("ffmpeg")
	if d := path("deno"); d != "" {
		env.Deno = filepath.Join(d, "deno")
	}
	return env
}
