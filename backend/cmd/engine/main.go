// Package-account launcher: provisions private config, then execs the original
// official Gopeed executable, preserving the CLI supervisor's process identity.
package main

import (
	"context"
	"dsmapp/internal/binaries"
	"dsmapp/internal/githubtoken"
	"dsmapp/internal/gopeed"
	"dsmapp/internal/media"
	"encoding/json"
	"fmt"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, "Gopeed launcher:", e)
		os.Exit(1)
	}
}
func run() error {
	if os.Geteuid() == 0 {
		return fmt.Errorf("refusing root execution")
	}
	if len(os.Args) != 3 {
		return fmt.Errorf("need payload and package var paths")
	}
	payload, data := os.Args[1], filepath.Join(os.Args[2], "gopeed")
	if canonical, e := filepath.EvalSymlinks(data); e == nil {
		data = canonical
	}
	if !filepath.IsAbs(payload) || !filepath.IsAbs(data) {
		return fmt.Errorf("paths must be absolute")
	}
	if e := os.MkdirAll(filepath.Join(data, "staging"), 0700); e != nil {
		return e
	}
	if e := os.Chmod(data, 0700); e != nil {
		return e
	}
	// DSM reports the package running before the checks below end; until the
	// engine answers, the App says it is starting.
	if e := binaries.MarkStarting(data); e != nil {
		return e
	}
	defer binaries.EndStarting(data)
	// The supervisor injects a fresh private token before each child start.
	config := map[string]any{"address": "127.0.0.1", "port": 19893, "storageDir": filepath.Join(data, "storage"), "whiteDownloadDirs": []string{filepath.Join(data, "staging", "*"), filepath.Join(filepath.Dir(data), "executor-staging", "*")}, "downloadConfig": map[string]any{"downloadDir": filepath.Join(data, "staging"), "extra": map[string]any{"autoStartTasks": false, "notifyWhenNewVersion": false}, "autoTorrent": map[string]any{"enable": false}, "archive": map[string]any{"autoExtract": false}}}
	prefs, e := gopeed.LoadSettings(data)
	if e != nil {
		return fmt.Errorf("invalid saved settings")
	}
	dc := config["downloadConfig"].(map[string]any)
	dc["maxRunning"] = prefs.MaxRunning
	dc["protocolConfig"] = gopeed.EngineProtocolConfig(prefs)
	cfg := filepath.Join(data, "engine.json")
	if e = binaries.Atomic(cfg, config); e != nil {
		return e
	}
	exe := filepath.Join(payload, "bin", "gopeed")
	env := []string{}
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "GOPEED_") {
			env = append(env, v)
		}
	}
	os.Clearenv()
	for _, v := range env {
		k, val, ok := strings.Cut(v, "=")
		if ok {
			os.Setenv(k, val)
		}
	}
	hash, e := binaries.Hash(exe)
	if e != nil {
		return e
	}
	var upstream struct{ Version string }
	raw, e := os.ReadFile(filepath.Join(payload, "bin/upstream.json"))
	if e != nil || json.Unmarshal(raw, &upstream) != nil {
		return fmt.Errorf("missing upstream metadata")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service := gopeed.New(data)
	service.Delegate = &delegate.Service{Client: delegate.Local("SynoPkgGopeed"), Store: background.NewRequestStore(filepath.Join(filepath.Dir(data), "background")), StagingDir: service.StagingDir, Operations: []string{"files.moveFromStaging"}, Lifetime: "until-revoked"}
	runner := &gopeed.MediaRunner{Service: service, Start: gopeed.StartMedia}
	preparer := &gopeed.PreparationRunner{Service: service}
	tools := media.Tools{Home: filepath.Join(data, "media"), GitHub: githubtoken.At(data)}
	_ = service.SweepStaging(ctx, time.Now())
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Tool downloads can take minutes; they run apart from saves and downloads.
		toolsIdle := map[string]chan struct{}{}
		for tool := range media.Specs {
			toolsIdle[tool] = make(chan struct{}, 1)
			toolsIdle[tool] <- struct{}{}
		}
		savesIdle := make(chan struct{}, 1)
		savesIdle <- struct{}{}
		tick := time.NewTicker(2 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				preparer.Close()
				runner.Close()
				<-savesIdle
				for _, idle := range toolsIdle {
					<-idle
				}
				return
			case <-tick.C:
				for tool, idle := range toolsIdle {
					select {
					case <-idle:
						go func(tool string, idle chan struct{}) { tools.ProcessTool(ctx, tool); idle <- struct{}{} }(tool, idle)
					default:
					}
				}
				_ = preparer.Tick(ctx)
				_ = runner.Tick(ctx)
				// Slow SDK/network saves cannot delay probe cancellation or media Stop.
				select {
				case <-savesIdle:
					go func() { _ = service.AdvanceControls(ctx); _ = service.SaveCompleted(ctx); savesIdle <- struct{}{} }()
				default:
				}
			}
		}
	}()
	defer func() { stop(); <-done }()
	return binaries.Supervise(ctx, data, binaries.Selection{Path: exe, Version: upstream.Version, SHA256: hash}, cfg, "http://127.0.0.1:19893", func(ctx context.Context) error {
		lookup, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		return service.ReconcileSubmissions(lookup)
	})
}
