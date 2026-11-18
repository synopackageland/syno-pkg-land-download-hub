package gopeed

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func httpResolveFixture(t *testing.T) *Service {
	t.Helper()
	s, _, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	s.checkURL = func(string) bool { return true }
	n := 0
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/resolve" {
			fmt.Fprint(w, `{"code":0,"data":null}`)
			return
		}
		var in struct{ Req struct{ URL string } }
		json.NewDecoder(r.Body).Decode(&in)
		if strings.Contains(in.Req.URL, "unsupported") {
			fmt.Fprint(w, `{"code":1}`)
			return
		}
		n++
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": fmt.Sprintf("rid-%d", n), "res": map[string]any{"hash": "", "files": []any{map[string]any{"name": "file.bin", "path": "", "size": 8}}}}})
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	return s
}

func TestANewResolveDiscardsTheOwnersEarlierPreviewAndItsStage(t *testing.T) {
	s := httpResolveFixture(t)
	btResolve(t, s, "admin", "https://example.com/a.bin")
	first := state(t, s).Resolved["rid-1"].Stage
	if out := btResolve(t, s, "admin", "https://example.com/b.bin"); out["success"] != true {
		t.Fatal(out)
	}
	st := state(t, s)
	if _, ok := st.Resolved["rid-1"]; ok || len(st.Resolved) != 1 {
		t.Fatalf("%+v", st.Resolved)
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("discarded preview kept its stage", err)
	}
	if _, err := os.Stat(st.Resolved["rid-2"].Stage); err != nil {
		t.Fatal(err)
	}
}

func TestARefusedResolveLeavesNoStage(t *testing.T) {
	s := httpResolveFixture(t)
	if out := btResolve(t, s, "admin", "https://example.com/unsupported"); out["success"] != false {
		t.Fatal(out)
	}
	if entries, _ := os.ReadDir(s.StagingDir); len(entries) != 0 {
		t.Fatal("refused resolve left a stage", entries)
	}
}

func TestAPendingMagnetResolveLeavesNoEmptyStage(t *testing.T) {
	s, f := newBTFixture(t)
	s.metadataWait = 50 * time.Millisecond
	f.stall = make(chan struct{})
	defer close(f.stall)
	btResolve(t, s, "admin", "magnet:?xt=urn:btih:"+f.hash)
	if _, err := os.Stat(filepath.Join(s.StagingDir, "bt-"+f.hash)); !os.IsNotExist(err) {
		t.Fatal("empty hash stage left behind", err)
	}
}

func TestADeletedTaskLeavesNoEmptyStage(t *testing.T) {
	s, f, _ := deletionFixture(t)
	job := readJob(t, s)
	if out := deleteOwned(t, s); out["success"] != true {
		t.Fatal(out)
	}
	if err := os.Remove(filepath.Join(job.Stage, job.Filename)); err != nil {
		t.Fatal(err)
	}
	f.state, f.code = "canceled", "OPERATION_CANCELED"
	r := restartDeletion(s)
	if err := r.SaveCompleted(context.Background()); err != nil || existsJob(t, r) {
		t.Fatal("not deleted", err)
	}
	if _, err := os.Stat(job.Stage); !os.IsNotExist(err) {
		t.Fatal("empty stage left behind", err)
	}
}

func TestStartupSweepRemovesOnlyOldEmptyStagesNoTaskUses(t *testing.T) {
	s, _, _ := fixture(t)
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	old := time.Now().Add(-2 * time.Hour)
	dir := func(name string, mtime time.Time, file string) string {
		p := filepath.Join(s.StagingDir, name)
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		if file != "" {
			os.WriteFile(filepath.Join(p, file), []byte("x"), 0600)
		}
		os.Chtimes(p, mtime, mtime)
		return p
	}
	orphan := dir("orphan", old, "")
	partial := dir("partial", old, "file.bin")
	fresh := dir("fresh", time.Now(), "")
	owned := dir("owned", old, "")
	previewed := dir("previewed", old, "")
	os.WriteFile(filepath.Join(s.StagingDir, "download-1"), nil, 0600)
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["mine"] = Job{Owner: "uid:alice", Stage: owned}
		st.Resolved["rid"] = Job{Owner: "uid:alice", Stage: previewed}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SweepStaging(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan kept", err)
	}
	for _, p := range []string{partial, fresh, owned, previewed, filepath.Join(s.StagingDir, "download-1")} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(p, err)
		}
	}
}

// a stage a removed download left before deletions dropped their
// stage keeps its file; startup removes it once no download refers to it.
func TestStartupSweepRemovesOldDownloadStagesNoTaskUsesWithTheirFiles(t *testing.T) {
	s, _, _ := fixture(t)
	s.StagingDir = filepath.Join(t.TempDir(), "executor-staging")
	old := time.Now().Add(-2 * time.Hour)
	stage := func(name string, mtime time.Time) string {
		p := filepath.Join(s.StagingDir, name)
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "100Mb.dat"), []byte("x"), 0600); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, mtime, mtime)
		return p
	}
	orphan := stage("57cb8094d23b22b0ec1ef6c4f6036e12", old)
	owned := stage("0123456789abcdef0123456789abcdef", old)
	previewed := stage("fedcba9876543210fedcba9876543210", old)
	fresh := stage("00000000000000000000000000000000", time.Now())
	other := []string{stage("bt-0123456789abcdef0123456789abcdef01234567", old), stage("legacy-ab", old), stage("ABCDEF0123456789ABCDEF0123456789", old), stage("57cb8094d23b22b0ec1ef6c4f6036e1", old)}
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["mine"] = Job{Owner: "uid:alice", Stage: owned}
		st.Resolved["rid"] = Job{Owner: "uid:alice", Stage: previewed}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SweepStaging(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Fatal("orphan download stage kept", err)
	}
	for _, p := range append([]string{owned, previewed, fresh}, other...) {
		if _, err := os.Stat(filepath.Join(p, "100Mb.dat")); err != nil {
			t.Fatal(p, err)
		}
	}
}
