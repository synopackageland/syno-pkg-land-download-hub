// Package binaries manages verified official executables in package-owned var.
// It never writes the installed SPK payload or calls a root service manager.
package binaries

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"dsmapp/internal/githubtoken"
)

type Selection struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}
type Runtime struct {
	Plugin   *PluginResult `json:"plugin,omitempty"`
	Active   Selection     `json:"active"`
	Previous *Selection    `json:"previous,omitempty"`
	Request  string        `json:"request,omitempty"`
	Error    string        `json:"error,omitempty"`
	Ready    bool          `json:"ready"`
	// Starting: this daemon has started the engine, which has not answered yet.
	Starting bool `json:"starting,omitempty"`
	// StartingSince is when Starting was recorded, in Unix milliseconds.
	StartingSince int64 `json:"startingSince,omitempty"`
}

// startingLimit bounds a starting record: the launcher's checks and the
// engine's startTimeout end well within it. An older one was left by a
// daemon that died while starting.
var startingLimit = 2 * time.Minute

// StartingAt reports whether, at now, a live daemon is starting the engine.
func (r Runtime) StartingAt(now time.Time) bool {
	age := now.Sub(time.UnixMilli(r.StartingSince))
	return r.Starting && r.StartingSince > 0 && age > -startingLimit && age < startingLimit
}

func (r *Runtime) beginStarting(now time.Time) {
	r.Ready, r.Starting, r.StartingSince = false, true, now.UnixMilli()
}

func (r *Runtime) endStarting() {
	r.Starting, r.StartingSince = false, 0
}

// MarkStarting records, before the launcher's own checks, that this daemon
// is starting. A first install has no record yet and starts an empty one.
func MarkStarting(data string) error {
	st, _ := ReadRuntime(data)
	st.beginStarting(time.Now())
	return Atomic(filepath.Join(data, "runtime.json"), st)
}

// EndStarting clears the starting record of a launcher that failed before
// its supervisor took over.
func EndStarting(data string) {
	if st, e := ReadRuntime(data); e == nil && st.Starting {
		st.endStarting()
		Atomic(filepath.Join(data, "runtime.json"), st)
	}
}

