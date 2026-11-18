package search

import (
	"net/url"
	"strings"
)

func keyParameter(k string) bool {
	k = strings.ToLower(strings.ReplaceAll(k, "-", "_"))
	return k == "apikey" || k == "api_key" || k == "jackett_apikey" || k == "passkey"
}

// RedactURL is applied before an upstream link enters a result, ticket or job.
// Query credentials are reconstructed only inside the private HTTP helper.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for k := range q {
		if keyParameter(k) {
			q.Set(k, "***")
		}
	}
	u.RawQuery = strings.ReplaceAll(q.Encode(), "%2A%2A%2A", "***")
	u.User = nil
	return u.String()
}
func sourceKey(in *SourceInput) {
	if in.Kind != "torznab" {
		return
	}
	u, err := url.Parse(in.URL)
	if err != nil {
		return
	}
	q := u.Query()
	for k, values := range q {
		if keyParameter(k) && len(values) > 0 && values[0] != "" && values[0] != "***" {
			if in.Values == nil {
				in.Values = map[string]string{}
			}
			key := "apikey"
			if strings.EqualFold(k, "passkey") {
				key = "passkey"
			}
			if in.Values[key] == "" {
				in.Values[key] = values[0]
			}
		}
	}
	in.URL = RedactURL(in.URL)
}
func redactHit(hit Hit, values map[string]string) Hit {
	hit.Link = RedactURL(hit.Link)
	for key, value := range values {
		if (keyParameter(key) || strings.Contains(strings.ToLower(key), "password")) && value != "" {
			hit.Title = strings.ReplaceAll(hit.Title, value, "***")
			hit.Date = strings.ReplaceAll(hit.Date, value, "***")
			hit.Link = strings.ReplaceAll(hit.Link, value, "***")
		}
	}
	return hit
}

// Source URLs are display-only. Mask every query value (including custom
// credential names) and recognizable path credentials; keep the private URL.
func sourceURL(src Source) string {
	u, err := url.Parse(src.URL)
	if err != nil || src.URL == "" {
		return ""
	}
	u.User = nil
	u.Fragment = ""
	q := u.Query()
	for k := range q {
		q.Set(k, "***")
	}
	u.RawQuery = strings.ReplaceAll(q.Encode(), "%2A%2A%2A", "***")
	parts := strings.Split(u.Path, "/")
	for i, part := range parts {
		if i > 0 && (keyParameter(parts[i-1]) || strings.Contains(strings.ToLower(parts[i-1]), "token")) {
			parts[i] = "***"
			continue
		}
		if len(part) >= 24 && !strings.Contains(part, ".") {
			parts[i] = "***"
		}
		for _, secret := range src.Values {
			if secret != "" {
				parts[i] = strings.ReplaceAll(parts[i], secret, "***")
			}
		}
	}
	u.Path = strings.Join(parts, "/")
	u.RawPath = ""
	return u.String()
}
