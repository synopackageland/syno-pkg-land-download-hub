package media

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const MaxCookieBytes = 256 << 10

type CookieSummary struct {
	Domains    []CookieDomain `json:"domains"`
	ImportedAt time.Time      `json:"importedAt"`
}
type CookieDomain struct {
	Domain string `json:"domain"`
	Count  int    `json:"count"`
}
type cookieRecord struct {
	Owner      string    `json:"owner"`
	Text       string    `json:"text"`
	ImportedAt time.Time `json:"importedAt"`
}
type cookie struct{ domain, line string }

// Cookies is a UID-keyed package-private store. Public methods return metadata
// only. Owner includes the account name to refuse a reused UID.
type Cookies struct{ Home string }

func ownerUID(owner string) (string, error) {
	uid, name, ok := strings.Cut(owner, ":")
	n, e := strconv.ParseUint(uid, 10, 32)
	if !ok || e != nil || n == 0 || name == "" || strconv.FormatUint(n, 10) != uid {
		return "", &Error{"identity_unavailable"}
	}
	return uid, nil
}
func privateDir(p string) error {
	if e := os.MkdirAll(p, 0700); e != nil {
		return e
	}
	st, e := os.Lstat(p)
	if e != nil {
		return e
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return &Error{"media_cookies_invalid"}
	}
	return os.Chmod(p, 0700)
}
func (s Cookies) dir(owner string) (string, error) {
	uid, e := ownerUID(owner)
	if e != nil {
		return "", e
	}
	if e = privateDir(s.Home); e != nil {
		return "", e
	}
	p := filepath.Join(s.Home, uid)
	if e = privateDir(p); e != nil {
		return "", e
	}
	return p, nil
}
func cookieDomain(s string) string { return strings.ToLower(strings.TrimPrefix(s, ".")) }
func parseCookies(text string) ([]cookie, error) {
	if len(text) > MaxCookieBytes || strings.ContainsRune(text, '\x00') {
		return nil, &Error{"media_cookies_invalid"}
	}
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 4096), MaxCookieBytes)
	var rows []cookie
	header := false
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if strings.HasPrefix(line, "# Netscape HTTP Cookie File") || strings.HasPrefix(line, "# HTTP Cookie File") {
			header = true
			continue
		}
		if strings.TrimSpace(line) == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_")) {
			continue
		}
		f := strings.Split(strings.TrimPrefix(line, "#HttpOnly_"), "\t")
		if len(f) != 7 || (f[1] != "TRUE" && f[1] != "FALSE") || (f[3] != "TRUE" && f[3] != "FALSE") || !strings.HasPrefix(f[2], "/") || f[5] == "" {
			return nil, &Error{"media_cookies_invalid"}
		}
		d := cookieDomain(f[0])
		if !validCookieDomain(d) {
			return nil, &Error{"media_cookies_invalid"}
		}
		expiry, e := strconv.ParseInt(f[4], 10, 64)
		if e != nil || expiry < 0 {
			return nil, &Error{"media_cookies_invalid"}
		}
		if expiry > 0 && expiry < time.Now().Unix() {
			continue
		}
		rows = append(rows, cookie{d, line})
		if len(rows) > 2000 {
			return nil, &Error{"media_cookies_invalid"}
		}
	}
	if scanner.Err() != nil || !header {
		return nil, &Error{"media_cookies_invalid"}
	}
	return rows, nil
}
func validCookieDomain(d string) bool {
	if len(d) > 253 || !strings.Contains(d, ".") || net.ParseIP(d) != nil {
		return false
	}
	for _, label := range strings.Split(d, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}
func (s Cookies) read(owner string) (cookieRecord, error) {
	var r cookieRecord
	dir, e := s.dir(owner)
	if e != nil {
		return r, e
	}
	p := filepath.Join(dir, "cookies.json")
	st, e := os.Lstat(p)
	if errors.Is(e, os.ErrNotExist) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	if !st.Mode().IsRegular() || st.Mode().Perm() != 0600 || st.Size() > MaxCookieBytes*3 {
		return r, &Error{"media_cookies_invalid"}
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return r, e
	}
	if json.Unmarshal(b, &r) != nil || r.Owner != owner {
		return cookieRecord{}, &Error{"media_cookies_invalid"}
	}
	return r, nil
}
func (s Cookies) write(owner string, r cookieRecord) error {
	dir, e := s.dir(owner)
	if e != nil {
		return e
	}
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".import-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if e = f.Chmod(0600); e != nil {
		return e
	}
	if _, e = f.Write(b); e != nil {
		return e
	}
	if e = f.Sync(); e != nil {
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), filepath.Join(dir, "cookies.json"))
}
func summary(r cookieRecord) CookieSummary {
	out := CookieSummary{Domains: []CookieDomain{}, ImportedAt: r.ImportedAt}
	rows, _ := parseCookies(r.Text)
	counts := map[string]int{}
	for _, c := range rows {
		counts[c.domain]++
	}
	for d, n := range counts {
		out.Domains = append(out.Domains, CookieDomain{d, n})
	}
	sort.Slice(out.Domains, func(i, j int) bool { return out.Domains[i].Domain < out.Domains[j].Domain })
	return out
}
func (s Cookies) Status(owner string) (CookieSummary, error) {
	r, e := s.read(owner)
	return summary(r), e
}
func (s Cookies) Import(owner, text string) (CookieSummary, error) {
	rows, e := parseCookies(text)
	if e != nil || len(rows) == 0 {
		return CookieSummary{}, &Error{"media_cookies_invalid"}
	}
	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n")
	for _, c := range rows {
		b.WriteString(c.line + "\n")
	}
	r := cookieRecord{owner, b.String(), time.Now().UTC()}
	if e = s.write(owner, r); e != nil {
		return CookieSummary{}, e
	}
	return summary(r), nil
}
func (s Cookies) Delete(owner, domain string) (CookieSummary, error) {
	r, e := s.read(owner)
	if e != nil {
		return CookieSummary{}, e
	}
	if domain == "" {
		e = s.Purge(owner)
		return CookieSummary{Domains: []CookieDomain{}}, e
	}
	if domain != cookieDomain(domain) || !validCookieDomain(domain) {
		return CookieSummary{}, &Error{"media_cookies_invalid"}
	}
	rows, _ := parseCookies(r.Text)
	var b strings.Builder
	b.WriteString("# Netscape HTTP Cookie File\n")
	for _, c := range rows {
		if c.domain != domain {
			b.WriteString(c.line + "\n")
		}
	}
	r.Text = b.String()
	if e = s.write(owner, r); e != nil {
		return CookieSummary{}, e
	}
	return summary(r), nil
}
func (s Cookies) Purge(owner string) error {
	uid, e := ownerUID(owner)
	if e != nil {
		return e
	}
	dir := filepath.Join(s.Home, uid)
	p := filepath.Join(dir, "cookies.json")
	st, e := os.Lstat(p)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() || st.Size() > MaxCookieBytes*3 {
		return &Error{"media_cookies_invalid"}
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return e
	}
	var r cookieRecord
	if json.Unmarshal(b, &r) != nil {
		return &Error{"media_cookies_invalid"}
	}
	// Cleaning a deleted name must not erase a replacement account's new jar.
	if r.Owner != owner {
		return nil
	}
	return os.RemoveAll(dir)
}

