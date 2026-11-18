package githubtoken

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var sample = "github_pat_" + strings.Repeat("A", 40) + "GHIJ"

func TestStoreKeepsTheTokenPrivateAndShowsOnlyItsLastFour(t *testing.T) {
	s := At(t.TempDir())
	if s.Load() != "" || s.Status().Saved {
		t.Fatal("empty store must be anonymous")
	}
	if err := s.Save(sample); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v, %v", info.Mode(), err)
	}
	if s.Load() != sample {
		t.Fatal("token not read back")
	}
	if st := s.Status(); !st.Saved || st.Last4 != "GHIJ" {
		t.Fatalf("status = %+v", st)
	}
	if err := s.Remove(); err != nil || s.Load() != "" {
		t.Fatalf("remove = %v", err)
	}
	if err := s.Remove(); err != nil {
		t.Fatal("removing twice must succeed")
	}
}

func TestStoreRefusesTextThatIsNotAToken(t *testing.T) {
	s := At(t.TempDir())
	for _, bad := range []string{"", "short", "has space in it 1234567890", strings.Repeat("a", 300), "abc\r\nInjected: header0123456789"} {
		if err := s.Save(bad); !errors.Is(err, ErrFormat) {
			t.Errorf("Save(%q) = %v", bad, err)
		}
	}
	if _, err := os.Stat(filepath.Join(s.Path)); !os.IsNotExist(err) {
		t.Fatal("refused token must not be written")
	}
}

func TestAuthorizeSendsTheTokenOnlyToGitHubOverHTTPS(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://api.github.com/repos/a/b/releases/latest":               true,
		"https://github.com/a/b/releases/download/v1/x.zip":              true,
		"https://codeload.github.com/a/b/tar.gz/0123":                    true,
		"http://api.github.com/rate_limit":                               false,
		"https://release-assets.githubusercontent.com/github-production": false,
		"https://objects.githubusercontent.com/x":                        false,
		"https://example.com/":                                           false,
		"https://api.github.com.example.com/":                            false,
	} {
		req, _ := http.NewRequest("GET", raw, nil)
		Authorize(req, sample)
		if got := req.Header.Get("Authorization") == "Bearer "+sample; got != want {
			t.Errorf("%s: authorized = %v", raw, got)
		}
	}
	req, _ := http.NewRequest("GET", "https://api.github.com/x", nil)
	Authorize(req, "")
	if req.Header.Get("Authorization") != "" {
		t.Fatal("no token must stay anonymous")
	}
}

func TestRedirectDropsTheTokenOffGitHub(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://release-assets.githubusercontent.com/x", nil)
	req.Header.Set("Authorization", "Bearer "+sample)
	Redirect(req)
	if req.Header.Get("Authorization") != "" {
		t.Fatal("token followed a redirect to another host")
	}
	req, _ = http.NewRequest("GET", "https://codeload.github.com/x", nil)
	req.Header.Set("Authorization", "Bearer "+sample)
	Redirect(req)
	if req.Header.Get("Authorization") == "" {
		t.Fatal("GitHub host lost the token")
	}
}

// fake answers requests without a network, recording what was sent.
type fake struct {
	seen *http.Request
	res  func(*http.Request) *http.Response
}

func (f *fake) RoundTrip(r *http.Request) (*http.Response, error) {
	f.seen = r
	return f.res(r), nil
}
func reply(status int, body string, h map[string]string) *http.Response {
	res := &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	for k, v := range h {
		res.Header.Set(k, v)
	}
	return res
}

func TestValidateReadsTheCoreLimitWithTheToken(t *testing.T) {
	f := &fake{res: func(*http.Request) *http.Response {
		return reply(200, `{"resources":{"core":{"limit":5000,"remaining":4999,"reset":4102444800}}}`, nil)
	}}
	limit, err := Validate(context.Background(), &http.Client{Transport: f}, sample)
	if err != nil {
		t.Fatal(err)
	}
	if f.seen.URL.String() != "https://api.github.com/rate_limit" || f.seen.Header.Get("Authorization") != "Bearer "+sample {
		t.Fatalf("request = %s %v", f.seen.URL, f.seen.Header)
	}
	if limit.Limit != 5000 || limit.Remaining != 4999 || !limit.Reset.Equal(time.Unix(4102444800, 0)) {
		t.Fatalf("limit = %+v", limit)
	}
}

func TestValidateSaysWhenGitHubRejectsTheToken(t *testing.T) {
	f := &fake{res: func(*http.Request) *http.Response { return reply(401, `{"message":"Bad credentials"}`, nil) }}
	if _, err := Validate(context.Background(), &http.Client{Transport: f}, sample); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	f.res = func(*http.Request) *http.Response { return reply(503, ``, nil) }
	if _, err := Validate(context.Background(), &http.Client{Transport: f}, sample); err == nil || errors.Is(err, ErrInvalid) {
		t.Fatalf("outage err = %v", err)
	}
}
