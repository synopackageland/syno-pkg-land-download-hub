package gopeed

import (
	"context"
	"net/http"
	"testing"
	"time"

	"dsmapp/internal/binaries"
	"github.com/synopackageland/syno-pkg-land-sdk/api"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// slowPicker stands in for the SDK picker round trip, which took about 5 s
// on the device. It reports whether jobs.lock was free.
type slowPicker struct {
	ownDestination
	s    *Service
	free chan bool
}

func (p slowPicker) SelectDestination(ctx context.Context, c api.Caller, r *http.Request, in delegate.SelectDestinationParams) (delegate.DestinationGrant, error) {
	probe, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	_, err := p.s.withState(probe, func(*State) (any, error) { return nil, nil })
	p.free <- err == nil
	return p.ownDestination.SelectDestination(ctx, c, r, in)
}

func TestDestinationPickerRunsOutsideTheJobsLock(t *testing.T) {
	s, _, _ := fixture(t)
	picker := slowPicker{s: s, free: make(chan bool, 1)}
	s.DestinationDelegate = picker
	out, _ := call(t, s, "normal1", "gopeed.destination", map[string]string{"key": "save", "path": "/home"})
	if out["success"] != true {
		t.Fatal(out)
	}
	if !<-picker.free {
		t.Fatal("the picker round trip held jobs.lock")
	}
}

// Choosing a folder never touches the engine, so it does not queue behind an
// engine update or task creation.
func TestDestinationDoesNotWaitForTheEngineLock(t *testing.T) {
	s, _, _ := fixture(t)
	s.DestinationDelegate = ownDestination{}
	unlock, err := (binaries.Manager{Data: s.Data}).Lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	started := time.Now()
	out, _ := call(t, s, "normal1", "gopeed.destination", map[string]string{"key": "save", "path": "/home"})
	if out["success"] != true || time.Since(started) > 2*time.Second {
		t.Fatal(time.Since(started), out)
	}
}

// The App must forward the authenticated caller and original request, rather
// than reconstructing a caller from a browser username or using loopback DSM.
type checkedDestination struct {
	ownDestination
	check func(context.Context, api.Caller, *http.Request, string, string) error
}

func (d checkedDestination) CheckDestination(ctx context.Context, c api.Caller, r *http.Request, key, folder string) error {
	return d.check(ctx, c, r, key, folder)
}
func TestDestinationForwardsTrustedCallerAndRefusal(t *testing.T) {
	s, _, calls := fixture(t)
	checks := 0
	s.DestinationDelegate = checkedDestination{check: func(_ context.Context, c api.Caller, r *http.Request, key, folder string) error {
		checks++
		if c.User() != "normal1" || r.Header.Get("actor") != "normal1" || key != "save" || folder != "/home" {
			t.Fatal("lost caller/request/destination binding")
		}
		return refused("DELEGATION_DENIED")
	}}
	out, _ := call(t, s, "normal1", "downloads.create_many", map[string]any{"path": "/home", "items": []map[string]string{{"key": "probe-key-0001", "url": "https://example.com/file"}}})
	if out["success"] != false || errorCode(out) != "DELEGATION_DENIED" || checks != 1 || *calls != 0 {
		t.Fatal(out, checks, *calls)
	}
}

func TestRevealPreservesPublishedHistoryAfterDestinationChange(t *testing.T) {
	s, _, _ := fixture(t)
	s.DestinationDelegate = nil // A current grant is not required to reveal history.
	_, err := s.withState(context.Background(), func(st *State) (any, error) {
		st.Jobs["history"] = Job{Owner: "uid:normal1", Published: true, Destination: "/home/Previous", Filename: "saved.pdf"}
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := call(t, s, "normal1", "gopeed.reveal", map[string]string{"id": "history"})
	if out["success"] != true {
		t.Fatal(out)
	}
	data := out["data"].(map[string]any)
	if data["folder"] != "/home/Previous" || data["filename"] != "saved.pdf" {
		t.Fatal(data)
	}
	out, _ = call(t, s, "normal2", "gopeed.reveal", map[string]string{"id": "history"})
	if out["success"] != false {
		t.Fatal("foreign history revealed", out)
	}
}
