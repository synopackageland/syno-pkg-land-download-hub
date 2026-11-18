package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	appapi "dsmapp/internal/api"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// stepRoutes expose the internal steps of a download submission (resolve,
// create and the media probe) so tests can drive each one on its own. They
// exist only in tests; the App's routes submit whole batches.
func stepRoutes(s *Service) []api.Route {
	owned := func(name string, fn func(context.Context, api.Caller, *http.Request, string, json.RawMessage) (any, error)) api.Route {
		return api.HandleRequest(name, func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			owner, err := s.Owner(c.User())
			if err != nil {
				return nil, refused("identity_unavailable")
			}
			return fn(ctx, c, r, owner, raw)
		})
	}
	return []api.Route{
		api.HandleRequest("test.resolve", s.resolveSource),
		api.HandleRequest("test.create", func(ctx context.Context, c api.Caller, r *http.Request, raw json.RawMessage) (any, error) {
			return s.locked(ctx, c, func(owner string, st *State) (any, error) { return s.create(ctx, r, c, owner, st, raw) })
		}),
		owned("test.media.probe", s.probeMedia),
		owned("test.media.create", s.createMedia),
	}
}

// testServer serves the App's routes and the step routes to authenticated users.
func testServer(s *Service, auth api.Authenticator) (http.Handler, error) {
	raw, err := appapi.Permissions()
	if err != nil {
		return nil, err
	}
	var policy map[string]any
	if err = json.Unmarshal(raw, &policy); err != nil {
		return nil, err
	}
	methods := map[string]any{}
	routes := append(s.Routes(), stepRoutes(s)...)
	for _, route := range routes {
		if rule, ok := policy["methods"].(map[string]any)[route.Method()]; ok {
			methods[route.Method()] = rule
		}
	}
	for _, route := range stepRoutes(s) {
		methods[route.Method()] = "authenticated"
	}
	policy["methods"] = methods
	if raw, err = json.Marshal(policy); err != nil {
		return nil, err
	}
	return api.NewServerWithPermissions(auth, raw, routes...)
}

// resolveAndCreate submits one address through both steps, as a batch link does.
func resolveAndCreate(t *testing.T, s *Service, user string, task any) map[string]any {
	t.Helper()
	out, _ := call(t, s, user, "test.resolve", mustMarshal(task))
	if out["success"] != true {
		return out
	}
	rid, _ := out["data"].(map[string]any)["data"].(map[string]any)["id"].(string)
	out, _ = call(t, s, user, "test.create", mustMarshal(map[string]string{"rid": rid}))
	return out
}
