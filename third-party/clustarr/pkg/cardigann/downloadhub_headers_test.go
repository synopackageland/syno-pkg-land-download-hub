package cardigann_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mediactl/clustarr/pkg/cardigann"
)

// A definition's own headers (an API key) go to the site only: a download
// selector that points at another host is fetched without them.
func TestDownloadHubDefinitionHeadersStayOnSite(t *testing.T) {
	var mu sync.Mutex
	got := map[string]string{}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got["other"+r.URL.Path] = r.Header.Get("X-Api-Key")
		mu.Unlock()
		_, _ = w.Write([]byte(fakeTorrent))
	}))
	t.Cleanup(other.Close)
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got["site"+r.URL.Path] = r.Header.Get("X-Api-Key")
		mu.Unlock()
		if r.URL.Path == "/details/1" {
			_, _ = w.Write([]byte(`<a class="dl" href="` + offSite(other.URL) + `/cache/1.torrent">x</a>`))
			return
		}
		_, _ = w.Write([]byte(fakeTorrent))
	}))
	t.Cleanup(tracker.Close)

	def := &cardigann.Definition{
		Links:  []string{tracker.URL + "/"},
		Search: cardigann.SearchBlock{Headers: map[string][]string{"X-Api-Key": {"secret-key"}}},
		Download: &cardigann.DownloadBlock{
			Selectors: []cardigann.SelectorField{{Selector: "a.dl", Attribute: "href"}},
		},
	}
	cfg, err := cardigann.NewConfig(def, tracker.URL+"/", nil)
	require.NoError(t, err)
	rc, err := cardigann.Engine{HTTP: tracker.Client()}.Download(context.Background(), def, cfg, "/details/1")
	require.NoError(t, err)
	_, _ = io.Copy(io.Discard, rc)
	_ = rc.Close()

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "secret-key", got["site/details/1"], "the site still receives its headers")
	assert.Equal(t, "", got["other/cache/1.torrent"], "another host receives none")
	_, hit := got["other/cache/1.torrent"]
	assert.True(t, hit, "the off-site file was fetched")
}
