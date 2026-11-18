package sdkcontrol

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// executor.status and executor.control are the SDK's, as the bridge runtime serves them.
func (cfg Config) rootRoutes() []api.Route {
	return append(delegate.ExecutorRoutes(cfg.executor(), cfg.DataDir), api.HandleRequest("root.run", cfg.executorRootRun))
}

func (cfg Config) executorRootRun(ctx context.Context, caller api.Caller, req *http.Request, raw json.RawMessage) (any, error) {
	if !caller.Admin() {
		return nil, errors.New("admin_required")
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeParams(raw, &in); err != nil {
		return nil, err
	}
	// Executor independently verifies both session values and administrator identity.
	auth, err := api.DSMSession(req)
	if err != nil {
		return nil, err
	}
	if err := cfg.executorHoldsThisBuild(ctx); err != nil {
		return nil, err
	}
	var out json.RawMessage
	if err := cfg.executor().Call(ctx, "root.run", auth, map[string]string{"name": in.Name}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// executorHoldsThisBuild refuses, as delegate does, an executor pinned to
// another build's policy or executor: its approved scripts are not this build's.
func (cfg Config) executorHoldsThisBuild(ctx context.Context) error {
	var pins delegate.ExecutorStatus
	if err := cfg.executor().Call(ctx, "status", nil, nil, &pins); err != nil {
		return err
	}
	if (cfg.ExecutorDigest != "" && pins.ExecutorDigest != cfg.ExecutorDigest) || (cfg.PolicyDigest != "" && pins.PolicyDigest != cfg.PolicyDigest) {
		return &api.Error{Code: delegate.CodePolicyDenied, Message: "the executor holds another build's approved policy; an administrator approves this build in Administrator setup"}
	}
	return nil
}

// executorSocketBase is a variable so tests can use a temporary directory.
var executorSocketBase = "/run/synopkgland"

// executor keeps the executor's protocol v1 refusal codes (POLICY_DENIED,
// EXECUTOR_UNAVAILABLE, ...) as the API error code. Without its own Timeout
// the client waits as the SDK says: delegate.ExecutorTimeout, root.run longer.
func (cfg Config) executor() *delegate.Client {
	return &delegate.Client{Socket: filepath.Join(executorSocketBase, cfg.PackageID, "executor.sock")}
}

// Routes exposes the generated fixed executor setup/control routes.
func (cfg Config) Routes() []api.Route { return append(cfg.rootRoutes(), cfg.portalRoutes()...) }
