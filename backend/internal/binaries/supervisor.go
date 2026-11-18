package binaries

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// RestartPolicy bounds automatic engine restarts: after Limit exits within
// Window the supervisor gives up and reports engine_crash_loop.
type RestartPolicy struct {
	Limit   int
	Window  time.Duration
	Backoff func(attempt int) time.Duration
}

var restartPolicy = RestartPolicy{Limit: 5, Window: 10 * time.Minute, Backoff: func(attempt int) time.Duration {
	return min(2*time.Second<<(attempt-1), time.Minute)
}}

// HealthPolicy probes a running engine every Interval, each probe bounded by
// Timeout; Failures consecutive misses restart it like an exit.
type HealthPolicy struct {
	Interval time.Duration
	Timeout  time.Duration
	Failures int
}

var healthPolicy = HealthPolicy{Interval: 30 * time.Second, Timeout: 5 * time.Second, Failures: 3}

// startTimeout bounds how long a started engine may take to answer.
var startTimeout = 12 * time.Second

// Supervise keeps process ownership in the package-account daemon. Only its
// direct child is stopped. Selection requests cannot supply command arguments.
func Supervise(ctx context.Context, data string, bundled Selection, cfg, endpoint string, beforeResume ...func(context.Context) error) error {
	parsed, e := url.Parse(endpoint)
	if e != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" || parsed.Port() == "" {
		return errors.New("invalid loopback endpoint")
	}
	port := parsed.Port()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()
	st := Runtime{Active: bundled}
	if saved, e := ReadRuntime(data); e == nil {
		st = saved
		// The launcher's first starting record names no engine yet.
		if st.Active.Path != bundled.Path && st.Active.Path != "" {
			if e = Verify(data, st.Active); e != nil {
				return e
			}
		} else {
			// The DSM installer owns the fixed SPK payload. An in-place SPK
			// upgrade replaces this binary, so use the current packaged identity.
			st.Active = bundled
		}
		if st.Previous != nil && st.Previous.Path == bundled.Path {
			current := bundled
			st.Previous = &current
		}
	}
	start := func(s Selection) (*exec.Cmd, chan error, error) {
		sweepEngineTemp()
		if e := rotateEngineToken(data, cfg); e != nil {
			return nil, nil, e
		}
		cmd := exec.Command(s.Path, "-c", cfg, "-A", "127.0.0.1", "-P", port)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()
		if e := cmd.Start(); e != nil {
			return nil, nil, e
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		return cmd, done, nil
	}
	stop := func(cmd *exec.Cmd, done chan error) {
		if cmd == nil {
			return
		}
		// A stopped (SIGSTOP) engine must run again to shut down cleanly.
		cmd.Process.Signal(syscall.SIGCONT)
		cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(7 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	}
	// probe asks the engine once, within timeout, for its version.
	probe := func(version string, timeout time.Duration) bool {
		token, e := os.ReadFile(filepath.Join(data, "engine.token"))
		if e != nil {
			return false
		}
		client := &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		defer client.CloseIdleConnections()
		r, _ := http.NewRequestWithContext(ctx, "GET", endpoint+"/api/v1/info", nil)
		r.Header.Set("X-Api-Token", string(token))
		resp, e := client.Do(r)
		if e != nil {
			return false
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		var info struct {
			Code int
			Data struct{ Version string }
		}
		return resp.StatusCode == 200 && json.Unmarshal(b, &info) == nil && info.Code == 0 && info.Data.Version == version
	}
	healthy := func(version string) bool {
		until := time.Now().Add(startTimeout)
		for time.Now().Before(until) {
			if probe(version, 500*time.Millisecond) {
				return true
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(150 * time.Millisecond):
			}
		}
		return false
	}
	// The product first recovers durable ownership for accepted submissions;
	// only then may a start resume package-owned interrupted engine tasks.
	recoverTasks := func() error {
		for _, recover := range beforeResume {
			if e := recover(ctx); e != nil {
				return e
			}
		}
		return resumeInterrupted(ctx, data, endpoint)
	}
	// Errors describe this daemon's engine; a previous crash is not current.
	st.Error = ""
	recovered, e := recoverPlugin(data)
	if e != nil {
		return e
	}
	if recovered != "" {
		st.Request = recovered
		st.Error = "plugin_update_failed_previous_restored"
		if recoveredState, read := ReadRuntime(data); read == nil {
			st.Plugin = recoveredState.Plugin
		}
	}
	// Persist not-ready before starting: a failed recovery must never leave a
	// previous daemon's ready receipt visible to the UI.
	// Until the engine answers, the App says the package is starting.
	st.beginStarting(time.Now())
	if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
		return e
	}
	cmd, done, e := start(st.Active)
	if e != nil {
		st.endStarting()
		Atomic(filepath.Join(data, "runtime.json"), st)
		return e
	}
	defer func() { stop(cmd, done); sweepEngineTemp() }()
	st.Ready = healthy(st.Active.Version)
	st.endStarting()
	if !st.Ready {
		Atomic(filepath.Join(data, "runtime.json"), st)
		return errors.New("engine health failed")
	}
	if e = recoverTasks(); e != nil {
		st.Ready = false
		st.Error = "task_recovery_failed"
		Atomic(filepath.Join(data, "runtime.json"), st)
		return e
	}
	if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
		return e
	}
	// relaunch restarts a crashed engine with backoff. Restarts within the
	// policy window are bounded; then the daemon stops with engine_crash_loop.
	var exits []time.Time
	relaunch := func() error {
		for {
			now := time.Now()
			recent := exits[:0]
			for _, at := range exits {
				if now.Sub(at) < restartPolicy.Window {
					recent = append(recent, at)
				}
			}
			exits = append(recent, now)
			st.Ready = false
			st.Error = "engine_restarting"
			if len(exits) >= restartPolicy.Limit {
				st.Error = "engine_crash_loop"
			}
			if e := Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
				return e
			}
			if st.Error == "engine_crash_loop" {
				return errors.New("engine keeps exiting")
			}
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(restartPolicy.Backoff(len(exits))):
			}
			var e error
			if cmd, done, e = start(st.Active); e == nil {
				if healthy(st.Active.Version) && recoverTasks() == nil {
					st.Ready, st.Error = true, ""
					return Atomic(filepath.Join(data, "runtime.json"), st)
				}
				stop(cmd, done)
			}
			cmd = nil
			if ctx.Err() != nil {
				return nil
			}
		}
	}
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	// A running engine that stops answering is not ready; after
	// healthPolicy.Failures misses in a row it is restarted like an exit.
	health := time.NewTicker(healthPolicy.Interval)
	defer health.Stop()
	misses, kept := 0, ""
	for {
		select {
		case <-health.C:
			if probe(st.Active.Version, healthPolicy.Timeout) {
				if misses > 0 && st.Error == "engine_unavailable" {
					st.Ready, st.Error = true, kept
					if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
						return e
					}
				}
				misses = 0
				continue
			}
			if misses++; misses == 1 {
				kept = st.Error
				st.Ready, st.Error = false, "engine_unavailable"
				if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
					return e
				}
			}
			if misses < healthPolicy.Failures {
				continue
			}
			misses = 0
			fmt.Fprintln(os.Stderr, "engine unresponsive; restarting")
			stop(cmd, done)
			cmd = nil
			if e = relaunch(); e != nil {
				return e
			}
		case <-ctx.Done():
			st.Ready = false
			Atomic(filepath.Join(data, "runtime.json"), st)
			return nil
		case e := <-done:
			cmd = nil
			fmt.Fprintln(os.Stderr, "engine exited:", e)
			if e = relaunch(); e != nil {
				return e
			}
		case <-tick.C:
			var req Request
			b, e := os.ReadFile(filepath.Join(data, "engine-request.json"))
			if e != nil || len(b) > 16384 || json.Unmarshal(b, &req) != nil || req.ID == "" || req.ID == st.Request {
				continue
			}
			if req.Plugin != nil {
				st.Ready = false
				if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
					return e
				}
				halt := func() { stop(cmd, done); cmd = nil }
				boot := func() error {
					var err error
					cmd, done, err = start(st.Active)
					if err != nil {
						return err
					}
					if !healthy(st.Active.Version) {
						return errors.New("engine health failed")
					}
					return nil
				}
				err := pluginTransaction(ctx, data, endpoint, req.ID, *req.Plugin, halt, boot)
				st.Request = req.ID
				st.Error = ""
				st.Ready = cmd != nil && healthy(st.Active.Version)
				if err != nil {
					st.Error = err.Error()
				}
				st.Plugin = &PluginResult{ID: req.Plugin.ID, Version: req.Plugin.Version, Error: st.Error}
				if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
					return e
				}
				if !st.Ready {
					return errors.New("plugin recovery left engine unavailable")
				}
				continue
			}
			// Bundled rollback is accepted only at the fixed packaged path and digest.
			e = Verify(data, req.Selection)
			if req.Selection == bundled {
				e = nil
			}
			if e != nil {
				st.Request = req.ID
				st.Error = "selection_verification_failed"
				Atomic(filepath.Join(data, "runtime.json"), st)
				continue
			}
			old := st.Active
			st.Ready = false
			if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
				return e
			}
			stop(cmd, done)
			cmd = nil
			cmd, done, e = start(req.Selection)
			if e == nil && healthy(req.Selection.Version) {
				st.Active = req.Selection
				st.Previous = &old
				st.Request = req.ID
				st.Error = ""
				st.Ready = true
			} else {
				if cmd != nil {
					stop(cmd, done)
					cmd = nil
				}
				cmd, done, e = start(old)
				st.Active = old
				st.Request = req.ID
				st.Error = "candidate_failed_previous_restored"
				st.Ready = e == nil && healthy(old.Version)
				if !st.Ready {
					st.Error = "candidate_recovery_failed"
					Atomic(filepath.Join(data, "runtime.json"), st)
					return errors.New(st.Error)
				}
			}
			if e = Atomic(filepath.Join(data, "runtime.json"), st); e != nil {
				return e
			}
		}
	}
}
