package search

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
)

func archiveFixture(t *testing.T, name, raw string) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(raw))})
	tw.Write([]byte(raw))
	tw.Close()
	gz.Close()
	return b.Bytes()
}
func blob(raw string) string {
	return fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(raw), raw))))
}
func TestUpdateRejectsTraversalAndHashMismatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		sha  string
	}{{"root/definitions/v11/../escape.yml", blob("id: fixture")}, {"root/definitions/v11/fixture.yml", "bad"}} {
		_, err := readArchive(archiveFixture(t, tc.name, "id: fixture"), map[string]string{"fixture.yml": tc.sha})
		if err == nil {
			t.Fatal("accepted unsafe archive")
		}
	}
}
func TestAtomicGenerationRollbackAndFailedValidation(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	original, err := s.bundle(ctx)
	if err != nil {
		t.Fatal(err)
	}
	candidate := Bundle{Commit: fmt.Sprintf("%040d", 2), Schema: 11, Definitions: map[string]string{"fixture.yml": "id: fixture"}}
	if err = s.install(ctx, candidate); err != nil {
		t.Fatal(err)
	}
	got, err := s.bundle(ctx)
	if err != nil || got.Commit != candidate.Commit {
		t.Fatalf("%+v %v", got, err)
	}
	s.Run = func(context.Context, Request) (Reply, error) {
		return Reply{Sites: []Site{{ID: "fixture", Reasons: []string{"schema_or_engine_unsupported"}}}}, nil
	}
	candidate.Commit = fmt.Sprintf("%040d", 3)
	if err = s.install(ctx, candidate); Code(err) != "search_update_invalid" {
		t.Fatalf("%v", err)
	}
	got, _ = s.bundle(ctx)
	if got.Commit == candidate.Commit {
		t.Fatal("failed update changed current")
	}
	if err = s.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ = s.bundle(ctx)
	if got.Commit != original.Commit {
		t.Fatal("rollback did not restore previous generation")
	}
}

type updateTransport func(*http.Request) (*http.Response, error)

func (f updateTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestPinnedUpdateFetchesExactTreeAndPreservesOldOnBadHash(t *testing.T) {
	s := fixtureService(t)
	raw := "id: fixture"
	commit := strings.Repeat("a", 40)
	bad := false
	calls := 0
	s.HTTP = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		var data []byte
		switch {
		case r.URL.Host == "api.github.com" && strings.HasSuffix(r.URL.Path, "commits/master"):
			data = []byte(`{"sha":"` + commit + `"}`)
		case r.URL.Host == "api.github.com" && strings.Contains(r.URL.Path, "git/trees/"+commit):
			sha := blob(raw)
			if bad {
				sha = strings.Repeat("b", 40)
			}
			data = []byte(`{"tree":[{"path":"definitions/v11/fixture.yml","type":"blob","mode":"100644","sha":"` + sha + `"}]}`)
		case r.URL.Host == "codeload.github.com" && strings.HasSuffix(r.URL.Path, commit):
			data = archiveFixture(t, "Indexers-"+commit+"/definitions/v11/fixture.yml", raw)
		default:
			t.Fatalf("unpinned update URL %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header)}, nil
	})}
	ctx := context.Background()
	first, err := s.Update(ctx)
	if err != nil || first.Commit != commit || !first.RollbackAvailable {
		t.Fatalf("%+v %v", first, err)
	}
	before := calls
	if _, err = s.Update(ctx); err != nil || calls != before {
		t.Fatalf("cache missed: calls=%d %v", calls, err)
	}
	bad = true
	if err = os.Remove(filepath.Join(s.Data, "update-check.json")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx); Code(err) != "search_update_hash" {
		t.Fatalf("%v", err)
	}
	got, _ := s.bundle(ctx)
	if got.Commit != commit {
		t.Fatal("failed fetch replaced live generation")
	}
}
func TestStagingFailureLeavesBaselineUsable(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	candidate := Bundle{Commit: strings.Repeat("c", 40), Schema: 11, Definitions: map[string]string{"fixture.yml": "id: fixture"}}
	candidate.DefinitionFiles = map[string]string{"fixture": "fixture.yml"}
	raw, _ := json.Marshal(candidate)
	path := filepath.Join(s.Data, "generations", digest(raw)+".json")
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.install(ctx, candidate); err == nil {
		t.Fatal("installation should fail on unreplaceable staging target")
	}
	got, err := s.bundle(ctx)
	if err != nil || got.Commit == candidate.Commit {
		t.Fatalf("%s %v", got.Commit, err)
	}
}

