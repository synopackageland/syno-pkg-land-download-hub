package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type appExecutor func(context.Context, string, map[string]string, any, any) error

func (f appExecutor) Call(ctx context.Context, op string, auth map[string]string, p, out any) error {
	return f(ctx, op, auth, p, out)
}
func encodeResult(out, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Real SDK admission/source pinning/journal/cleanup; the executor and DSM are
// fixtures. This does not certify destination bytes, UID or NAS reboot.
func TestGopeedCompletionUsesRealSDKAndDurableMirror(t *testing.T) {
	s, _ := stagedDelegate(t)
	moves := 0
	state := "queued"
	exec := appExecutor(func(_ context.Context, op string, auth map[string]string, p, out any) error {
		if len(auth) != 0 {
			t.Fatal("completion retained DSM credentials")
		}
		switch op {
		case "files.moveFromStaging":
			moves++
			return encodeResult(out, delegate.OperationResult{JobID: "gopeed-mine", State: "queued"})
		case "files.operationStatus":
			result := delegate.OperationResult{JobID: "gopeed-mine", State: state}
			if state == "committed" {
				result.Receipt = &delegate.Receipt{Path: "/home/Downloads/file.bin", Size: 8}
			}
			return encodeResult(out, result)
		default:
			t.Fatalf("unexpected SDK operation %s", op)
			return nil
		}
	})
	store := filepath.Join(t.TempDir(), "background")
	makeSDK := func() *delegate.Service {
		return &delegate.Service{Client: exec, Store: background.NewRequestStore(store), StagingDir: s.StagingDir, Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}}
	}
	s.Delegate = makeSDK()
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if moves != 1 {
		t.Fatal(moves)
	}
	binding, err := makeSDK().Store.MoveBinding(context.Background(), "gopeed-mine")
	if err != nil || binding.DelegationID != "grant1" {
		t.Fatal(binding, err)
	}
	state = "committed"
	s.Delegate = makeSDK()
	if err = s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !readJob(t, s).Published || moves != 1 {
		t.Fatal("lost receipt or readmitted")
	}
	if _, err = os.Stat(filepath.Join(s.StagingDir, "private/file.bin")); !os.IsNotExist(err) {
		t.Fatal("SDK must clean only committed source", err)
	}
}

