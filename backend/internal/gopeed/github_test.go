package gopeed

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitHubTokenIsCheckedOnceSavedPrivatelyAndNeverReturned(t *testing.T) {
	s, _, _ := fixture(t)
	token := "github_pat_" + strings.Repeat("D", 36) + "wxyz"
	answer := 200
	var sent string
	s.githubHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		sent = r.URL.String() + " " + r.Header.Get("Authorization")
		return &http.Response{StatusCode: answer, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"resources":{"core":{"limit":5000,"remaining":5000,"reset":4102444800}}}`))}, nil
	})}
	out := adminCall(t, s, "gopeed.github.set", map[string]string{"token": token})
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), token) || sent != "https://api.github.com/rate_limit Bearer "+token {
		t.Fatalf("reply %s, sent %q", raw, sent)
	}
	data, _ := out["data"].(map[string]any)
	if data["saved"] != true || data["last4"] != "wxyz" || data["limit"] != float64(5000) {
		t.Fatalf("reply %s", raw)
	}
	info, err := os.Stat(filepath.Join(s.Data, "github-token"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal(info, err)
	}
	if st, _ := adminCall(t, s, "gopeed.github.status", map[string]any{})["data"].(map[string]any); st["saved"] != true || st["last4"] != "wxyz" {
		t.Fatal(st)
	}
	answer = 401
	if code, _ := errorOf(adminCall(t, s, "gopeed.github.set", map[string]string{"token": "github_pat_" + strings.Repeat("E", 40)})); code != "github_token_invalid" {
		t.Fatal(code)
	}
	if st, _ := adminCall(t, s, "gopeed.github.status", map[string]any{})["data"].(map[string]any); st["last4"] != "wxyz" {
		t.Fatal("a rejected token replaced the saved one", st)
	}
	if code, _ := errorOf(adminCall(t, s, "gopeed.github.set", map[string]string{"token": "not a token"})); code != "github_token_format" {
		t.Fatal(code)
	}
	adminCall(t, s, "gopeed.github.remove", map[string]any{})
	if _, err := os.Stat(filepath.Join(s.Data, "github-token")); !os.IsNotExist(err) {
		t.Fatal("token kept after remove")
	}
}

func TestGitHubTokenIsForAdministratorsOnly(t *testing.T) {
	s, _, _ := fixture(t)
	for _, method := range []string{"gopeed.github.status", "gopeed.github.set", "gopeed.github.remove"} {
		out, _ := call(t, s, "normal1", method, map[string]any{})
		if code, _ := errorOf(out); code == "" {
			t.Fatalf("%s answered a non-administrator: %v", method, out)
		}
	}
}

func TestGitHubLimitMessageNamesTheResetAndTheSetting(t *testing.T) {
	s, _, _ := fixture(t)
	s.coreHTTP = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		h := http.Header{}
		h.Set("X-RateLimit-Remaining", "0")
		h.Set("X-RateLimit-Reset", "4102444800")
		return &http.Response{StatusCode: 403, Header: h, Body: http.NoBody}, nil
	})}
	_, message := errorOf(adminCall(t, s, "binaries.check", map[string]any{}))
	if !strings.Contains(message, "2100-01-01T00:00:00Z") || !strings.Contains(message, "Settings > GitHub") {
		t.Fatal(message)
	}
}
