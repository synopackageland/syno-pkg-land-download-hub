package gopeed

import (
	appapi "dsmapp/internal/api"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"net/http"
)

// Partial test servers omit SDK capabilities; production uses NewServerWithOperations.
// NewServer preserves the exact permissions bytes bound by the SDK build.
// SDK routes keep their own policy and AppPriv precedes every route.
func NewServer(auth api.Authenticator, routes ...api.Route) (http.Handler, error) {
	raw, err := appapi.Permissions()
	if err != nil {
		return nil, err
	}
	// The production composition adds the SDK entry metadata route alongside
	// this service. Include it here too, preserving the exact central policy.
	routes = append(routes, api.HandleRequest("app.status", api.AppStatusHandler("SynoPkgGopeed", "dev", nil)))
	return api.NewServerWithPermissions(auth, raw, routes...)
}
