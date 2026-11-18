package gopeed

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"dsmapp/internal/binaries"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// An unreachable engine is explained by the supervisor's runtime error, so
// every user, not only administrators, sees the crash loop.
func TestUnreachableEngineReportsTheSupervisorError(t *testing.T) {
	for _, tc := range []struct{ runtime, code string }{
		{"engine_crash_loop", "engine_crash_loop"},
		{"engine_restarting", "engine_restarting"},
		{"", "engine_unavailable"},
		{"selection_verification_failed", "engine_unavailable"},
	} {
		s, _, _ := fixture(t)
		s.Engine = "http://127.0.0.1:1"
		if e := binaries.Atomic(filepath.Join(s.Data, "runtime.json"), binaries.Runtime{Error: tc.runtime}); e != nil {
			t.Fatal(e)
		}
		out, _ := call(t, s, "normal1", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
		b, _ := json.Marshal(out)
		if out["success"] != false || !strings.Contains(string(b), `"`+tc.code+`"`) {
			t.Fatal(tc.runtime, string(b))
		}
	}
}

// while the package is stopped the engine cannot answer either;
// the SDK's executor client names the package's lifecycle from its mailbox,
// so the page says the App is stopped instead of the engine not responding.
func TestUnreachableEngineOfAStoppedPackageReportsItsLifecycle(t *testing.T) {
	for _, tc := range []struct{ request, code string }{
		{"stop", "APP_STOPPED"},
		{"", "engine_unavailable"},
	} {
		s, _, _ := fixture(t)
		s.Engine = "http://127.0.0.1:1"
		mailbox := t.TempDir()
		if tc.request != "" {
			if e := os.WriteFile(filepath.Join(mailbox, "control"), []byte(tc.request), 0600); e != nil {
				t.Fatal(e)
			}
		}
		s.Executor = &delegate.Client{Socket: filepath.Join(t.TempDir(), "executor.sock"), Timeout: time.Second, Mailbox: mailbox}
		out, _ := call(t, s, "normal1", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
		b, _ := json.Marshal(out)
		if out["success"] != false || !strings.Contains(string(b), `"`+tc.code+`"`) {
			t.Fatal(tc.request, string(b))
		}
	}
}

// after the package has started, its engine takes a moment to
// answer. The supervisor records that it is starting; the page then says
// the App is starting, not that the engine does not respond, and so does
// a list request meanwhile. A new link is admitted without waiting for it.
func TestUnreachableEngineThatIsStartingReportsAppStarting(t *testing.T) {
	s, _ := newEngineFixture(t)
	s.Engine = "http://127.0.0.1:1"
	if e := binaries.MarkStarting(s.Data); e != nil {
		t.Fatal(e)
	}
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	if b, _ := json.Marshal(out); out["success"] != false || !strings.Contains(string(b), `"`+delegate.CodeAppStarting+`"`) {
		t.Fatal(string(b))
	}
	if r := createMany(t, s, "alice", item(1, "https://example.com/a.bin"))[0]; r["status"] != "created" {
		t.Fatal(r)
	}
}

// A daemon killed while starting (kill -9) left starting:true behind; long
// after, the engine that does not answer is unavailable, not starting.
func TestUnreachableEngineOfALeftStartingRecordIsUnavailable(t *testing.T) {
	s, _ := newEngineFixture(t)
	s.Engine = "http://127.0.0.1:1"
	left := binaries.Runtime{Starting: true, StartingSince: time.Now().Add(-time.Hour).UnixMilli()}
	if e := binaries.Atomic(filepath.Join(s.Data, "runtime.json"), left); e != nil {
		t.Fatal(e)
	}
	out, _ := call(t, s, "alice", "gopeed.request", Input{Method: "GET", Path: "/api/v1/tasks"})
	if b, _ := json.Marshal(out); out["success"] != false || !strings.Contains(string(b), `"engine_unavailable"`) {
		t.Fatal(string(b))
	}
}
