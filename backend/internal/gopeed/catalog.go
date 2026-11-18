package gopeed

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
)

type CatalogEntry struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
	RepoURL     string `json:"repoUrl"`
	Directory   string `json:"directory"`
	URL         string `json:"url"`
	// The store's icon, kept only on GitHub's raw host over https.
	Icon string `json:"icon,omitempty"`
}

func storeIcon(raw string) string {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 512 || u.Scheme != "https" || u.Host != "raw.githubusercontent.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	return u.String()
}

func (s *Service) catalog(ctx context.Context, raw json.RawMessage) (any, error) {
	if e := decodeStrict(raw, &struct{}{}); e != nil {
		return nil, e
	}
	endpoint := s.CatalogURL
	if endpoint == "" {
		endpoint = "https://gopeed.com/api/extensions?page=1&limit=100&sort=stars&order=desc"
	}
	req, e := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if e != nil {
		return nil, refused("catalog_unavailable")
	}
	resp, e := s.HTTP.Do(req)
	if e != nil {
		return nil, refused("catalog_unavailable")
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(io.LimitReader(resp.Body, (512<<10)+1))
	var page struct {
		Data       []CatalogEntry
		Pagination struct{ HasNext bool }
	}
	if e != nil || len(b) > 512<<10 || resp.StatusCode != 200 || json.Unmarshal(b, &page) != nil || page.Data == nil || len(page.Data) > 100 || page.Pagination.HasNext {
		return nil, refused("catalog_unavailable")
	}
	out := []CatalogEntry{}
	for _, entry := range page.Data {
		entry.URL = entry.RepoURL
		entry.Icon = storeIcon(entry.Icon)
		if entry.Directory != "" {
			entry.URL += "#" + entry.Directory
		}
		if !pluginURL(entry.URL) || !pluginID.MatchString(entry.ID) || entry.Title == "" || len(entry.Title) > 256 || len(entry.Description) > 8192 || len(entry.Version) > 64 {
			continue
		}
		out = append(out, entry)
	}
	return out, nil
}
