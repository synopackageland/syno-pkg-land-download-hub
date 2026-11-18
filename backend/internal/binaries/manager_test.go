package binaries

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dsmapp/internal/githubtoken"
)

func archive(name string, mode os.FileMode, body []byte) []byte {
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	h := &zip.FileHeader{Name: name}
	h.SetMode(mode)
	w, _ := z.CreateHeader(h)
	w.Write(body)
	z.Close()
	return b.Bytes()
}
func TestArchiveAdmission(t *testing.T) {
	body := make([]byte, 64)
	copy(body, []byte{0x7f, 'E', 'L', 'F', 2, 1})
	body[18] = 62
	body[6] = 1
	body[16] = 2
	body[20] = 1
	body[52] = 64
	for _, tc := range []struct {
		name string
		mode os.FileMode
		ok   bool
	}{{"bundle/gopeed", 0700, true}, {"../gopeed", 0700, false}, {"bundle/gopeed", os.ModeSymlink | 0700, false}, {"bundle/other", 0700, false}} {
		t.Run(tc.name+tc.mode.String(), func(t *testing.T) {
			b := archive(tc.name, tc.mode, body)
			sum := sha256.Sum256(b)
			r := Release{Version: "1.9.3", SHA256: hex.EncodeToString(sum[:]), Arch: "amd64"}
			p := filepath.Join(t.TempDir(), "gopeed")
			e := Extract(b, r, p)
			if (e == nil) != tc.ok {
				t.Fatal(e)
			}
		})
	}
	b := archive("bundle/gopeed", 0700, body)
	if Extract(b, Release{Version: "1.9.3", SHA256: "wrong", Arch: "amd64"}, filepath.Join(t.TempDir(), "bad")) == nil {
		t.Fatal("unverified archive accepted")
	}
}
func TestSelectionTamperAndVersionFence(t *testing.T) {
	d := t.TempDir()
	d, _ = filepath.EvalSymlinks(d)
	p := filepath.Join(d, "managed", "1.9.3", "gopeed")
	os.MkdirAll(filepath.Dir(p), 0700)
	os.WriteFile(p, []byte("official"), 0700)
	h, _ := Hash(p)
	s := Selection{Path: p, Version: "1.9.3", SHA256: h}
	if Verify(d, s) != nil {
		t.Fatal("valid rejected")
	}
	os.WriteFile(p, []byte("changed"), 0700)
	if Verify(d, s) == nil {
		t.Fatal("tampered accepted")
	}
	for _, v := range []string{"2.0.0", "1.10.0", "1.9.2", "1.9.4-beta", "../1.9.4"} {
		if Compatible(v) {
			t.Fatal(v)
		}
	}
}
func TestManagerGateExcludesConcurrentMutations(t *testing.T) {
	m := Manager{Data: t.TempDir()}
	unlock, e := m.Lock(context.Background())
	if e != nil {
		t.Fatal(e)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := m.Lock(ctx); e == nil {
		t.Fatal("concurrent lock accepted")
	}
}
func TestExtractOfficialArchive(t *testing.T) {
	p := os.Getenv("GOPEED_TEST_ARCHIVE")
	if p == "" {
		t.Skip("official archive not selected")
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(b)
	if e = Extract(b, Release{Version: "1.9.3", Arch: "amd64", SHA256: hex.EncodeToString(h[:])}, filepath.Join(t.TempDir(), "gopeed")); e != nil {
		t.Fatal(e)
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func coreRelease(v string) string {
	name := "gopeed-web-v" + v + "-linux-amd64.zip"
	return `{"tag_name":"v` + v + `","assets":[{"name":"` + name + `","browser_download_url":"https://github.com/GopeedLab/gopeed/releases/download/v` + v + `/` + name + `","digest":"sha256:` + strings.Repeat("a", 64) + `"}]}`
}

func TestCoreReleaseCheckIsSharedAndSaysWhenRateLimited(t *testing.T) {
	calls, body, limited := 0, coreRelease("1.9.3"), false
	m := Manager{Data: t.TempDir(), Arch: "amd64", HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if limited {
			h := http.Header{}
			h.Set("X-RateLimit-Remaining", "0")
			h.Set("X-RateLimit-Reset", "1791943382")
			return &http.Response{StatusCode: 403, Header: h, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}}
	r, err := m.Release(context.Background())
	if err != nil || r.Version != "1.9.3" {
		t.Fatal(r, err)
	}
	body = coreRelease("1.9.4")
	if r, err = m.Release(context.Background()); err != nil || r.Version != "1.9.3" || calls != 1 {
		t.Fatal("check not shared", r, err, calls)
	}
	limited = true
	var rl *RateLimitError
	if _, err = m.Latest(context.Background()); !errors.As(err, &rl) || rl.Reset.Unix() != 1791943382 {
		t.Fatal(err)
	}
}

func TestCoreReleaseCheckSendsTheSavedTokenOnlyToGitHub(t *testing.T) {
	data := t.TempDir()
	token := "github_pat_" + strings.Repeat("A", 40)
	var seen []string
	m := Manager{Data: data, Arch: "amd64", HTTP: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Host+" "+r.Header.Get("Authorization"))
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "0")
		h.Set("X-RateLimit-Reset", "4102444800")
		return &http.Response{StatusCode: 403, Header: h, Body: io.NopCloser(strings.NewReader("{}"))}, nil
	})}}
	if _, err := m.Release(context.Background()); err == nil {
		t.Fatal("limit not reported")
	}
	if err := githubtoken.At(data).Save(token); err != nil {
		t.Fatal(err)
	}
	m.Release(context.Background())
	if len(seen) != 2 || seen[0] != "api.github.com " || seen[1] != "api.github.com Bearer "+token {
		t.Fatalf("requests = %q", seen)
	}
}