// Copy makes a new private cookie file for one run, filtered to source-host
// domains. It must be removed after Wait, including pause, shutdown and failure.
func (s Cookies) Copy(owner, source, tempDir string) (string, func(), error) {
	cleanup := func() {}
	if _, e := os.Stat(s.Home); os.IsNotExist(e) {
		return "", cleanup, nil
	}
	r, e := s.read(owner)
	if e != nil {
		return "", cleanup, e
	}
	if r.Text == "" {
		return "", cleanup, nil
	}
	u, e := url.Parse(source)
	if e != nil {
		return "", cleanup, &Error{"media_cookies_invalid"}
	}
	host := strings.ToLower(u.Hostname())
	hosts := []string{host}
	// YouTube's short links resolve to its canonical media host.
	if host == "youtu.be" {
		hosts = append(hosts, "www.youtube.com")
	}
	rows, e := parseCookies(r.Text)
	if e != nil {
		return "", cleanup, e
	}
	var b bytes.Buffer
	b.WriteString("# Netscape HTTP Cookie File\n")
	n := 0
	for _, c := range rows {
		for _, h := range hosts {
			if h == c.domain || strings.HasSuffix(h, "."+c.domain) {
				b.WriteString(c.line + "\n")
				n++
				break
			}
		}
	}
	if n == 0 {
		return "", cleanup, nil
	}
	if e = privateDir(tempDir); e != nil {
		return "", cleanup, e
	}
	f, e := os.CreateTemp(tempDir, ".cookies-*")
	if e != nil {
		return "", cleanup, e
	}
	p := f.Name()
	cleanup = func() { os.Remove(p) }
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b.Bytes())
	}
	closeErr := f.Close()
	if e == nil {
		e = closeErr
	}
	if e != nil {
		cleanup()
		return "", func() {}, e
	}
	return p, cleanup, nil
}
