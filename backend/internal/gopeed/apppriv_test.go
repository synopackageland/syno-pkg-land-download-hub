package gopeed

import (
	"encoding/json"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type ordinaryEntryAuth struct{}

func (ordinaryEntryAuth) Authenticate(*http.Request) (api.Identity, error) {
	return api.Identity{User: "normal1"}, nil
}
func TestGlobalHandlersRequireAdminEvenInsideAuthenticatedRoute(t *testing.T) {
	s := New(t.TempDir())
	for _, route := range s.adminRoutes() {
		if route.Method() == "gopeed.session" {
			continue
		}
		t.Run(route.Method(), func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"schema": 1, "default": "administrators", "methods": map[string]string{route.Method(): "authenticated"}})
			server, e := api.NewServerWithPermissions(ordinaryEntryAuth{}, raw, route)
			if e != nil {
				t.Fatal(e)
			}
			r := httptest.NewRequest("POST", "http://nas/api", strings.NewReader(`{"method":"`+route.Method()+`","params":{}}`))
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			var out struct {
				Error *api.Error `json:"error"`
			}
			if e = json.Unmarshal(w.Body.Bytes(), &out); e != nil {
				t.Fatal(e)
			}
			if out.Error == nil || out.Error.Code != "admin_required" {
				t.Fatalf("global route accepted ordinary subject: %+v", out.Error)
			}
		})
	}
}
