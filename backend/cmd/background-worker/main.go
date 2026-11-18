// Command background-worker is the package's fixed background worker. DSM runs
// it as the package's systemd user unit under the package account; it never
// runs as root and takes no input except the jobs in its private spool.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"

	"dsmapp/internal/gopeed"
	"dsmapp/internal/sdksettings"
	"github.com/synopackageland/syno-pkg-land-sdk/background"
	"github.com/synopackageland/syno-pkg-land-sdk/delegate"
)

// Set by `synopkgland build-spk` through -ldflags -X.
var packageID = "dev"
var delegateScope, delegateOperationsJSON, delegateDestinationsJSON, delegateLifetime string

func main() {
	log.SetFlags(0)
	log.SetPrefix(packageID + " background: ")
	if err := background.RefuseRoot(os.Geteuid()); err != nil {
		log.Fatal(err)
	}
	name := ""
	if u, err := user.Current(); err == nil {
		name = u.Username
	}
	info := background.WorkerInfo{PID: os.Getpid(), UID: os.Getuid(), Username: name}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	log.Printf("started as uid %s", strconv.Itoa(info.UID))
	dataDir := "/var/packages/" + packageID + "/var"
	service := &delegate.Service{Client: delegate.Local(packageID), Store: background.NewStore(dataDir + "/background"), StagingDir: dataDir + "/executor-staging"}
	settings, err := app.ParseDelegateConfig(app.Config{DelegateScope: delegateScope, DelegateOperationsJSON: delegateOperationsJSON, DelegateDestinationsJSON: delegateDestinationsJSON, DelegateLifetime: delegateLifetime})
	if err != nil {
		log.Fatal(err)
	}
	service.Scope, service.Operations, service.Destinations, service.Lifetime = settings.Scope, settings.Operations, settings.Destinations, settings.Lifetime
	// Both loops live in the existing package-account worker unit. CGI requests
	// only submit to the SDK spool; no work depends on their lifetime.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 2)
	go func() { done <- service.Run(ctx, info) }()
	go func() { done <- gopeed.New(dataDir + "/gopeed").SearchRunner(info).Run(ctx) }()
	first := <-done
	cancel()
	second := <-done
	if first != nil {
		log.Fatal(first)
	}
	if second != nil {
		log.Fatal(second)
	}
}
