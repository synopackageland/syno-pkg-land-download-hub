package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mediactl/clustarr/pkg/cardigann"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func response(r *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{"text/html"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}
}
func TestSolverChallengeOnlyAndSession(t *testing.T) {
	calls := 0
	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		if r.URL.Path != "/v1" || in["cmd"] != "request.get" || in["maxTimeout"].(float64) > 60000 {
			t.Error("bad solver request", in)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials relayed")
		}
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "solution": map[string]any{"status": 200, "url": in["url"], "response": "<html>solved</html>", "userAgent": "solver-agent", "cookies": []map[string]any{{"name": "cf_clearance", "value": "private-cookie", "domain": "source.test", "path": "/"}}}})
	}))
	defer solver.Close()
	direct := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/next" {
			if r.UserAgent() != "solver-agent" || !strings.Contains(r.Header.Get("Cookie"), "cf_clearance=private-cookie") {
				t.Error("session not reused")
			}
			return response(r, 200, "ok"), nil
		}
		out := response(r, 403, "Forbidden")
		out.Header.Set("Server", "cloudflare")
		if r.URL.Path == "/challenge" {
			out.Header.Set("Cf-Mitigated", "challenge")
		}
		return out, nil
	})
	tr := newSolverTransport(direct, solver.URL)
	tr.validate = func(context.Context, *http.Request) error { return nil }
	for _, path := range []string{"/forbidden", "/challenge", "/next"} {
		r, _ := http.NewRequest("GET", "https://source.test"+path, nil)
		res, err := tr.RoundTrip(r)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
	}
	if calls != 1 {
		t.Fatalf("solver called %d times", calls)
	}
	// A distinct source gets neither cookies nor the user agent.
	r, _ := http.NewRequest("GET", "https://other.test/forbidden", nil)
	res, err := tr.RoundTrip(r)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if calls != 1 {
		t.Fatal(calls)
	}
}
func TestSolverRefusesCredentialReplayAndForeignSolution(t *testing.T) {
	calls := 0
	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"status":"ok","solution":{"status":200,"url":"http://127.0.0.1/private","response":"secret"}}`)
	}))
	defer solver.Close()
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		out := response(r, 403, `<html><div id="cf-chl-widget">challenge</div></html>`)
		return out, nil
	})
	for _, header := range []string{"Cookie", "Authorization", "X-Api-Key"} {
		tr := newSolverTransport(base, solver.URL)
		tr.validate = func(context.Context, *http.Request) error { return nil }
		r, _ := http.NewRequest("GET", "https://source.test/", nil)
		r.Header.Set(header, "secret")
		_, err := tr.RoundTrip(r)
		if err == nil {
			t.Fatal("credential challenge should stay refused")
		}
	}
	if calls != 0 {
		t.Fatal("credentials sent to solver")
	}
	tr := newSolverTransport(base, solver.URL)
	tr.validate = func(context.Context, *http.Request) error { return nil }
	r, _ := http.NewRequest("GET", "https://source.test/", nil)
	if _, err := tr.RoundTrip(r); err == nil {
		t.Fatal("foreign final URL accepted")
	}
}
func TestSolverCancellationAndNoRedirect(t *testing.T) {
	received := make(chan struct{})
	solver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		close(received)
		select {
		case <-r.Context().Done():
		case <-time.After(200 * time.Millisecond):
		}
	}))
	defer solver.Close()
	tr := newSolverTransport(roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 403, "<html>cf-chl-test</html>"), nil
	}), solver.URL)
	tr.validate = func(context.Context, *http.Request) error { return nil }
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r, _ := http.NewRequestWithContext(ctx, "GET", "https://source.test/", nil)
	_, err := tr.RoundTrip(r)
	if err == nil {
		t.Fatal("timeout ignored")
	}
}
func TestCloudflareDetectionNeedsEvidence(t *testing.T) {
	for _, tc := range []struct {
		status  int
		headers http.Header
		body    string
		want    bool
	}{
		{403, http.Header{"Server": []string{"cloudflare"}}, "Forbidden", false},
		{503, http.Header{"Server": []string{"cloudflare"}}, "Unavailable", false},
		{403, http.Header{"Cf-Mitigated": []string{"challenge"}}, "", true},
		{200, http.Header{}, "<html>cf-chl-test</html>", true},
		{200, http.Header{}, "normal article mentioning Cloudflare", false},
	} {
		if got := cardigann.IsCloudflareChallenge(tc.headers, []byte(tc.body)); got != tc.want {
			t.Errorf("status %d: %v", tc.status, got)
		}
	}
}

func TestSolverTargetBlacklist(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "0.0.0.0", "[::1]", "[fd00::1]"} {
		r, _ := http.NewRequest("GET", "http://"+host+"/", nil)
		if publicSolverTarget(context.Background(), r) == nil {
			t.Fatal("private target accepted", host)
		}
	}
}

func TestOpenSiteRealDefinitionsEscapesQueryWithoutFetching(t *testing.T) {
	raw, err := os.ReadFile("../backend/internal/search/baseline/bundle.json")
	if err != nil {
		t.Fatal(err)
	}
	var bundle struct {
		Definitions map[string]string `json:"definitions"`
	}
	json.Unmarshal(raw, &bundle)
	for _, name := range []string{"1337x.yml", "kickasstorrents-ws.yml", "uindex.yml"} {
		rep := run(context.Background(), request{Op: "site", Kind: "builtin", YAML: bundle.Definitions[name], Query: "a&b"}, &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { t.Fatal("site link fetched network"); return nil, nil })})
		if rep.Code != "" || !strings.Contains(rep.SiteURL, "a%26b") {
			t.Errorf("%s: %+v", name, rep)
		}
	}
}

func TestSolverRefusesURLCredentials(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.WriteHeader(500) }))
	defer server.Close()
	base := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return response(r, 403, "<html>cf-chl-test</html>"), nil
	})
	for _, raw := range []string{"https://source.test/api?apikey=private", "https://source.test/api?passkey=private", "https://source.test/api?token=private", "https://user:private@source.test/"} {
		tr := newSolverTransport(base, server.URL)
		tr.validate = func(context.Context, *http.Request) error { return nil }
		r, _ := http.NewRequest("GET", raw, nil)
		_, err := tr.RoundTrip(r)
		if err == nil {
			t.Fatal("credential URL should stay refused")
		}
	}
	if calls != 0 {
		t.Fatal("credential URL sent to solver", calls)
	}
}
