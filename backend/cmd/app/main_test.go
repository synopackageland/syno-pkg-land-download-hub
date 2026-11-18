package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// Match this App's manifest-linked capabilities in direct Go test builds.
func testBuildConfig() {
	delegateScope, delegateOperationsJSON, delegateDestinationsJSON, delegateLifetime = "install-selected", `["files.moveFromStaging"]`, `["save"]`, "until-revoked"
	portal = "executor"
}
func TestMain(m *testing.M) { testBuildConfig(); os.Exit(m.Run()) }

type testAuth struct {
	identity api.Identity
	err      error
	calls    int
}

func (a *testAuth) Authenticate(*http.Request) (api.Identity, error) {
	a.calls++
	return a.identity, a.err
}
func invoke(h http.Handler, method string) map[string]any {
	r := httptest.NewRequest("POST", "http://nas/cgi/api.cgi", strings.NewReader(`{"method":"`+method+`","params":{}}`))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SYNO-TOKEN", "test-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		panic(err)
	}
	return result
}
func code(result map[string]any) string {
	if e, ok := result["error"].(map[string]any); ok {
		return e["code"].(string)
	}
	return ""
}

func TestMobileEntryStatusUsesAuthenticatedSDKMetadata(t *testing.T) {
	auth := &testAuth{identity: api.Identity{User: "fixture-user"}}
	server, err := newAppServer(auth, appRoutes(t.TempDir()), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://app.example/cgi/api.cgi", strings.NewReader(`{"method":"app.status","params":{}}`))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-SYNO-TOKEN", "fixture-token")
	r.Header.Set("Accept-Language", "zh-TW, en;q=0.9")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	var result struct {
		Success bool          `json:"success"`
		Data    api.AppStatus `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || result.Data.Package != packageID || result.Data.Version != version || result.Data.User != "fixture-user" || result.Data.Admin || result.Data.Locale != "zh-TW" {
		t.Fatalf("entry metadata: %s", w.Body.String())
	}
	auth.err = api.ErrNoSession
	if got := code(invoke(server, "app.status")); got != "session_required" {
		t.Fatalf("anonymous entry metadata: %s", got)
	}
}

func TestDescriptionUsesTheProductionDSMAndEntryGate(t *testing.T) {
	before := entry
	entry = "app-privilege"
	t.Cleanup(func() { entry = before })
	dir := t.TempDir()
	a := &testAuth{err: api.ErrNoSession}
	server, err := newAppServer(a, appRoutes(t.TempDir()), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := code(invoke(server, "synopkgland.describe")); got != "session_required" {
		t.Fatal(got)
	}
	withCookie := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.AddCookie(&http.Cookie{Name: "id", Value: "fixture-session"})
		server.ServeHTTP(w, r)
	})
	a.err, a.identity = nil, api.Identity{User: "normal1"}
	cacheEntryDecision(t, dir, "normal1", false)
	if got := code(invoke(withCookie, "synopkgland.describe")); got != "app_privilege_denied" {
		t.Fatal(got)
	}
	cacheEntryDecision(t, dir, "normal1", true)
	// Unit fixture has no installed description: only authorized callers reach
	// that lookup. Build/inspect and the device gate cover the installed file.
	if got := code(invoke(withCookie, "synopkgland.describe")); got != "unknown_method" {
		t.Fatal(got)
	}
}
func TestProductionAPIAuthorizationMatrix(t *testing.T) {
	delegateScope = "home"
	delegateOperationsJSON = `["files.moveFromStaging"]`
	delegateDestinationsJSON = `["save"]`
	delegateLifetime = "until-revoked"
	// dsm-package.yaml declares portal below DSM 7.2: the executor registers it.
	portal = "executor"
	t.Cleanup(func() {
		testBuildConfig()
	})
	data := t.TempDir() + "/untouched"
	routes := appRoutes(data)
	var methods []string
	for _, r := range routes {
		methods = append(methods, r.Method())
	}
	sort.Strings(methods)
	// The real-device refusal matrix must cover every route in production.
	raw, err := os.ReadFile("../../../generated/routes.json")
	if err != nil {
		t.Fatal(err)
	}
	var registry map[string]struct{ Permission string }
	if err = json.Unmarshal(raw, &registry); err != nil {
		t.Fatal(err)
	}
	var fixture struct{ User, Admin []string }
	for name, op := range registry {
		if op.Permission == "authenticated" {
			fixture.User = append(fixture.User, name)
		} else {
			fixture.Admin = append(fixture.Admin, name)
		}
	}
	matrix := append(append([]string{}, fixture.User...), fixture.Admin...)
	sort.Strings(matrix)
	if !reflect.DeepEqual(methods, matrix) {
		t.Fatalf("live API matrix drift: production %v, fixture %v", methods, matrix)
	}
	a := &testAuth{identity: api.Identity{User: "normal1"}}
	server, err := newAppServer(a, routes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	userMethods := map[string]bool{}
	for _, m := range fixture.User {
		userMethods[m] = true
	}
	for _, method := range append(methods, "future.unknown") {
		t.Run(method, func(t *testing.T) {
			before := a.calls
			got := code(invoke(server, method))
			if method == "future.unknown" {
				// The SDK tells signed-in callers that a method is unregistered.
				if got != "unknown_method" {
					t.Fatalf("got %q", got)
				}
			} else if (got == "admin_required") == userMethods[method] {
				t.Fatalf("got %q", got)
			}
			if a.calls != before+1 {
				t.Fatal("must authenticate once per request")
			}
		})
	}
	a.identity = api.Identity{User: "admin", Admin: true}
	for _, method := range methods {
		if got := code(invoke(server, method)); got == "admin_required" {
			t.Fatalf("admin policy refused %s", method)
		}
	}
	result := invoke(server, "gopeed.session")
	if result["success"] != true {
		t.Fatalf("admin refused: %v", result)
	}
	a.identity.Admin = false
	if result := invoke(server, "gopeed.session"); result["success"] != true || result["data"].(map[string]any)["admin"] != false {
		t.Fatalf("cached admin privilege: %v", result)
	}
	if got := code(invoke(server, "gopeed.settings.get")); got != "admin_required" {
		t.Fatal(got)
	}
	a.err = api.ErrNoSession
	if got := code(invoke(server, "gopeed.session")); got != "session_required" {
		t.Fatal(got)
	}
	a.err = errors.New("DSM unavailable")
	if got := code(invoke(server, "gopeed.session")); got != "auth_unavailable" {
		t.Fatal(got)
	}
}
func TestEveryBusinessRouteHasAnExplicitPermission(t *testing.T) {
	raw, err := os.ReadFile("../../internal/api/operations.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Operations map[string]struct{ Permission string }
	}
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	for _, route := range appRoutes(t.TempDir()) {
		if strings.HasPrefix(route.Method(), "nas.") {
			continue
		}
		if _, ok := policy.Operations[route.Method()]; !ok {
			t.Errorf("missing explicit permission: %s", route.Method())
		}
	}
}

func TestProductionCGIRefusesJSONAmplificationBeforeAuth(t *testing.T) {
	for name, params := range map[string]string{
		"wide": "[" + strings.Repeat("0,", 65536) + "0]",
		"deep": strings.Repeat("[", 33) + "0" + strings.Repeat("]", 33),
	} {
		t.Run(name, func(t *testing.T) {
			a := &testAuth{err: api.ErrNoSession}
			data := t.TempDir() + "/untouched"
			server, err := newAppServer(a, appRoutes(data), t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRequest("POST", "http://nas/cgi/api.cgi", strings.NewReader(`{"method":"gopeed.session","params":`+params+`}`))
			r.Header.Set("Sec-Fetch-Site", "same-origin")
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-SYNO-TOKEN", "forged-token")
			r.Header.Set("X-Syno-Admin", "true")
			w := httptest.NewRecorder()
			cgiHandler(server).ServeHTTP(w, r)
			var result map[string]any
			if err = json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if code(result) != "request_too_large" || a.calls != 0 {
				t.Fatalf("result=%v auth=%d", result, a.calls)
			}
			if _, err = os.Stat(data); !os.IsNotExist(err) {
				t.Fatalf("refused request touched private data: %v", err)
			}
		})
	}
}

// Model DSM's decision in the SDK's bounded cache, not a product AppPriv implementation.
func cacheEntryDecision(t *testing.T, dir, user string, allowed bool) {
	t.Helper()
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(user+"\x00SYNO.SDS.SynoPkgGopeed.Instance\x00192.0.2.1")))
	body, err := json.Marshal(map[string]any{key: map[string]any{"allowed": allowed, "at": time.Now().Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(dir, "entry"), 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(dir, "entry", "entry-cache.json"), body, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestAppPrivilegeProductionEntryAndRoleMatrix(t *testing.T) {
	before := entry
	entry = "app-privilege"
	t.Cleanup(func() { entry = before })
	dir := t.TempDir()
	a := &testAuth{identity: api.Identity{User: "normal1"}}
	executed := 0
	routes := appRoutes(t.TempDir())
	for i, route := range routes {
		switch route.Method() {
		case "gopeed.session":
			routes[i] = api.Handle("gopeed.session", func(context.Context, api.Caller, json.RawMessage) (any, error) { executed++; return true, nil })
		case "gopeed.settings.set":
			routes[i] = api.Handle("gopeed.settings.set", func(context.Context, api.Caller, json.RawMessage) (any, error) { executed++; return true, nil })
		}
	}
	server, err := newAppServer(a, routes, dir)
	if err != nil {
		t.Fatal(err)
	}
	cacheEntryDecision(t, dir, "normal1", false)
	for _, m := range []string{"gopeed.session", "gopeed.settings.set", "future.unknown"} {
		if got := code(invoke(server, m)); got != "app_privilege_denied" {
			t.Fatalf("before grant %s: %q", m, got)
		}
	}
	if executed != 0 {
		t.Fatal("entry denial reached handler")
	}
	cacheEntryDecision(t, dir, "normal1", true)
	if got := code(invoke(server, "gopeed.session")); got != "" {
		t.Fatal("after grant", got)
	}
	if got := code(invoke(server, "gopeed.settings.set")); got != "admin_required" {
		t.Fatal("global setting", got)
	}
	a.identity = api.Identity{User: "admin", Admin: true}
	for _, m := range []string{"gopeed.session", "gopeed.settings.set"} {
		if got := code(invoke(server, m)); got != "" {
			t.Fatal("admin", got)
		}
	}
	a.err = api.ErrNoSession
	if got := code(invoke(server, "gopeed.session")); got != "session_required" {
		t.Fatal("anonymous", got)
	}
}
func TestEntryRefusalsRetainSpecificAPIErrorCodes(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{api.ErrAdminRequired, "admin_required"}, {api.ErrAppPrivilegeDenied, "app_privilege_denied"}} {
		server, err := newAppServer(&testAuth{err: fmt.Errorf("wrapped: %w", tc.err)}, appRoutes(t.TempDir()), t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if got := code(invoke(server, "gopeed.session")); got != tc.code {
			t.Fatalf("%s: %s", tc.code, got)
		}
	}
}

func TestAppPrivilegeDenialPrecedesEveryRegisteredProductionMethod(t *testing.T) {
	oldEntry, oldScope, oldOps, oldKeys, oldLifetime, oldPortal := entry, delegateScope, delegateOperationsJSON, delegateDestinationsJSON, delegateLifetime, portal
	entry = "app-privilege"
	portal = "executor"
	delegateScope = "install-selected"
	delegateOperationsJSON = `["files.moveFromStaging"]`
	delegateDestinationsJSON = `["save"]`
	delegateLifetime = "until-revoked"
	t.Cleanup(func() {
		entry = oldEntry
		delegateScope = oldScope
		delegateOperationsJSON = oldOps
		delegateDestinationsJSON = oldKeys
		delegateLifetime = oldLifetime
		portal = oldPortal
	})
	dir := t.TempDir()
	cacheEntryDecision(t, dir, "normal1", false)
	routes := appRoutes(filepath.Join(dir, "gopeed"))
	server, e := newAppServer(&testAuth{identity: api.Identity{User: "normal1"}}, routes, dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, r := range routes {
		if got := code(invoke(server, r.Method())); got != "app_privilege_denied" {
			t.Fatalf("%s: %s", r.Method(), got)
		}
	}
}

// Native transfer forms are prepared by the SDK before the authenticator runs.
func TestSessionTokenValidation(t *testing.T) {
	for name, tokens := range map[string][]string{
		"missing": nil, "empty": {""}, "duplicate": {"one", "two"},
		"oversize": {strings.Repeat("a", 4097)}, "newline": {"one\ntwo"},
		"nul": {"one\x00two"}, "valid": {"fixture-token"},
	} {
		t.Run(name, func(t *testing.T) {
			auth := &testAuth{identity: api.Identity{User: "fixture"}}
			r := httptest.NewRequest("POST", "http://nas/cgi/api.cgi", nil)
			r.Header["X-Syno-Token"] = tokens
			_, err := (sessionAuthenticator{auth}).Authenticate(r)
			if name == "valid" {
				if err != nil || auth.calls != 1 {
					t.Fatalf("err=%v calls=%d", err, auth.calls)
				}
			} else if !errors.Is(err, api.ErrNoSession) || auth.calls != 0 {
				t.Fatalf("err=%v calls=%d", err, auth.calls)
			}
		})
	}
}

func TestFormTokenTransferReachesStreamRoute(t *testing.T) {
	a := &testAuth{identity: api.Identity{User: "admin", Admin: true}}
	streamed := false
	routes := appRoutes(t.TempDir())
	for i, route := range routes {
		if route.Method() == "nas.files.download" {
			routes[i] = api.HandleStream("nas.files.download", func(_ context.Context, _ api.Caller, w http.ResponseWriter, _ *http.Request) error {
				streamed = true
				return nil
			})
		}
	}
	server, err := newAppServer(a, routes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "http://nas/cgi/api.cgi?method=nas.files.download&path=/share/a.txt", strings.NewReader("SynoToken=form-token"))
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	cgiHandler(server).ServeHTTP(w, r)
	if !streamed || a.calls != 1 {
		t.Fatalf("streamed=%v auth=%d body=%s", streamed, a.calls, w.Body.String())
	}
}

// SDK 0.9.9: a state-changing request must prove its origin before any route
// or session check runs; browsers send these headers, scripts must add them.
func TestAppServerRefusesPostsWithoutOrigin(t *testing.T) {
	a := &testAuth{identity: api.Identity{User: "admin", Admin: true}}
	server, err := newAppServer(a, appRoutes(t.TempDir()+"/untouched"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for name, header := range map[string][2]string{"missing": {}, "cross-site": {"Origin", "https://foreign.invalid"}} {
		t.Run(name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "http://nas/cgi/api.cgi", strings.NewReader(`{"method":"gopeed.session","params":{}}`))
			r.Header.Set("Content-Type", "application/json")
			if header[0] != "" {
				r.Header.Set(header[0], header[1])
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)
			var out map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatal(w.Body.String())
			}
			want := map[string]string{"missing": "origin_required", "cross-site": "cross_origin"}[name]
			if got := code(out); got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		})
	}
}

// A core update downloads for up to 3 minutes and waits 40 seconds for the
// new engine, so dsm-package.yaml sets backend.timeout_seconds: 240, which the
// build passes as requestTimeout.
func TestCGIHandlerGivesRoutesTheBuildRequestDeadline(t *testing.T) {
	saved := requestTimeout
	t.Cleanup(func() { requestTimeout = saved })
	requestTimeout = "240"
	deadline := api.Handle("gopeed.session", func(ctx context.Context, _ api.Caller, _ json.RawMessage) (any, error) {
		at, ok := ctx.Deadline()
		if !ok {
			return nil, errors.New("no deadline")
		}
		return time.Until(at).Seconds(), nil
	})
	routes := appRoutes(t.TempDir())
	for i, route := range routes {
		if route.Method() == deadline.Method() {
			routes[i] = deadline
		}
	}
	server, err := newAppServer(&testAuth{identity: api.Identity{User: "admin", Admin: true}}, routes, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := invoke(cgiHandler(server), "gopeed.session")
	if left, _ := out["data"].(float64); left <= 230 || left > 240 {
		t.Fatalf("deadline: %v", out)
	}
}

func TestRequestDeadlineFollowsTheBuildSetting(t *testing.T) {
	saved := requestTimeout
	t.Cleanup(func() { requestTimeout = saved })
	for setting, want := range map[string]time.Duration{
		"":    api.DefaultRequestTimeout,
		"120": 120 * time.Second,
		"1":   time.Second,
		"0":   api.DefaultRequestTimeout,
		"-5":  api.DefaultRequestTimeout,
		"x":   api.DefaultRequestTimeout,
	} {
		requestTimeout = setting
		var left time.Duration
		api.WithRequestDeadline(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			d, _ := r.Context().Deadline()
			left = time.Until(d)
		}), deadline()).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil))
		if left > want || left < want-time.Second {
			t.Errorf("requestTimeout %q: deadline in %v, want %v", setting, left, want)
		}
	}
}

// a stopped package's App names its lifecycle, from the executor
// mailbox, both in the SDK's delegate.status and where the engine cannot
// answer, so the SDK says the App is stopped and the page asks for nothing.
func TestAStoppedPackageAnswersAPP_STOPPED(t *testing.T) {
	delegateScope, delegateOperationsJSON, delegateDestinationsJSON, delegateLifetime = "home", `["files.moveFromStaging"]`, `["save"]`, "until-revoked"
	t.Cleanup(func() {
		testBuildConfig()
		localExecutor = delegate.Local
	})
	root := t.TempDir()
	localExecutor = func(id string) *delegate.Client {
		client := delegate.Local(id)
		client.Mailbox = filepath.Join(root, "executor")
		return client
	}
	data := filepath.Join(root, "gopeed")
	for name, content := range map[string]string{"executor/control": "stop", "gopeed/engine.token": strings.Repeat("a", 64)} {
		os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0700)
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	// The App owns downloads by the caller's local account.
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	server, err := newAppServer(&testAuth{identity: api.Identity{User: account.Username, Admin: true}}, appRoutes(data), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for method, params := range map[string]string{"delegate.status": `{}`, "gopeed.request": `{"method":"GET","path":"/api/v1/tasks","query":{},"data":null}`} {
		r := httptest.NewRequest("POST", "http://nas/cgi/api.cgi", strings.NewReader(`{"method":"`+method+`","params":`+params+`}`))
		r.Header.Set("Sec-Fetch-Site", "same-origin")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-SYNO-TOKEN", "test-token")
		r.AddCookie(&http.Cookie{Name: "id", Value: "session"})
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), `"APP_STOPPED"`) {
			t.Fatal(method, w.Body.String())
		}
	}
}
