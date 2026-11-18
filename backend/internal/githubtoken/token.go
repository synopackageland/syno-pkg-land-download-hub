// Package githubtoken keeps the administrator's optional GitHub token and
// adds it to requests bound for GitHub. Without a token every call stays
// anonymous. The token never leaves this package except as an Authorization
// header to a GitHub host over HTTPS; status shows only its last four
// characters, and nothing here logs it.
package githubtoken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// File is the token's name in the package data directory.
const File = "github-token"

// hosts receive the token: the API, release downloads (github.com, which
// redirects to signed asset storage that never gets it) and archives.
var hosts = map[string]bool{"api.github.com": true, "github.com": true, "codeload.github.com": true}

// ErrInvalid is GitHub answering 401 to a request that carried the token:
// it is wrong, expired or revoked.
var ErrInvalid = errors.New("github token invalid or expired")

// ErrFormat refuses text that cannot be a GitHub token.
var ErrFormat = errors.New("github token format")

var format = regexp.MustCompile(`^[A-Za-z0-9_]{20,255}$`)

// Valid tells whether text can be a GitHub token at all, before any call.
func Valid(token string) bool { return format.MatchString(token) }

// Store is the token file; the zero Store has no token.
type Store struct{ Path string }

func At(data string) Store { return Store{Path: filepath.Join(data, File)} }

// Load returns the saved token, or "" for anonymous calls.
func (s Store) Load() string {
	if s.Path == "" {
		return ""
	}
	b, err := os.ReadFile(s.Path)
	if err != nil || len(b) > 512 {
		return ""
	}
	token := strings.TrimSpace(string(b))
	if !format.MatchString(token) {
		return ""
	}
	return token
}

// Save writes the token readable by the package account only.
func (s Store) Save(token string) error {
	if !format.MatchString(token) {
		return ErrFormat
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".github-token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		if _, err = f.WriteString(token); err == nil {
			err = f.Sync()
		}
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), s.Path)
}

func (s Store) Remove() error {
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Status is what the browser may see.
type Status struct {
	Saved bool   `json:"saved"`
	Last4 string `json:"last4,omitempty"`
}

func (s Store) Status() Status {
	token := s.Load()
	if token == "" {
		return Status{}
	}
	return Status{Saved: true, Last4: token[len(token)-4:]}
}

func github(r *http.Request) bool {
	return r.URL.Scheme == "https" && r.URL.User == nil && hosts[r.URL.Host]
}

// Authorize adds the token to a request for a GitHub host over HTTPS.
func Authorize(r *http.Request, token string) {
	if token != "" && github(r) {
		r.Header.Set("Authorization", "Bearer "+token)
	}
}

// Redirect is called from a client's CheckRedirect: a hop away from GitHub
// never carries the token, even where Go would forward it.
func Redirect(r *http.Request) {
	if !github(r) {
		r.Header.Del("Authorization")
	}
}

// Limit is the core REST quota GitHub reports for the token.
type Limit struct {
	Limit     int       `json:"limit"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
}

// Validate asks GitHub once for the token's quota. /rate_limit is free: it
// does not count against the quota.
func Validate(ctx context.Context, client *http.Client, token string) (Limit, error) {
	var out Limit
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/rate_limit", nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := client.Do(req)
	if err != nil {
		return out, errors.New("github unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return out, ErrInvalid
	}
	if res.StatusCode != http.StatusOK {
		return out, fmt.Errorf("github HTTP %d", res.StatusCode)
	}
	var body struct {
		Resources struct {
			Core struct {
				Limit, Remaining int
				Reset            int64
			}
		}
	}
	if json.NewDecoder(io.LimitReader(res.Body, 64<<10)).Decode(&body) != nil || body.Resources.Core.Limit <= 0 {
		return out, errors.New("github answer invalid")
	}
	c := body.Resources.Core
	return Limit{Limit: c.Limit, Remaining: c.Remaining, Reset: time.Unix(c.Reset, 0)}, nil
}
