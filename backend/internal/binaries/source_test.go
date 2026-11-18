package binaries

import (
	"errors"
	"io"
	"strings"

	"dsmapp/internal/githubtoken"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestSourceErrorSeparatesRateLimitFromOtherRefusals(t *testing.T) {
	reset := time.Unix(1791943382, 0)
	limited := func(code int, remaining string) *http.Response {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", remaining)
		h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		return &http.Response{StatusCode: code, Header: h}
	}
	for _, code := range []int{403, 429} {
		var rl *RateLimitError
		if err := SourceError(limited(code, "0")); !errors.As(err, &rl) || !rl.Reset.Equal(reset) {
			t.Fatalf("%d: %v", code, err)
		}
	}
	now := time.Unix(1791940000, 0)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	h := http.Header{}
	h.Set("Retry-After", "120")
	var rl *RateLimitError
	if err := SourceError(&http.Response{StatusCode: 429, Header: h}); !errors.As(err, &rl) || !rl.Reset.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("429 from the web without quota headers: %v", err)
	}
	if err := SourceError(limited(403, "12")); errors.As(err, &rl) || errors.Is(err, ErrUnavailable) {
		t.Fatalf("403 with quota left is a refusal, not a limit: %v", err)
	}
	if err := SourceError(&http.Response{StatusCode: 404, Header: http.Header{}}); errors.As(err, &rl) || errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	if err := SourceError(&http.Response{StatusCode: 503, Header: http.Header{}}); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestLatestTagReadsOnlyTheRedirect(t *testing.T) {
	var followed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/yt-dlp/yt-dlp/releases/latest":
			http.Redirect(w, r, "/yt-dlp/yt-dlp/releases/tag/2026.08.19", http.StatusFound)
		case "/other/releases/latest":
			http.Redirect(w, r, "/yt-dlp/yt-dlp/releases/tag/2026.08.19", http.StatusFound)
		case "/none/releases/latest":
			w.Write([]byte("<html>no releases</html>"))
		case "/limited/releases/latest":
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", "1791943382")
			w.WriteHeader(http.StatusTooManyRequests)
		default:
			followed = true
		}
	}))
	defer srv.Close()
	tag, err := LatestTag(t.Context(), srv.Client(), srv.URL, "yt-dlp/yt-dlp", "")
	if err != nil || tag != "2026.08.19" || followed {
		t.Fatalf("%q %v followed=%v", tag, err, followed)
	}
	if _, err = LatestTag(t.Context(), srv.Client(), srv.URL, "other", ""); err == nil {
		t.Fatal("redirect to another repository accepted")
	}
	if _, err = LatestTag(t.Context(), srv.Client(), srv.URL, "none", ""); err == nil {
		t.Fatal("answer without a redirect accepted")
	}
	var rl *RateLimitError
	if _, err = LatestTag(t.Context(), srv.Client(), srv.URL, "limited", ""); !errors.As(err, &rl) {
		t.Fatal(err)
	}
	srv.Close()
	if _, err = LatestTag(t.Context(), srv.Client(), srv.URL, "yt-dlp/yt-dlp", ""); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
}

func TestCachedSharesOneAnswerUntilItExpires(t *testing.T) {
	file := filepath.Join(t.TempDir(), "checks.json")
	now := time.Unix(1791940000, 0)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	calls := 0
	fetch := func(v string, err error) func() (string, error) {
		return func() (string, error) { calls++; return v, err }
	}
	if v, err := Cached(file, "yt-dlp", fetch("a", nil)); v != "a" || err != nil {
		t.Fatal(v, err)
	}
	if v, err := Cached(file, "yt-dlp", fetch("b", nil)); v != "a" || err != nil || calls != 1 {
		t.Fatal("fresh answer not shared", v, err, calls)
	}
	if v, _ := Cached(file, "ffmpeg", fetch("c", nil)); v != "c" || calls != 2 {
		t.Fatal("keys share an answer", v, calls)
	}
	now = now.Add(CheckTTL)
	if v, _ := Cached(file, "yt-dlp", fetch("b", nil)); v != "b" || calls != 3 {
		t.Fatal("expired answer kept", v, calls)
	}
}

func TestCachedRemembersARateLimitUntilItsReset(t *testing.T) {
	file := filepath.Join(t.TempDir(), "checks.json")
	now := time.Unix(1791940000, 0)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	calls := 0
	limited := func() (string, error) { calls++; return "", &RateLimitError{Reset: now.Add(10 * time.Minute)} }
	var rl *RateLimitError
	if _, err := Cached(file, "core", limited); !errors.As(err, &rl) {
		t.Fatal(err)
	}
	if _, err := Cached(file, "core", limited); !errors.As(err, &rl) || calls != 1 || !rl.Reset.Equal(now.Add(10*time.Minute)) {
		t.Fatal("limit not shared", err, calls)
	}
	now = now.Add(10 * time.Minute)
	if v, err := Cached(file, "core", func() (string, error) { calls++; return "v", nil }); v != "v" || err != nil || calls != 2 {
		t.Fatal("limit kept after reset", v, err, calls)
	}
	offline := func() (string, error) { calls++; return "", ErrUnavailable }
	now = now.Add(CheckTTL)
	Cached(file, "core", offline)
	Cached(file, "core", offline)
	if calls != 4 {
		t.Fatal("network failure cached", calls)
	}
}

func TestSourceErrorNamesSecondaryLimitsAndARejectedToken(t *testing.T) {
	now := time.Unix(1791940000, 0)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = time.Now })
	body := `{"message":"You have exceeded a secondary rate limit. Please wait a few minutes before you try again."}`
	var rl *RateLimitError
	err := SourceError(&http.Response{StatusCode: 403, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))})
	if !errors.As(err, &rl) || !rl.Reset.Equal(now.Add(time.Minute)) {
		t.Fatalf("secondary limit: %v", err)
	}
	req, _ := http.NewRequest("GET", "https://api.github.com/x", nil)
	req.Header.Set("Authorization", "Bearer x")
	if err := SourceError(&http.Response{StatusCode: 401, Header: http.Header{}, Request: req}); !errors.Is(err, githubtoken.ErrInvalid) {
		t.Fatalf("401 with a token: %v", err)
	}
	anon, _ := http.NewRequest("GET", "https://api.github.com/x", nil)
	if err := SourceError(&http.Response{StatusCode: 401, Header: http.Header{}, Request: anon}); errors.Is(err, githubtoken.ErrInvalid) {
		t.Fatal("anonymous 401 blamed a token")
	}
}

func TestCachedKeysTheTokenSoANewTokenIsNotHeldBackByAnAnonymousLimit(t *testing.T) {
	file := filepath.Join(t.TempDir(), "checks.json")
	calls := 0
	limited := func() (string, error) { calls++; return "", &RateLimitError{Reset: time.Now().Add(time.Hour)} }
	ok := func() (string, error) { calls++; return "v1", nil }
	Cached(file, "tool", limited)
	if _, err := Cached(file, TokenKey("tool", "github_pat_x"), ok); err != nil || calls != 2 {
		t.Fatalf("token call held back: %v calls=%d", err, calls)
	}
	if TokenKey("tool", "") != "tool" {
		t.Fatal("anonymous key changed")
	}
}
