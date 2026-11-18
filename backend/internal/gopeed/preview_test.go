package gopeed

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	appapi "dsmapp/internal/api"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
)

func TestRegistryPreviewsNeverRunMutationOrExposeInputs(t *testing.T) {
	s, _, calls := fixture(t)
	s.withState(context.Background(), func(st *State) (any, error) { st.Jobs["mine"] = Job{Owner: "uid:alice"}; return nil, nil })
	statePath := filepath.Join(s.Data, "jobs.json")
	before, _ := os.ReadFile(statePath)
	var registry struct {
		Operations map[string]struct {
			AI, Permission string
			Examples       []map[string]any
		}
	}
	if err := json.Unmarshal(appapi.Operations(), &registry); err != nil {
		t.Fatal(err)
	}
	count := 0
	ordinaryServer, err := testServer(s, adminAuth{})
	if err != nil {
		t.Fatal(err)
	}
	for name, op := range registry.Operations {
		if op.AI != "preview" {
			continue
		}
		count++
		user := "alice"
		if op.Permission == "administrators" {
			user = "admin"
		}
		out, _ := call(t, s, user, name, op.Examples[0])
		if out["success"] != true {
			t.Fatalf("%s: %v", name, out)
		}
		data := out["data"].(map[string]any)
		if data["confirmationRequired"] != true || data["checks"] != "execution_required" || len(data) != 4 {
			t.Fatalf("%s: %v", name, out)
		}
		if op.Permission == "administrators" {
			body, _ := json.Marshal(map[string]any{"method": name, "params": op.Examples[0]})
			req := httptest.NewRequest("POST", "http://fixture/api.cgi", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			req.Header.Set("actor", "alice")
			response := httptest.NewRecorder()
			ordinaryServer.ServeHTTP(response, req)
			var denied map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &denied); err != nil {
				t.Fatal(err)
			}
			if denied["success"] != false {
				t.Fatalf("admin preview admitted ordinary user: %s", name)
			}
		}
	}
	if count < 15 {
		t.Fatal("missing write previews", count)
	}
	after, _ := os.ReadFile(statePath)
	if !reflect.DeepEqual(before, after) || *calls != 0 {
		t.Fatal("preview changed state or called engine")
	}
}

func TestRegistryRefusesMissingHiddenRouteBeforeServing(t *testing.T) {
	s, _, _ := fixture(t)
	permissions, err := appapi.Permissions()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = api.NewServerWithOperations(actorAuth{}, appapi.Operations(), permissions, s.Routes()[0]); err == nil {
		t.Fatal("missing handlers accepted")
	}
}
