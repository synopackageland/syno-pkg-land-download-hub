package gopeed

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

type ownDestination struct{}

func (ownDestination) CheckDestination(_ context.Context, c api.Caller, r *http.Request, key, folder string) error {
	if c.User() == "" || r == nil || key != "save" || !validFolder(folder) {
		return refused("denied")
	}
	return nil
}

func (ownDestination) SelectDestination(_ context.Context, c api.Caller, _ *http.Request, p delegate.SelectDestinationParams) (delegate.DestinationGrant, error) {
	return delegate.DestinationGrant{ID: "grant-" + c.User(), Key: p.Key, Path: p.Path, Username: c.User(), Lifetime: "until-revoked"}, nil
}
func (ownDestination) Destination(_ context.Context, c api.Caller, _ *http.Request, key string) (delegate.DestinationState, error) {
	return delegate.DestinationState{Status: "active", Selection: &delegate.ScopeSelection{Scope: "home", Root: "/home", Generation: 1}, Destination: &delegate.DestinationGrant{ID: "grant-" + c.User(), Key: key, Path: "/home", Username: c.User(), Lifetime: "until-revoked"}}, nil
}

func TestLegacyGrantAdoptionOnlyOwnMatchingSafeUnfinishedJobs(t *testing.T) {
	s, files, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		for id, j := range map[string]Job{
			"mine":           {Owner: "uid:admin", Destination: "/home", Filename: "file.bin", Stage: filepath.Join(s.Data, "staging", "old")},
			"history":        {Owner: "uid:admin", Destination: "/home", Published: true},
			"unknown":        {Owner: "uid:admin", Destination: "/home", Uncertain: true},
			"another-folder": {Owner: "uid:admin", Destination: "/home/Other"},
			"foreign":        {Owner: "uid:normal1", Destination: "/home"},
		} {
			st.Jobs[id] = j
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	job := readJob(t, s)
	os.MkdirAll(job.Stage, 0700)
	os.WriteFile(filepath.Join(job.Stage, job.Filename), []byte("legacy"), 0600)
	out, _ := call(t, s, "admin", "gopeed.destination", map[string]string{"key": "save", "path": "/home"})
	if out["success"] != true {
		t.Fatal(out)
	}
	_, err = s.withState(context.Background(), func(st *State) (any, error) {
		for id, j := range st.Jobs {
			want := ""
			if id == "mine" {
				want = "grant-admin"
			}
			if j.DelegationID != want {
				t.Fatal(id, j)
			}
		}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	save := &fakeSave{state: "queued"}
	s.Delegate = save
	completionEngine(t, s, completionTask(6), completionStats(6))
	if err = s.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if save.calls != 1 || save.input.DelegationID != "grant-admin" || save.input.Size != 6 || files.uploaded != "" {
		t.Fatal(save.input, save.calls)
	}
	moved := readJob(t, s)
	if filepath.Dir(moved.Stage) != s.StagingDir || moved.Stage == job.Stage {
		t.Fatal("legacy bytes not moved into SDK staging", moved)
	}
	raw, err := os.ReadFile(filepath.Join(s.StagingDir, save.input.Staging))
	if err != nil || string(raw) != "legacy" {
		t.Fatal(string(raw), err)
	}
	restarted := New(s.Data)
	restarted.StagingDir = s.StagingDir
	restarted.Engine = s.Engine
	restarted.Delegate = save
	if err = restarted.SaveCompleted(context.Background()); err != nil || save.calls != 2 {
		t.Fatal("legacy replay", err, save.calls)
	}
}

func TestLegacySymlinkAndUncertainJobsNeverEnterSDK(t *testing.T) {
	for _, kind := range []string{"symlink-file", "symlink-directory", "uncertain"} {
		t.Run(kind, func(t *testing.T) {
			s, _, _ := fixture(t)
			s.DestinationDelegate = ownDestination{}
			seed(t, s)
			job := readJob(t, s)
			os.MkdirAll(job.Stage, 0700)
			outside := filepath.Join(s.Data, "outside")
			os.WriteFile(outside, []byte("secret"), 0600)
			if kind == "symlink-directory" {
				os.Remove(job.Stage)
				os.Symlink(s.Data, job.Stage)
			} else if kind == "symlink-file" {
				os.Symlink(outside, filepath.Join(job.Stage, job.Filename))
			} else {
				os.WriteFile(filepath.Join(job.Stage, job.Filename), []byte("legacy"), 0600)
			}
			_, _ = s.withState(context.Background(), func(st *State) (any, error) {
				j := st.Jobs["mine"]
				j.DelegationID = "grant-alice"
				j.Uncertain = kind == "uncertain"
				st.Jobs["mine"] = j
				return nil, nil
			})
			save := &fakeSave{state: "queued"}
			s.Delegate = save
			if err := s.SaveCompleted(context.Background()); err == nil || save.calls != 0 {
				t.Fatal("unsafe legacy save admitted", kind, err, save.calls)
			}
		})
	}
}

func TestLegacyUnboundResolvedRequestCannotBecomeNewDownload(t *testing.T) {
	s, _, calls := fixture(t)
	s.DestinationDelegate = ownDestination{}
	_, _ = s.withState(context.Background(), func(st *State) (any, error) {
		st.Resolved["old-rid"] = Job{Owner: "uid:normal1", Destination: "/home"}
		return nil, nil
	})
	out, _ := call(t, s, "normal1", "test.create", json.RawMessage(`{"rid":"old-rid"}`))
	if out["success"] != false || *calls != 0 {
		t.Fatal(out, *calls)
	}
}

func TestLegacyStageRenameCrashRecoversWithoutReplay(t *testing.T) {
	s, _, _ := fixture(t)
	seed(t, s)
	job := readJob(t, s)
	job.DelegationID = "grant-alice"
	os.MkdirAll(job.Stage, 0700)
	os.WriteFile(filepath.Join(job.Stage, job.Filename), []byte("legacy"), 0600)
	_, err := s.withState(context.Background(), func(st *State) (any, error) { st.Jobs["mine"] = job; return nil, nil })
	if err != nil {
		t.Fatal(err)
	}
	// Crash after the durable directory rename, before jobs.json is replaced.
	if err = s.relocateLegacyStage("mine", &job); err != nil {
		t.Fatal(err)
	}
	// The unchanged official engine retains its own completion counters even
	// when the adapter crashed between a private directory rename and commit.
	completionEngine(t, s, completionTask(6), completionStats(6))
	save := &fakeSave{state: "queued"}
	restarted := New(s.Data)
	restarted.Engine, restarted.Delegate = s.Engine, save
	if err = restarted.SaveCompleted(context.Background()); err != nil {
		t.Fatal(err)
	}
	if readJob(t, restarted).Stage != job.Stage || save.calls != 1 {
		t.Fatal("lost renamed staging", readJob(t, restarted))
	}
	if err = restarted.SaveCompleted(context.Background()); err != nil || save.calls != 2 {
		t.Fatal("crash recovery replayed", err)
	}
}
