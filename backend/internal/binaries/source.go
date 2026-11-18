package binaries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"dsmapp/internal/githubtoken"
)

// ErrUnavailable means the official source could not be reached at all.
var ErrUnavailable = errors.New("official source unavailable")

// RateLimitError is GitHub refusing anonymous calls until Reset. The quota is
// shared by every machine behind the same public address.
type RateLimitError struct{ Reset time.Time }

func (e *RateLimitError) Error() string {
	return "official source rate limited until " + e.Reset.UTC().Format(time.RFC3339)
}

// SourceError classifies a non-success answer from the official source. The
// API's 403 is a limit only when no quota remains or GitHub names its
// secondary limit; the web answers 429. A 401 to a request that carried the
// administrator's token means the token is wrong or expired.
func SourceError(resp *http.Response) error {
	h := resp.Header
	if resp.StatusCode == 401 && resp.Request != nil && resp.Request.Header.Get("Authorization") != "" {
		return githubtoken.ErrInvalid
	}
	secondary := false
	if resp.StatusCode == 403 && h.Get("X-RateLimit-Remaining") != "0" && resp.Body != nil {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		secondary = strings.Contains(strings.ToLower(string(b)), "secondary rate limit")
	}
	if resp.StatusCode == 429 || resp.StatusCode == 403 && (h.Get("X-RateLimit-Remaining") == "0" || secondary) {
		if after, err := strconv.Atoi(h.Get("Retry-After")); err == nil && after >= 0 && h.Get("X-RateLimit-Remaining") != "0" {
			return &RateLimitError{Reset: clock().Add(time.Duration(after) * time.Second)}
		}
		if reset, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
			return &RateLimitError{Reset: time.Unix(reset, 0)}
		}
		if after, err := strconv.Atoi(h.Get("Retry-After")); err == nil && after >= 0 {
			return &RateLimitError{Reset: clock().Add(time.Duration(after) * time.Second)}
		}
		// GitHub asks to wait at least a minute after a secondary limit.
		if secondary {
			return &RateLimitError{Reset: clock().Add(time.Minute)}
		}
		return &RateLimitError{Reset: clock().Add(time.Hour)}
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("%w: HTTP %d", ErrUnavailable, resp.StatusCode)
	}
	return fmt.Errorf("official source HTTP %d", resp.StatusCode)
}

// TokenKey separates cache entries made with a token from anonymous ones, so
// a limit GitHub set for this address does not hold back a new token.
func TokenKey(key, token string) string {
	if token == "" {
		return key
	}
	return key + "+token"
}

// LatestTag reads the tag of a repository's latest release from the redirect
// of web/<repo>/releases/latest, which is not counted against the API quota.
// GitHub points it at the newest release that is neither a draft nor a
// prerelease. The redirect is not followed and no page is downloaded.
func LatestTag(ctx context.Context, client *http.Client, web, repo, token string) (string, error) {
	base, err := url.Parse(web + "/" + repo + "/releases/latest")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base.String(), nil)
	if err != nil {
		return "", err
	}
	githubtoken.Authorize(req, token)
	once := *client
	once.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := once.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound && resp.StatusCode != http.StatusMovedPermanently {
		if resp.StatusCode >= 400 {
			return "", SourceError(resp)
		}
		return "", errors.New("no stable release")
	}
	loc, err := base.Parse(resp.Header.Get("Location"))
	prefix := "/" + repo + "/releases/tag/"
	if err != nil || loc.Scheme != base.Scheme || loc.Host != base.Host || loc.User != nil || !strings.HasPrefix(loc.Path, prefix) {
		return "", errors.New("unexpected release redirect")
	}
	tag := strings.TrimPrefix(loc.Path, prefix)
	if tag == "" || strings.Contains(tag, "/") {
		return "", errors.New("unexpected release redirect")
	}
	return tag, nil
}

// CheckTTL is how long one release check answers every later check.
var CheckTTL = time.Hour

var clock = time.Now

type cacheEntry struct {
	At    time.Time       `json:"at"`
	Value json.RawMessage `json:"value,omitempty"`
	Reset time.Time       `json:"reset,omitempty"`
}

// Cached answers from file while key's last answer is fresh, so tools, users
// and page refreshes share one upstream call. A rate limit is kept until it
// resets; any other failure is retried on the next call.
func Cached[T any](file, key string, fetch func() (T, error)) (T, error) {
	entries := map[string]cacheEntry{}
	if b, err := os.ReadFile(file); err == nil && len(b) <= 256<<10 {
		json.Unmarshal(b, &entries)
	}
	now := clock()
	var out T
	if e, ok := entries[key]; ok {
		if now.Before(e.Reset) {
			return out, &RateLimitError{Reset: e.Reset}
		}
		if e.Value != nil && now.Sub(e.At) < CheckTTL && json.Unmarshal(e.Value, &out) == nil {
			return out, nil
		}
	}
	out, err := fetch()
	var rl *RateLimitError
	switch {
	case err == nil:
		b, _ := json.Marshal(out)
		entries[key] = cacheEntry{At: now, Value: b}
	case errors.As(err, &rl):
		entries[key] = cacheEntry{At: now, Reset: rl.Reset}
	default:
		return out, err
	}
	Atomic(file, entries)
	return out, err
}
