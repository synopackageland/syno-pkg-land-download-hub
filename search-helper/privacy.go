package main

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/mediactl/clustarr/pkg/cardigann"
)

func secretName(name string) bool {
	n := strings.ToLower(name)
	for _, part := range []string{"password", "passkey", "apikey", "api_key", "cookie", "rsskey", "username", "token", "secret", "authorization"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}
func urlCredentials(u *url.URL) bool {
	if u.User != nil {
		return true
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return true
	}
	for key := range query {
		if secretName(key) {
			return true
		}
	}
	return false
}
func sensitive(def *cardigann.Definition) map[string]bool {
	names := map[string]bool{}
	if def != nil {
		for _, f := range def.Settings {
			if f.Type == "password" || secretName(f.Name) {
				names[f.Name] = true
			}
		}
	}
	return names
}
func credentialsInURLs(def *cardigann.Definition) bool {
	names := sensitive(def)
	has := func(s string) bool {
		for name := range names {
			if strings.Contains(s, ".Config."+name) {
				return true
			}
		}
		return false
	}
	inputs := func(m map[string]cardigann.Scalar) bool {
		for _, s := range m {
			if has(string(s)) {
				return true
			}
		}
		return false
	}
	if has(def.Search.Path) {
		return true
	}
	get := len(def.Search.Paths) == 0
	for _, p := range def.Search.Paths {
		if has(p.Path) {
			return true
		}
		if strings.ToUpper(strings.TrimSpace(p.Method)) != "POST" {
			get = true
			if inputs(p.Inputs) {
				return true
			}
		}
	}
	if get && inputs(def.Search.Inputs) {
		return true
	}
	if def.Login != nil {
		if has(def.Login.Path) {
			return true
		}
		if (def.Login.Method == "get" || def.Login.Method == "oneurl") && inputs(def.Login.Inputs) {
			return true
		}
	}
	return false
}

type privateTransport struct {
	base   http.RoundTripper
	values []string
}

func (t privateTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	decoded, _ := url.QueryUnescape(req.URL.String())
	for _, v := range t.values {
		if v != "" && strings.Contains(decoded, v) {
			return nil, errors.New("credentials_in_url")
		}
	}
	return t.base.RoundTrip(req)
}
func withPrivacy(client *http.Client, def *cardigann.Definition, values map[string]string) *http.Client {
	clone := *client
	rt := client.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	keys := sensitive(def)
	secrets := []string{}
	for key, value := range values {
		if keys[key] || secretName(key) {
			secrets = append(secrets, value)
		}
	}
	clone.Transport = privateTransport{rt, secrets}
	return &clone
}
