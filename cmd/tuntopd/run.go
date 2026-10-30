// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/anton2901/tuntop/internal/daemon"
	"github.com/anton2901/tuntop/internal/diag"
	"github.com/anton2901/tuntop/internal/netcfg"
	"github.com/anton2901/tuntop/internal/paths"
	"github.com/anton2901/tuntop/internal/store"
	"github.com/anton2901/tuntop/internal/version"
	"github.com/anton2901/tuntop/internal/xray"
)

// runDaemon is the service: it serves the control socket until SIGTERM.
// The VPN starts disconnected; "tuntop on" connects.
func runDaemon(args []string) error {
	fs := flag.NewFlagSet("run", flag.ExitOnError)
	stateDir := fs.String("state", paths.StateDir, "state directory (subscriptions, selection)")
	socket := fs.String("socket", paths.Socket, "control socket path")
	group := fs.String("group", paths.Group, "group allowed to control tuntop through the socket (empty: root only)")
	logLevel := fs.String("loglevel", "warning", "xray log level: debug, info, warning, error, none")
	_ = fs.Parse(args)

	if os.Geteuid() != 0 {
		return errors.New("run: must run as root (manages a network interface and routes)")
	}
	log.SetFlags(0) // journald adds timestamps
	ring := daemon.NewLogRing(500)
	out := io.MultiWriter(os.Stderr, ring)
	if f, err := diag.OpenRotating(paths.LogFile, 5<<20, 4); err != nil {
		log.Printf("log file %s: %v", paths.LogFile, err)
	} else {
		defer func() { _ = f.Close() }()
		out = io.MultiWriter(out, diag.Stamped{W: f})
	}
	log.SetOutput(out)
	log.Printf("tuntopd %s starting (%s/%s, %s)", version.String(), runtime.GOOS, runtime.GOARCH, runtime.Version())
	if dir := xray.SetupAssets(); dir != "" {
		log.Printf("geo databases: %s", dir)
	} else {
		log.Print("geo databases not found; profiles with geoip:/geosite: rules will fail")
	}

	nc := netcfg.Default
	nc.StateDir = *stateDir
	// A crash may have left the system DNS pointed at the dead tunnel.
	if err := netcfg.Recover(nc); err != nil {
		log.Printf("restore network settings: %v", err)
	}
	d, err := daemon.New(store.Store{Dir: *stateDir}, daemon.Deps{
		Tunnel:     daemon.XrayTunnel{Net: nc, LogLevel: *logLevel},
		LocalProxy: daemon.StartLocalProxy,
		KillSwitch: daemon.NetKillSwitch{Net: nc},
		Check:      daemon.CheckTunnel,
		Fetch:      daemon.Fetch,
		Proxy:      daemon.StartSOCKS,
		Probe:      daemon.Probe,
		Log:        ring,
	})
	if err != nil {
		return err
	}
	defer d.Close()

	l, err := daemon.ListenUnix(*socket, *group)
	if err != nil {
		return err
	}
	srv := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: 10 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go d.RunUpdater(ctx)
	go d.RunTraffic(ctx)
	go d.RunAutoTest(ctx)
	go d.RunHealth(ctx)
	go d.RunAutoConnect(ctx)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	log.Printf("listening on %s", *socket)
	if err := srv.Serve(l); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	log.Print("stopping, routing restored")
	return nil
}
