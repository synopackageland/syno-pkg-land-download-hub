package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCredentialTemplateNeverReachesURL(t *testing.T) {
	raw := strings.Replace(fixture, "search:\n", "settings:\n  - {name: apikey, type: password, label: Key}\nsearch:\n  inputs: {apikey: '{{ .Config.apikey }}'}\n", 1)
	_, meta := inspect(raw)
	if meta.Supported || !strings.Contains(strings.Join(meta.Reasons, ","), "credentials_in_url") {
		t.Fatalf("%+v", meta)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("private query reached network") }))
	defer srv.Close()
	req, _ := http.NewRequestWithContext(context.Background(), "GET", srv.URL+"/?apikey=very-secret", nil)
	_, err := withPrivacy(srv.Client(), nil, map[string]string{"apikey": "very-secret"}).Do(req)
	if err == nil {
		t.Fatal("secret URL allowed")
	}
}
func TestFeedDownloadDoesNotRunSearchAndDoesNotLeakRedirectHeaders(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "" || r.Header.Get("Cookie") != "" {
			t.Error("cross-origin credentials")
		}
		w.Write([]byte("d1:ai1ee"))
	}))
	defer target.Close()
	// Different hostname spelling exercises an actual origin change, same fixture server.
	targetURL := strings.Replace(target.URL, "127.0.0.1", "localhost", 1)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "" {
			t.Error("download rewritten as search")
		}
		http.Redirect(w, r, targetURL, http.StatusFound)
	}))
	defer source.Close()
	client := source.Client()
	client.CheckRedirect = newClient(true).CheckRedirect
	rep := feed(context.Background(), request{Op: "download", Kind: "torznab", URL: source.URL, Values: map[string]string{"apikey": "secret", "cookie": "session=secret"}}, client)
	if rep.Code != "" || rep.Torrent == "" {
		t.Fatalf("%+v", rep)
	}
}
