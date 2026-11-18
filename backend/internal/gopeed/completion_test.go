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
	"sync/atomic"
	"testing"
	"time"
)

func completionTask(size int64) map[string]any {
	return map[string]any{"status": "done", "protocol": "http", "meta": map[string]any{"res": map[string]any{"size": size}}, "progress": map[string]any{"downloaded": size}}
}

func completionStats(size int64) map[string]any {
	return map[string]any{"connections": []any{map[string]any{"downloaded": size, "completed": true, "failed": false}}}
}

func completionEngine(t *testing.T, s *Service, task, stats any) {
	t.Helper()
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-SYNO-TOKEN") != "" {
			t.Error("completion must not use DSM credentials")
		}
		data := task
		if strings.HasSuffix(r.URL.Path, "/stats") {
			data = stats
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
}

func TestIncompleteEngineDoneNeverReachesDelegate(t *testing.T) {
	cases := []struct {
		name  string
		task  any
		stats any
	}{
		{"timeout preallocated zero bytes", completionTask(8), map[string]any{"connections": []any{map[string]any{"downloaded": 0, "completed": false, "failed": false}}}},
		{"partial write", completionTask(8), completionStats(3)},
		{"incomplete connection despite full count", completionTask(8), map[string]any{"connections": []any{map[string]any{"downloaded": 8, "completed": false, "failed": false}}}},
		{"failed connection despite full count", completionTask(8), map[string]any{"connections": []any{map[string]any{"downloaded": 8, "completed": true, "failed": true}}}},
		{"missing connection completion", completionTask(8), map[string]any{"connections": []any{map[string]any{"downloaded": 8, "failed": false}}}},
		{"missing failed field", completionTask(8), map[string]any{"connections": []any{map[string]any{"downloaded": 8, "completed": true}}}},
		{"negative counter", completionTask(8), completionStats(-1)},
		{"excess counter", completionTask(8), completionStats(9)},
		{"no write evidence", completionTask(8), map[string]any{"connections": []any{}}},
		{"no connection without range", map[string]any{"status": "done", "protocol": "http", "meta": map[string]any{"res": map[string]any{"size": 8, "range": false}}, "progress": map[string]any{"downloaded": 8}}, map[string]any{"connections": []any{}}},
		{"malformed stats", completionTask(8), map[string]any{"connections": "invalid"}},
		{"missing stats", completionTask(8), nil},
		{"wrong expected size", completionTask(9), completionStats(8)},
		{"missing resource size", map[string]any{"status": "done"}, completionStats(8)},
		{"invalid resource size", completionTask(-1), completionStats(8)},
		{"engine error", map[string]any{"status": "error", "errMsg": "sensitive upstream URL"}, completionStats(8)},
		{"downloaded disagrees with actual size", map[string]any{"status": "done", "protocol": "http", "meta": map[string]any{"res": map[string]any{"size": 8}}, "progress": map[string]any{"downloaded": 7}}, completionStats(8)},
	}
	for _, tc := range cases {
		for _, via := range []string{"daemon", "owner API"} {
			t.Run(tc.name+"/"+via, func(t *testing.T) {
				s, saver := stagedDelegate(t)
				completionEngine(t, s, tc.task, tc.stats)
				if via == "daemon" {
					if err := s.SaveCompleted(context.Background()); err == nil {
						t.Fatal("incomplete engine completion accepted")
					}
				} else {
					out, _ := call(t, s, "alice", "gopeed.publish", map[string]string{"id": "mine"})
					if out["success"] != false || out["error"].(map[string]any)["code"] != "download_incomplete" {
						t.Fatal("incomplete engine completion accepted", out)
					}
				}
				job := readJob(t, s)
				if saver.calls != 0 || saver.queries != 0 || job.SaveStarted || job.Published || job.Uncertain || job.SaveJobID != "" || job.SaveError != "download_incomplete" {
					t.Fatal("failed download reached publication or lacked durable error", job, saver.calls, saver.queries)
				}
				encoded, _ := json.Marshal(job)
				if strings.Contains(string(encoded), "sensitive upstream") {
					t.Fatal("upstream diagnostics leaked")
				}
				restarted := New(s.Data)
				restarted.Engine, restarted.StagingDir, restarted.Delegate = s.Engine, s.StagingDir, saver
				_ = restarted.SaveCompleted(context.Background())
				if saver.calls != 0 || readJob(t, restarted).SaveError != "download_incomplete" {
					t.Fatal("restart published failed content")
				}
			})
		}
	}
}

// Gopeed 1.9.3 writes a range resource that the resolve request prefetched in
// full without opening a connection, so its stats list none.
func TestFullyPrefetchedRangeDownloadIsComplete(t *testing.T) {
	s, saver := stagedDelegate(t)
	task := completionTask(8)
	task["meta"].(map[string]any)["res"].(map[string]any)["range"] = true
	completionEngine(t, s, task, map[string]any{"connections": []any{}})
	if err := s.SaveCompleted(context.Background()); err != nil || saver.calls != 1 {
		t.Fatal("prefetched range download rejected", err, saver.calls)
	}
}

