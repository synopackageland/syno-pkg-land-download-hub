package gopeed

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// githubRefusal explains a GitHub refusal to the page and to the command
// line alike: a limit says when it resets and where a token raises it.
// The page reads the reset time from the message.
func githubRefusal(code string, err error) error {
	var rl *binaries.RateLimitError
	switch {
	case errors.As(err, &rl):
		return &api.Error{Code: code, Message: "GitHub rate limit reached; it resets at " + rl.Reset.UTC().Format(time.RFC3339) + ". Add a GitHub token in Settings > GitHub to raise the limit"}
	case errors.Is(err, githubtoken.ErrInvalid):
		return &api.Error{Code: "github_token_invalid", Message: "The GitHub token is invalid or expired; replace or remove it in Settings > GitHub"}
	}
	return nil
}

func (s *Service) githubClient() *http.Client {
	if s.githubHTTP != nil {
		return s.githubHTTP
	}
	return &http.Client{Timeout: 15 * time.Second, Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 10 * time.Second}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

// githubRoutes keep the administrator's optional token. It is checked with
// GitHub once when saved and never sent back to the browser.
func (s *Service) githubRoutes() []api.Route {
	store := func() githubtoken.Store { return githubtoken.At(s.Data) }
	return []api.Route{
		api.Handle("gopeed.github.status", func(_ context.Context, c api.Caller, raw json.RawMessage) (any, error) {
			if err := api.RequireAdmin(c); err != nil {
				return nil, err
			}
			if err := decodeStrict(raw, &struct{}{}); err != nil {
				return nil, err
			}
			return store().Status(), nil
		}),
		api.Handle("gopeed.github.set", func(ctx context.Context, c api.Caller, raw json.RawMessage) (any, error) {
			if err := api.RequireAdmin(c); err != nil {
				return nil, err
			}
			var in struct {
				Token string `json:"token"`
			}
			if err := decodeStrict(raw, &in); err != nil {
				return nil, err
			}
			if !githubtoken.Valid(in.Token) {
				return nil, refused("github_token_format")
			}
			limit, err := githubtoken.Validate(ctx, s.githubClient(), in.Token)
			if errors.Is(err, githubtoken.ErrInvalid) {
				return nil, githubRefusal("", err)
			} else if err != nil {
				return nil, refused("github_unavailable")
			}
			st := store()
			if err = st.Save(in.Token); err != nil {
				return nil, refused("github_token_storage")
			}
			return struct {
				githubtoken.Status
				githubtoken.Limit
			}{st.Status(), limit}, nil
		}),
		api.Handle("gopeed.github.remove", func(_ context.Context, c api.Caller, raw json.RawMessage) (any, error) {
			if err := api.RequireAdmin(c); err != nil {
				return nil, err
			}
			if err := decodeStrict(raw, &struct{}{}); err != nil {
				return nil, err
			}
			if err := store().Remove(); err != nil {
				return nil, refused("github_token_storage")
			}
			return store().Status(), nil
		}),
	}
}
