package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const cookieFixture = "# Netscape HTTP Cookie File\n#HttpOnly_.video.example\tTRUE\t/\tTRUE\t0\tsession\tsecret-video\n.other.example\tTRUE\t/\tFALSE\t0\tid\tsecret-other\n"

func TestCookiesMetadataIsolationPermissionsAndFilteredCopy(t *testing.T) {
	s := Cookies{Home: filepath.Join(t.TempDir(), "cookies")}
	a := "1001:alice"
	b := "1002:bob"
	out, e := s.Import(a, cookieFixture)
	if e != nil || len(out.Domains) != 2 {
		t.Fatal(out, e)
	}
	for p, mode := range map[string]os.FileMode{s.Home: 0700, filepath.Join(s.Home, "1001"): 0700, filepath.Join(s.Home, "1001/cookies.json"): 0600} {
		st, e := os.Stat(p)
		if e != nil || st.Mode().Perm() != mode {
			t.Fatal(p, st, e)
		}
	}
	status, e := s.Status(b)
	if e != nil || len(status.Domains) != 0 {
		t.Fatal(status, e)
	}
	if _, e = s.Status("1001:replacement"); e == nil {
		t.Fatal("reused UID exposed prior account")
	}
	p, clean, e := s.Copy(a, "https://www.video.example/watch", t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	raw, e := os.ReadFile(p)
	if e != nil || !strings.Contains(string(raw), "secret-video") || strings.Contains(string(raw), "secret-other") {
		t.Fatal(string(raw), e)
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
	clean()
	if _, e = os.Stat(p); !os.IsNotExist(e) {
		t.Fatal("copy retained")
	}
	out, e = s.Delete(a, "video.example")
	if e != nil || len(out.Domains) != 1 || out.Domains[0].Domain != "other.example" {
		t.Fatal(out, e)
	}
	if _, e = s.Delete(a, ""); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(s.Home, "1001")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
}
func TestCookiesRejectMalformedAndSymlinks(t *testing.T) {
	s := Cookies{Home: filepath.Join(t.TempDir(), "cookies")}
	for _, txt := range []string{"session=secret", strings.Repeat("x", MaxCookieBytes+1), "# Netscape HTTP Cookie File\n.com\tTRUE\t/\tFALSE\t0\tid\tx", strings.ReplaceAll(cookieFixture, "\tTRUE\t/", "\tMAYBE\t/")} {
		if _, e := s.Import("1001:alice", txt); e == nil {
			t.Fatal("accepted invalid input")
		}
	}
	os.MkdirAll(s.Home, 0700)
	os.Symlink(t.TempDir(), filepath.Join(s.Home, "1001"))
	if _, e := s.Import("1001:alice", cookieFixture); e == nil {
		t.Fatal("followed UID symlink")
	}
}

func TestDeletedAccountCleanupDoesNotEraseAReusedUIDJar(t *testing.T) {
	s := Cookies{Home: filepath.Join(t.TempDir(), "cookies")}
	s.Import("1001:replacement", cookieFixture)
	if e := s.Purge("1001:deleted"); e != nil {
		t.Fatal(e)
	}
	out, e := s.Status("1001:replacement")
	if e != nil || len(out.Domains) != 2 {
		t.Fatal(out, e)
	}
}
