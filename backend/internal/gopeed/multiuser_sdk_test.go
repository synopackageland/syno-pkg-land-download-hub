package gopeed

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// The real SDK checks exact username+UID even if the executor returns all grants.
// The writer is a fixture: real DSM owner/ACL acceptance remains with regression fixture.
func TestRealSDKThreeSubjectsBindOnlyTheirOwnUIDGrantAfterLogout(t *testing.T) {
	s, files, _ := fixture(t)
	uids := map[string]string{"admin": "1200", "normal1": "1201", "normal2": "1202"}
	s.Owner = func(name string) (string, error) { return uids[name] + ":" + name, nil }
	grants := []delegate.Grant{}
	for name, uid := range uids {
		grants = append(grants, delegate.Grant{DelegationID: "grant-" + name, Subject: delegate.Subject{Username: name, UID: json.Number(uid)}, Lifetime: "until-revoked", Destination: "save", Path: "/home", Scope: "home", ScopeGeneration: 1})
	}
	writes := map[string]string{}
	moves := 0
	sdk := &delegate.Service{Scope: "install-selected", Lifetime: "until-revoked", Operations: []string{"files.moveFromStaging"}, Destinations: []string{"save"}, LookupUID: func(name string) string { return uids[name] }, Store: background.NewRequestStore(filepath.Join(t.TempDir(), "background")), StagingDir: s.StagingDir}
	sdk.Client = appExecutor(func(_ context.Context, op string, auth map[string]string, p, out any) error {
		switch op {
		case "status":
			disabled, allowed := false, true
			return encodeResult(out, delegate.ExecutorStatus{ExecutorDigest: "pin", PolicyDigest: "policy", Version: "fixture", Ready: true, Disabled: &disabled, Allowed: &allowed, Selection: &delegate.ScopeSelection{Scope: "home", Root: "/home", Generation: 1}})
		case "delegate.create":
			// The caller-bound list provides real authority; selection cannot trust this
			// fixture reply unless its subject matches the caller's independently read UID.
			for _, grant := range grants {
				if auth["dsm_cookie"] == "session-"+grant.Subject.Username {
					return encodeResult(out, grant)
				}
			}
			return &api.Error{Code: "DELEGATION_DENIED"}
		case "delegate.list":
			return encodeResult(out, map[string]any{"delegations": grants})
		case "files.moveFromStaging":
			moves++
			if len(auth) != 0 {
				t.Fatal("offline save retained credentials")
			}
			in := p.(delegate.MoveParams)
			for _, grant := range grants {
				if grant.DelegationID == in.DelegationID {
					writes[in.JobID] = grant.Subject.UIDString()
				}
			}
			if writes[in.JobID] == "" {
				t.Fatal("unbound writer identity")
			}
			return encodeResult(out, delegate.OperationResult{JobID: in.JobID, State: "queued"})
		case "files.operationStatus":
			in := p.(delegate.OperationRef)
			return encodeResult(out, delegate.OperationResult{JobID: in.JobID, State: "queued"})
		default:
			t.Fatal(op)
			return nil
		}
	})
	s.DestinationDelegate = sdk
	s.Delegate = sdk
	// Destination() must ignore a same-name grant with another numeric UID.
	grants = append(grants, delegate.Grant{DelegationID: "recycled-account", Subject: delegate.Subject{Username: "normal1", UID: "9999"}, Lifetime: "until-revoked", Destination: "save", Path: "/home", Scope: "home", ScopeGeneration: 1})
	for name, uid := range uids {
		server, err := NewServer(actorAuth{}, s.Routes()...)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest("POST", "http://fixture/api.cgi", bytes.NewBufferString(`{"method":"gopeed.destination","params":{"key":"save","path":"/home"}}`))
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-SYNO-TOKEN", "current-token")
		req.Header.Set("Cookie", "id=session-"+name)
		req.Header.Set("actor", name)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, req)
		var out map[string]any
		if json.Unmarshal(response.Body.Bytes(), &out) != nil || out["success"] != true {
			t.Fatal(response.Body.String())
		}
		_, err = s.withState(context.Background(), func(st *State) (any, error) {
			if st.Grants[uid+":"+name] != "grant-"+name {
				t.Fatal("wrong UID grant", st.Grants)
			}
			st.Jobs[name] = Job{Owner: uid + ":" + name, Stage: filepath.Join(s.StagingDir, name), Filename: name + ".bin", Destination: "/home", DelegationID: "grant-" + name}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		job := filepath.Join(s.StagingDir, name)
		os.MkdirAll(job, 0700)
		os.WriteFile(filepath.Join(job, name+".bin"), []byte(name), 0600)
	}
	files.deny = true
	if err := s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	for name, uid := range uids {
		if writes[operationID(name)] != uid {
			t.Fatal(name, writes)
		}
	}
	if files.uploaded != "" {
		t.Fatal("session writer used")
	}
	restarted := New(s.Data)
	restarted.Engine = s.Engine
	restarted.StagingDir = s.StagingDir
	restarted.Delegate = sdk
	if err := restarted.SaveCompleted(context.Background()); err != nil || len(writes) != 3 || moves != 3 {
		t.Fatal(err, writes)
	}
}
