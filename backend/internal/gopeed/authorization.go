package gopeed

import (
	appapi "dsmapp/internal/api"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

// DescribeRoutes uses exactly the same central policy as the request server.
func DescribeRoutes(routes ...api.Route) ([]map[string]string, error) {
	raw, err := appapi.Permissions()
	if err != nil {
		return nil, err
	}
	return api.DescribeRoutes(raw, routes...)
}