// The catalog says when its definitions were fetched; the bundled baseline
// was never fetched on this NAS.
func TestCatalogSaysWhenItsDefinitionsWereFetched(t *testing.T) {
	s := fixtureService(t)
	ctx := context.Background()
	s.Run = func(_ context.Context, in Request) (Reply, error) {
		out := Reply{}
		for range in.Definitions {
			out.Sites = append(out.Sites, Site{ID: "fixture", Supported: true, Reasons: []string{}, Settings: []Setting{}})
		}
		return out, nil
	}
	c, err := s.Catalog(ctx)
	if err != nil || c.UpdatedAt != "" {
		t.Fatalf("baseline: %q %v", c.UpdatedAt, err)
	}
	before := time.Now().Add(-time.Second)
	if err = s.install(ctx, Bundle{Commit: fmt.Sprintf("%040d", 2), Schema: 11, Definitions: map[string]string{"fixture.yml": "id: fixture"}}); err != nil {
		t.Fatal(err)
	}
	c, err = s.Catalog(ctx)
	when, perr := time.Parse(time.RFC3339, c.UpdatedAt)
	if err != nil || perr != nil || when.Before(before) {
		t.Fatalf("%q %v %v", c.UpdatedAt, err, perr)
	}
}

func TestUpdateCachesRateLimitAndKeepsLocalCatalogReadable(t *testing.T) {
	s := fixtureService(t)
	if err := s.install(context.Background(), Bundle{Commit: strings.Repeat("a", 40), Schema: 11, Definitions: map[string]string{"fixture.yml": "id: fixture"}}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	s.HTTP = &http.Client{Transport: updateTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 403, Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {fmt.Sprint(time.Now().Add(time.Hour).Unix())}}, Body: io.NopCloser(strings.NewReader("limited"))}, nil
	})}
	for i := 0; i < 2; i++ {
		if _, err := s.Update(context.Background()); Code(err) != "search_update_rate_limited" {
			t.Fatalf("%v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("retried rate limited GitHub %d times", calls)
	}
	if _, err := s.Catalog(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatal("reading local catalog contacted GitHub")
	}
}

func TestUpdateDoesNotMislabelEvery403AsRateLimiting(t *testing.T) {
	s := fixtureService(t)
	s.HTTP = &http.Client{Transport: updateTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 403, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("forbidden"))}, nil
	})}
	if _, err := s.Update(context.Background()); Code(err) != "search_update_forbidden" {
		t.Fatalf("%v", err)
	}
}

func TestUpdateSaysWhenTheLimitResetsAndATokenSkipsTheAnonymousLimit(t *testing.T) {
	s := fixtureService(t)
	reset := time.Now().Add(time.Hour).Truncate(time.Second)
	var seen []string
	limited := true
	s.HTTP = &http.Client{Transport: updateTransport(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.URL.Host+" "+r.Header.Get("Authorization"))
		if limited {
			return &http.Response{StatusCode: 403, Header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {fmt.Sprint(reset.Unix())}}, Body: io.NopCloser(strings.NewReader("limited"))}, nil
		}
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
	})}
	_, err := s.Update(context.Background())
	var rl *binaries.RateLimitError
	if Code(err) != "search_update_rate_limited" || !errors.As(err, &rl) || !rl.Reset.Equal(reset) {
		t.Fatalf("err = %v", err)
	}
	if _, err = s.Update(context.Background()); !errors.As(err, &rl) || !rl.Reset.Equal(reset) {
		t.Fatalf("cached err = %v", err)
	}
	token := "github_pat_" + strings.Repeat("C", 40)
	s.GitHub = githubtoken.At(t.TempDir())
	if err = s.GitHub.Save(token); err != nil {
		t.Fatal(err)
	}
	limited = false
	if _, err = s.Update(context.Background()); Code(err) != "github_token_invalid" || !errors.Is(err, githubtoken.ErrInvalid) {
		t.Fatalf("token err = %v", err)
	}
	if len(seen) != 2 || seen[0] != "api.github.com " || seen[1] != "api.github.com Bearer "+token {
		t.Fatalf("requests = %q", seen)
	}
}