func TestRealSDKAccessRefusalKeepsSourceAndReselectionDoesNotReviveOldJob(t *testing.T) {
	for _, code := range []string{"DELEGATION_DENIED"} {
		t.Run(code, func(t *testing.T) {
			s, _ := stagedDelegate(t)
			executorCalls := 0
			sdk := &delegate.Service{Store: background.NewRequestStore(filepath.Join(t.TempDir(), "background")), StagingDir: s.StagingDir, Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}}
			sdk.Client = appExecutor(func(_ context.Context, op string, auth map[string]string, _, _ any) error {
				executorCalls++
				if op != "files.moveFromStaging" || len(auth) != 0 {
					t.Fatal(op, "unexpected online authority or status retry")
				}
				return &api.Error{Code: code}
			})
			s.Delegate = sdk
			if err := s.SaveCompleted(context.Background()); saveCode(err) != code {
				t.Fatal(err)
			}
			binding, err := sdk.Store.MoveBinding(context.Background(), "gopeed-mine")
			if err != nil || binding.Refusal != code || binding.CleanupDone {
				t.Fatal(binding, err)
			}
			// The owner explicitly selects a fresh SDK destination. This affects new
			// downloads only, never the revoked grant in an already attempted job.
			s.DestinationDelegate = ownDestination{}
			out, _ := call(t, s, "alice", "gopeed.destination", map[string]string{"key": "save", "path": "/home"})
			if out["success"] != true {
				t.Fatal(out)
			}
			_, err = s.withState(context.Background(), func(st *State) (any, error) {
				if st.Grants["uid:alice"] != "grant-alice" || st.Jobs["mine"].DelegationID != "grant1" || st.Jobs["mine"].SaveState != "access_denied" {
					t.Fatal("reselection revived old attempt", st)
				}
				return nil, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SaveCompleted(context.Background()); err != nil {
				t.Fatal(err)
			}
			if err = sdk.SweepMoves(context.Background()); err != nil || executorCalls != 1 {
				t.Fatal("refused mirror retried", err, executorCalls)
			}
			job := readJob(t, s)
			if raw, err := os.ReadFile(filepath.Join(job.Stage, job.Filename)); err != nil || string(raw) != "download" {
				t.Fatal("staging removed", err)
			}
		})
	}
}

func TestPickerBindsActualSDKGrantAndRejectsHomeScopeEscape(t *testing.T) {
	s, _, _ := fixture(t)
	current, _ := user.Current()
	creates := 0
	sdk := &delegate.Service{Scope: "home", Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}, Destinations: []string{"save"}, LookupUID: func(string) string { return current.Uid }}
	sdk.Client = appExecutor(func(_ context.Context, op string, auth map[string]string, p, out any) error {
		if op == "status" {
			disabled, allowed := false, true
			return encodeResult(out, delegate.ExecutorStatus{ExecutorDigest: "executor", PolicyDigest: "policy", Version: "fixture", Ready: true, Disabled: &disabled, Allowed: &allowed})
		}
		if op == "delegate.list" {
			return encodeResult(out, map[string]any{"delegations": []delegate.Grant{{DelegationID: "grant1", Subject: delegate.Subject{Username: "alice", UID: json.Number(current.Uid)}, Lifetime: "until-revoked", Destination: "save", Path: "/home/Downloads", Scope: "home"}}})
		}
		if op != "delegate.create" || auth["dsm_cookie"] != "current-session" {
			t.Fatal(op)
		}
		if p.(map[string]any)["path"] != "/home/Downloads" {
			return &api.Error{Code: "SCOPE_DENIED", Message: "scope denied"}
		}
		creates++
		return encodeResult(out, delegate.Grant{DelegationID: "grant1", Subject: delegate.Subject{Username: "alice", UID: json.Number(current.Uid)}, Lifetime: "until-revoked", Destination: "save", Path: "/home/Downloads"})
	})
	s.DestinationDelegate = sdk
	out, _ := call(t, s, "alice", "gopeed.destination", map[string]string{"key": "save", "path": "/home/Downloads"})
	if out["success"] != true || creates != 1 {
		t.Fatal(out)
	}
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		if st.Grants["uid:alice"] != "grant1" || st.Folders["uid:alice"] != "/home/Downloads" {
			t.Fatal(st)
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ = call(t, s, "alice", "gopeed.destination", map[string]string{"key": "save", "path": "/shared"})
	if out["success"] == true || creates != 1 {
		t.Fatal("scope escape", out)
	}
	out, _ = call(t, s, "alice", "gopeed.folder", map[string]string{"folder": "/shared"})
	if out["success"] != false {
		t.Fatal("session-only destination enabled", out)
	}
	_, _ = s.withState(context.Background(), func(st *State) (any, error) {
		if st.Grants["uid:alice"] != "grant1" {
			t.Fatal("refusal changed the existing grant")
		}
		return nil, nil
	})
}

func TestInstallSelectedShareUsesCallerDestinationAPI(t *testing.T) {
	s, _, _ := fixture(t)
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-SYNO-TOKEN") != "" {
			t.Fatal("session leaked to engine")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": "resolved", "res": map[string]any{"files": []map[string]any{{"name": "file.bin", "path": "", "size": 8}}}}})
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	current, _ := user.Current()
	lookups, creates := 0, 0
	active := true
	sdk := &delegate.Service{Scope: "install-selected", Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}, Destinations: []string{"save"}, LookupUID: func(string) string { return current.Uid }}
	sdk.Client = appExecutor(func(_ context.Context, op string, auth map[string]string, p, out any) error {
		switch op {
		case "status":
			disabled, allowed := false, true
			return encodeResult(out, delegate.ExecutorStatus{ExecutorDigest: "executor-pin", PolicyDigest: "policy-pin", Version: "fixture", Ready: true, Disabled: &disabled, Allowed: &allowed, Selection: &delegate.ScopeSelection{Scope: "shared:downloads", Root: "/downloads", Generation: 4}})
		case "delegate.create":
			creates++
			return encodeResult(out, delegate.Grant{DelegationID: "shared-grant", Subject: delegate.Subject{Username: "alice", UID: json.Number(current.Uid)}, Lifetime: "until-revoked", Destination: "save", Path: "/downloads/Exports"})
		case "files.checkPermission":
			return encodeResult(out, map[string]bool{"allowed": true})
		case "delegate.list":
			lookups++
			return encodeResult(out, map[string]any{"delegations": []delegate.Grant{{DelegationID: "shared-grant", Subject: delegate.Subject{Username: "alice", UID: json.Number(current.Uid)}, Lifetime: "until-revoked", Destination: "save", Path: "/downloads/Exports", Scope: "shared:downloads", ScopeGeneration: 4, Revoked: !active}}})
		default:
			t.Fatalf("unexpected %s", op)
			return nil
		}
	})
	s.DestinationDelegate = sdk
	out, _ := call(t, s, "alice", "gopeed.destination", map[string]string{"key": "save", "path": "/downloads/Exports"})
	if out["success"] != true || creates != 1 || lookups != 1 {
		t.Fatalf("selection must use fresh Destination API: %v, %d/%d", out, creates, lookups)
	}
	body := json.RawMessage(`{"req":{"url":"https://example.com/file.bin"},"opts":{"path":"/downloads/Exports"}}`)
	out, _ = call(t, s, "alice", "test.resolve", json.RawMessage(body))
	if out["success"] != true || lookups != 3 {
		t.Fatal("resolve did not validate current grant", out, lookups)
	}
	active = false
	out, _ = call(t, s, "alice", "test.resolve", json.RawMessage(body))
	if out["success"] == true || lookups != 4 {
		t.Fatal("cached revoked grant reused or silently fell back to session", out, lookups)
	}
	out, _ = call(t, s, "alice", "test.create", json.RawMessage(`{"rid":"resolved"}`))
	if out["success"] == true || lookups != 5 {
		t.Fatal("resolved task reused revoked grant", out, lookups)
	}
}
