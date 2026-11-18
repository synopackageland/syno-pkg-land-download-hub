package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/mediactl/clustarr/pkg/cardigann"
)

// Each instance is private to one source's helper run. Cookies and browser UA
// never cross source/owner/process boundaries or enter stored source values.
type solverTransport struct {
	base     http.RoundTripper
	endpoint string
	client   *http.Client
	jar      http.CookieJar
	agents   map[string]string
	validate func(context.Context, *http.Request) error
}

func newSolverTransport(base http.RoundTripper, endpoint string) *solverTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if endpoint != "" {
		endpoint = strings.TrimRight(endpoint, "/")
		if !strings.HasSuffix(endpoint, "/v1") {
			endpoint += "/v1"
		}
	}
	jar, _ := cookiejar.New(nil)
	return &solverTransport{base: base, endpoint: endpoint, jar: jar, agents: map[string]string{}, validate: publicSolverTarget, client: &http.Client{Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 70 * time.Second, MaxResponseHeaderBytes: 64 << 10}, Timeout: 70 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func publicSolverTarget(ctx context.Context, r *http.Request) error {
	u := r.URL
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return errors.New("solver target refused")
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil || len(ips) == 0 {
		return errors.New("solver target refused")
	}
	for _, ip := range ips {
		if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
			return errors.New("solver target refused")
		}
	}
	return nil
}
func origin(u *url.URL) string { return u.Scheme + "://" + strings.ToLower(u.Host) }
func (t *solverTransport) RoundTrip(original *http.Request) (*http.Response, error) {
	r := original.Clone(original.Context())
	if agent := t.agents[origin(r.URL)]; agent != "" {
		r.Header.Set("User-Agent", agent)
		for _, c := range t.jar.Cookies(r.URL) {
			r.AddCookie(c)
		}
	}
	res, err := t.base.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	// Inspect the same bounded body the engine accepts. The response is restored
	// for its parser; ordinary 403/503 responses are never sent to the solver.
	b, err := io.ReadAll(io.LimitReader(res.Body, (8<<20)+1))
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(b) > 8<<20 {
		return nil, errors.New("solver response too large")
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	if !cardigann.IsCloudflareChallenge(res.Header, b) {
		return res, nil
	}
	if t.endpoint == "" {
		return res, nil
	}
	// The browser API cannot enforce the engine's redirect credential policy.
	// Restrict fallback to anonymous GETs; never replay login forms or send user
	// cookies, auth headers or API keys to a remote browser.
	challenge := &cardigann.CloudflareChallengeError{StatusCode: res.StatusCode}
	if original.Method != "GET" || urlCredentials(original.URL) || original.Header.Get("Cookie") != "" || original.Header.Get("Authorization") != "" || original.Header.Get("X-Api-Key") != "" {
		res.Body.Close()
		return nil, challenge
	}
	if err = t.validate(r.Context(), r); err != nil {
		res.Body.Close()
		return nil, err
	}
	res.Body.Close()
	payload, _ := json.Marshal(map[string]any{"cmd": "request.get", "url": r.URL.String(), "maxTimeout": 60000})
	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", t.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, challenge
	}
	req.Header.Set("Content-Type", "application/json")
	solved, err := t.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, challenge
	}
	defer solved.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(solved.Body, (12<<20)+1))
	if err != nil || len(raw) > 12<<20 || solved.StatusCode != 200 {
		return nil, challenge
	}
	var out struct {
		Status   string `json:"status"`
		Solution struct {
			Status    int    `json:"status"`
			URL       string `json:"url"`
			Response  string `json:"response"`
			UserAgent string `json:"userAgent"`
			Cookies   []struct {
				Name, Value, Domain, Path string
				Expires                   float64
				Secure                    bool
			} `json:"cookies"`
		} `json:"solution"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Status != "ok" || out.Solution.Status < 200 || out.Solution.Status >= 300 || len(out.Solution.Response) > 8<<20 {
		return nil, challenge
	}
	final, err := url.Parse(out.Solution.URL)
	if err != nil || origin(final) != origin(r.URL) || final.User != nil || final.Fragment != "" {
		return nil, challenge
	}
	finalReq := r.Clone(ctx)
	finalReq.URL = final
	if t.validate(ctx, finalReq) != nil {
		return nil, challenge
	}
	if cardigann.IsCloudflareChallenge(http.Header{}, []byte(out.Solution.Response)) {
		return nil, challenge
	}
	agent := out.Solution.UserAgent
	if agent == "" || len(agent) > 2048 || strings.ContainsAny(agent, "\r\n\x00") || len(out.Solution.Cookies) > 64 {
		return nil, challenge
	}
	cookies := []*http.Cookie{}
	total := 0
	for _, c := range out.Solution.Cookies {
		total += len(c.Name) + len(c.Value)
		if total > 16<<10 || strings.ContainsAny(c.Name+c.Value, "\r\n\x00;") {
			return nil, challenge
		}
		domain := strings.TrimPrefix(strings.ToLower(c.Domain), ".")
		host := strings.ToLower(r.URL.Hostname())
		if domain != "" && domain != host && !strings.HasSuffix(host, "."+domain) {
			continue
		}
		cookie := &http.Cookie{Name: c.Name, Value: c.Value, Path: c.Path, Secure: c.Secure}
		if c.Expires > 0 {
			cookie.Expires = time.Unix(int64(c.Expires), 0)
		}
		cookies = append(cookies, cookie)
	}
	t.jar.SetCookies(r.URL, cookies)
	t.agents[origin(r.URL)] = agent
	return &http.Response{StatusCode: out.Solution.Status, Header: http.Header{"Content-Type": []string{"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(out.Solution.Response)), Request: r}, nil
}
func withSolver(client *http.Client, endpoint string) *http.Client {
	clone := *client
	clone.Transport = newSolverTransport(client.Transport, endpoint)
	if endpoint != "" {
		clone.Timeout = 85 * time.Second
	}
	return &clone
}
