package main

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

func keyParameter(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return key == "apikey" || key == "api_key" || key == "jackett_apikey" || key == "passkey"
}
func redactLink(raw string) string {
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
func authenticateURL(u *url.URL, values map[string]string) {
	q := u.Query()
	found := false
	for key := range q {
		if keyParameter(key) {
			value := values["apikey"]
			if strings.EqualFold(key, "passkey") {
				value = values["passkey"]
			}
			if value != "" {
				q.Set(key, value)
				found = true
			}
		}
	}
	if !found && values["apikey"] != "" {
		q.Set("apikey", values["apikey"])
	}
	u.RawQuery = q.Encode()
}
func feedClient(client *http.Client, in request) *http.Client {
	safeValues := map[string]string{}
	for k, v := range in.Values {
		if in.Kind != "torznab" || !keyParameter(k) {
			safeValues[k] = v
		}
	}
	clone := *withPrivacy(client, nil, safeValues)
	previous := client.CheckRedirect
	origin, _ := url.Parse(in.URL)
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if previous != nil {
			if err := previous(req, via); err != nil {
				return err
			}
		}
		if origin != nil && strings.EqualFold(origin.Host, req.URL.Host) && origin.Scheme == req.URL.Scheme {
			if in.Kind == "torznab" {
				authenticateURL(req.URL, in.Values)
			}
		} else {
			q := req.URL.Query()
			for k := range q {
				if keyParameter(k) {
					q.Del(k)
				}
			}
			req.URL.RawQuery = q.Encode()
			req.Header.Del("X-Api-Key")
			req.Header.Del("Cookie")
			req.Header.Del("Authorization")
		}
		return nil
	}
	return &clone
}

func feed(ctx context.Context, in request, client *http.Client) reply {
	rep := feedRaw(ctx, in, client)
	for i := range rep.Results {
		hit := &rep.Results[i]
		hit.Link = redactLink(hit.Link)
		for key, value := range in.Values {
			if keyParameter(key) && value != "" {
				hit.Title = strings.ReplaceAll(hit.Title, value, "***")
				hit.Link = strings.ReplaceAll(hit.Link, value, "***")
				hit.Hash = strings.ReplaceAll(hit.Hash, value, "***")
			}
		}
	}
	return rep
}