func TestVerifiedZeroContentIsNotMistakenForIncompleteDownload(t *testing.T) {
	s, saver := stagedDelegate(t)
	job := readJob(t, s)
	if err := os.WriteFile(filepath.Join(job.Stage, job.Filename), make([]byte, 8), 0600); err != nil {
		t.Fatal(err)
	}
	completionEngine(t, s, completionTask(8), completionStats(8))
	if err := s.SaveCompleted(context.Background()); err != nil || saver.calls != 1 {
		t.Fatal("legitimate all-zero source rejected", err, saver.calls)
	}
}

func TestRunningSourceWaitsWithoutStartingPublication(t *testing.T) {
	s, saver := stagedDelegate(t)
	completionEngine(t, s, map[string]any{"status": "running"}, completionStats(0))
	if err := s.SaveCompleted(context.Background()); err != nil || saver.calls != 0 || readJob(t, s).SaveError != "" {
		t.Fatal("active source must keep waiting", err, saver.calls)
	}
}

func TestVerifiedConnectionTotalsAndEmptyBody(t *testing.T) {
	for _, size := range []int64{0, 8} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			s, saver := stagedDelegate(t)
			job := readJob(t, s)
			if err := os.Truncate(filepath.Join(job.Stage, job.Filename), size); err != nil {
				t.Fatal(err)
			}
			stats := map[string]any{"connections": []any{
				map[string]any{"downloaded": size / 2, "completed": true, "failed": false},
				map[string]any{"downloaded": size - size/2, "completed": true, "failed": false},
			}}
			completionEngine(t, s, completionTask(size), stats)
			if err := s.SaveCompleted(context.Background()); err != nil || saver.calls != 1 || saver.input.Size != size {
				t.Fatal("valid written totals rejected", err, saver.calls)
			}
		})
	}
}

func TestStatsUnavailableWaitsThenVerifiesBeforeSingleMove(t *testing.T) {
	s, saver := stagedDelegate(t)
	var ready atomic.Bool
	engine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data := any(completionTask(8))
		if strings.HasSuffix(r.URL.Path, "/stats") {
			if !ready.Load() {
				fmt.Fprint(w, `{"code":1000,"data":null}`)
				return
			}
			data = completionStats(8)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	t.Cleanup(engine.Close)
	s.Engine = engine.URL
	if err := s.SaveCompleted(context.Background()); err == nil || saver.calls != 0 || readJob(t, s).SaveError != "" || readJob(t, s).SaveStarted {
		t.Fatal("unverified completion admitted or permanently failed", err, saver.calls)
	}
	ready.Store(true)
	if err := s.SaveCompleted(context.Background()); err != nil || saver.calls != 1 {
		t.Fatal("recovered evidence must admit once", err, saver.calls)
	}
	ready.Store(false) // SDK receives the same tuple without re-verifying the engine.
	if err := s.SaveCompleted(context.Background()); err != nil || saver.calls != 2 {
		t.Fatal("already admitted work replayed", err, saver.calls)
	}
}

func TestOfficialEngineDelayedBodyCannotPublishPreallocatedFile(t *testing.T) {
	s := officialEngine(t)
	payload := strings.Repeat("nonzero-body", 3498) + "tailtail" // 41984 bytes
	if len(payload) != 41984 {
		t.Fatal(len(payload))
	}
	stop := make(chan struct{})
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		w.(http.Flusher).Flush()
		select {
		case <-stop:
			return
		case <-r.Context().Done():
			return
		case <-time.After(45 * time.Second):
			_, _ = fmt.Fprint(w, payload)
		}
	}))
	t.Cleanup(func() { close(stop); source.Close() })
	stage := filepath.Join(s.StagingDir, "delayed")
	if err := os.MkdirAll(stage, 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	reply, err := s.engine(ctx, "POST", "/api/v1/tasks", nil, map[string]any{"req": map[string]any{"url": source.URL + "/slow.bin"}, "opts": map[string]any{"path": stage, "name": "slow.bin"}})
	var id string
	if err != nil || reply.Code != 0 || json.Unmarshal(reply.Data, &id) != nil || id == "" {
		t.Fatal(reply, err)
	}
	_, err = s.withState(ctx, func(st *State) (any, error) {
		st.Jobs[id] = Job{Owner: "uid:normal1", Stage: stage, Destination: "/home", Filename: "slow.bin", DelegationID: "own-grant"}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	saver := &fakeSave{state: "queued"}
	s.Delegate = saver
	for ctx.Err() == nil {
		reply, err = s.engine(ctx, "GET", "/api/v1/tasks/"+id, nil, nil)
		var task struct {
			Status string `json:"status"`
		}
		if err != nil || json.Unmarshal(reply.Data, &task) != nil {
			t.Fatal(reply, err)
		}
		if task.Status == "done" || task.Status == "error" {
			stats, _ := s.engine(ctx, "GET", "/api/v1/tasks/"+id+"/stats", nil, nil)
			t.Logf("official terminal task=%s stats=%s", reply.Data, stats.Data)
			if err = s.SaveCompleted(ctx); err == nil || saver.calls != 0 {
				b, _ := os.ReadFile(filepath.Join(stage, "slow.bin"))
				t.Fatalf("premature content published: moves=%d actual=%d nonzero=%t error=%v", saver.calls, len(b), strings.Trim(string(b), "\x00") != "", err)
			}
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("engine did not reach its bounded read timeout", ctx.Err())
}