type Request struct {
	Plugin    *PluginUpdate `json:"plugin,omitempty"`
	ID        string        `json:"id"`
	Selection Selection     `json:"selection"`
}
type Release struct {
	Version    string `json:"version"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Arch       string `json:"arch"`
	Compatible bool   `json:"compatible"`
}
type Manager struct {
	Data string
	HTTP *http.Client
	Arch string
}

var versionRE = regexp.MustCompile(`^1\.9\.([0-9]{1,4})$`)

func Compatible(v string) bool {
	m := versionRE.FindStringSubmatch(v)
	if m == nil {
		return false
	}
	var n int
	fmt.Sscan(m[1], &n)
	return n >= 3
}
func Hash(p string) (string, error) {
	f, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, io.LimitReader(f, (128<<20)+1))
	return hex.EncodeToString(h.Sum(nil)), e
}
func Verify(data string, s Selection) error {
	if !Compatible(s.Version) || len(s.SHA256) != 64 || !filepath.IsAbs(s.Path) {
		return errors.New("invalid selection")
	}
	p, e := filepath.EvalSymlinks(s.Path)
	if e != nil || p != s.Path {
		return errors.New("unsafe selection path")
	}
	root := filepath.Join(data, "managed") + string(os.PathSeparator)
	if !strings.HasPrefix(p, root) {
		return errors.New("selection outside managed store")
	}
	h, e := Hash(p)
	if e != nil || h != s.SHA256 {
		return errors.New("binary verification failed")
	}
	return nil
}
func Atomic(p string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".state-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	f.Chmod(0600)
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e == nil {
		e = os.Rename(f.Name(), p)
	}
	if e == nil {
		d, x := os.Open(filepath.Dir(p))
		if x != nil {
			return x
		}
		defer d.Close()
		e = d.Sync()
	}
	return e
}
func ReadRuntime(data string) (Runtime, error) {
	var s Runtime
	b, e := os.ReadFile(filepath.Join(data, "runtime.json"))
	if e != nil {
		return s, e
	}
	if len(b) > 16384 || json.Unmarshal(b, &s) != nil {
		return s, errors.New("invalid runtime state")
	}
	return s, nil
}
func (m Manager) Lock(ctx context.Context) (func(), error) {
	if e := os.MkdirAll(m.Data, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(m.Data, "engine-operation.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	for {
		if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e == nil {
			return func() { syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
		}
		if e != syscall.EWOULDBLOCK {
			f.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}
func (m Manager) client() *http.Client {
	if m.HTTP != nil {
		return m.HTTP
	}
	return &http.Client{Timeout: 3 * time.Minute, Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second}, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 5 || !official(r.URL) {
			return errors.New("redirect refused")
		}
		githubtoken.Redirect(r)
		return nil
	}}
}
func official(u *url.URL) bool {
	return u.Scheme == "https" && u.User == nil && (u.Host == "api.github.com" || u.Host == "github.com" || u.Host == "release-assets.githubusercontent.com")
}
func (m Manager) get(ctx context.Context, u string, max int64) ([]byte, error) {
	if m.HTTP == nil {
		p, e := url.Parse(u)
		if e != nil || !official(p) {
			return nil, errors.New("source refused")
		}
	}
	r, e := http.NewRequestWithContext(ctx, "GET", u, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("Accept", "application/vnd.github+json")
	githubtoken.Authorize(r, githubtoken.At(m.Data).Load())
	resp, e := m.client().Do(r)
	if e != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, SourceError(resp)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, errors.New("source size limit")
	}
	return b, nil
}

// Release is Latest shared through the check cache. Gopeed publishes no
// checksum file, so the asset digest still comes from the release API.
func (m Manager) Release(ctx context.Context) (Release, error) {
	arch := m.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	return Cached(filepath.Join(m.Data, "release-check.json"), TokenKey("gopeed-"+arch, githubtoken.At(m.Data).Load()), func() (Release, error) { return m.Latest(ctx) })
}
func (m Manager) Latest(ctx context.Context) (Release, error) {
	var out Release
	b, e := m.get(ctx, "https://api.github.com/repos/GopeedLab/gopeed/releases/latest", 2<<20)
	if e != nil {
		return out, e
	}
	var meta struct {
		Tag        string `json:"tag_name"`
		Draft      bool
		Prerelease bool
		Assets     []struct {
			Name   string
			URL    string `json:"browser_download_url"`
			Digest string
		}
	}
	if json.Unmarshal(b, &meta) != nil || meta.Draft || meta.Prerelease {
		return out, errors.New("invalid stable release")
	}
	arch := m.Arch
	if arch == "" {
		arch = runtime.GOARCH
	}
	v := strings.TrimPrefix(meta.Tag, "v")
	out = Release{Version: v, Arch: arch, Compatible: Compatible(v)}
	name := "gopeed-web-v" + v + "-linux-" + arch + ".zip"
	for _, a := range meta.Assets {
		if a.Name == name {
			out.URL = a.URL
			out.SHA256 = strings.TrimPrefix(a.Digest, "sha256:")
		}
	}
	if out.URL != "https://github.com/GopeedLab/gopeed/releases/download/v"+v+"/"+name || len(out.SHA256) != 64 {
		return out, errors.New("official asset or digest unavailable")
	}
	if _, e = hex.DecodeString(out.SHA256); e != nil {
		return out, e
	}
	return out, nil
}
func Extract(b []byte, r Release, p string) error {
	sum := sha256.Sum256(b)
	if hex.EncodeToString(sum[:]) != r.SHA256 {
		return errors.New("archive checksum mismatch")
	}
	z, e := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if e != nil {
		return e
	}
	if len(z.File) > 256 {
		return errors.New("archive entries limit")
	}
	var selected *zip.File
	for _, f := range z.File {
		n := strings.TrimSuffix(f.Name, "/")
		if n == "" || path.Clean(n) != n || path.IsAbs(n) || n == ".." || strings.HasPrefix(n, "../") || strings.Contains(n, "\\") || f.Mode()&os.ModeSymlink != 0 {
			return errors.New("unsafe archive")
		}
		if path.Base(n) == "gopeed" && !f.FileInfo().IsDir() {
			if selected != nil || f.UncompressedSize64 == 0 || f.UncompressedSize64 > 128<<20 {
				return errors.New("invalid executable")
			}
			selected = f
		}
	}
	if selected == nil {
		return errors.New("missing executable")
	}
	src, e := selected.Open()
	if e != nil {
		return e
	}
	defer src.Close()
	f, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if e != nil {
		return e
	}
	_, e = io.Copy(f, io.LimitReader(src, (128<<20)+1))
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	exe, e := elf.Open(p)
	if e != nil {
		return errors.New("invalid ELF executable")
	}
	defer exe.Close()
	want := elf.EM_X86_64
	if r.Arch == "arm64" {
		want = elf.EM_AARCH64
	} else if r.Arch != "amd64" {
		return errors.New("unsupported architecture")
	}
	if exe.Machine != want || exe.Class != elf.ELFCLASS64 || exe.Data != elf.ELFDATA2LSB {
		return errors.New("ELF architecture mismatch")
	}
	return nil
}
func (m Manager) Install(ctx context.Context, r Release) (Selection, error) {
	var s Selection
	if !r.Compatible || !Compatible(r.Version) {
		return s, errors.New("update requires adapter compatibility upgrade")
	}
	b, e := m.get(ctx, r.URL, 256<<20)
	if e != nil {
		return s, e
	}
	dir, e := os.MkdirTemp(m.Data, ".candidate-*")
	if e != nil {
		return s, e
	}
	defer os.RemoveAll(dir)
	p := filepath.Join(dir, "gopeed")
	if e = Extract(b, r, p); e != nil {
		return s, e
	}
	h, e := Hash(p)
	if e != nil {
		return s, e
	}
	dest := filepath.Join(m.Data, "managed", r.Version+"-"+h[:16])
	if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return s, e
	}
	if e = os.Rename(dir, dest); e != nil {
		existing := Selection{Path: filepath.Join(dest, "gopeed"), Version: r.Version, SHA256: h}
		if Verify(m.Data, existing) != nil {
			return s, e
		}
	}
	s = Selection{Path: filepath.Join(dest, "gopeed"), Version: r.Version, SHA256: h}
	return s, Verify(m.Data, s)
}
func (m Manager) Activate(ctx context.Context, s Selection) (Runtime, error) {
	if e := Verify(m.Data, s); e != nil {
		st, x := ReadRuntime(m.Data)
		if x != nil || st.Previous == nil || *st.Previous != s {
			return Runtime{}, e
		}
		h, x := Hash(s.Path)
		if x != nil || h != s.SHA256 {
			return Runtime{}, e
		}
	}
	id := fmt.Sprintf("%d", time.Now().UnixNano())
	if e := Atomic(filepath.Join(m.Data, "engine-request.json"), Request{ID: id, Selection: s}); e != nil {
		return Runtime{}, e
	}
	timer := time.NewTimer(40 * time.Second)
	defer timer.Stop()
	for {
		st, e := ReadRuntime(m.Data)
		if e == nil && st.Request == id {
			if st.Error != "" {
				return st, errors.New(st.Error)
			}
			if !st.Ready {
				return st, errors.New("engine not ready")
			}
			return st, nil
		}
		select {
		case <-ctx.Done():
			return Runtime{}, ctx.Err()
		case <-timer.C:
			return Runtime{}, errors.New("activation uncertain; check status before retrying")
		case <-time.After(150 * time.Millisecond):
		}
	}
}
