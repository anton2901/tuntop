// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/anton2901/tuntop/internal/device"
	"github.com/anton2901/tuntop/internal/netcfg"
	"github.com/anton2901/tuntop/internal/store"
	"github.com/anton2901/tuntop/internal/sub"
	"github.com/anton2901/tuntop/internal/xray"
)

// XrayTunnel runs profiles in Xray's TUN mode with OS routing from netcfg.
// Xray runs in a child process (see child.go).
type XrayTunnel struct {
	Net      netcfg.Config
	LogLevel string
	// Exe and Args start the child; default: this executable with
	// ChildCommand.
	Exe  string
	Args []string
}

// NetKillSwitch is the OS kill switch from netcfg.
type NetKillSwitch struct{ Net netcfg.Config }

// On implements KillSwitch.
func (k NetKillSwitch) On() error { return netcfg.KillSwitchOn(k.Net) }

// Off implements KillSwitch.
func (k NetKillSwitch) Off() error { return netcfg.KillSwitchOff(k.Net) }

// netCheckEvery is how often a running tunnel checks that the network it
// is bound to is still the active one (macOS: Wi-Fi ↔ cable).
const netCheckEvery = 5 * time.Second

// Start implements Tunnel.
func (t XrayTunnel) Start(profile []byte) (Session, error) {
	bypass, err := netcfg.Bypass(t.Net)
	if err != nil {
		return nil, err
	}
	cfg, err := xray.WithTUN(profile, xray.TUNOptions{
		Name: t.Net.Iface, MTU: 1500, Bypass: bypass, LogLevel: t.LogLevel,
		Hosts: resolveServers(profile),
	})
	if err != nil {
		return nil, err
	}
	exe, args := t.Exe, t.Args
	if exe == "" {
		if exe, err = os.Executable(); err != nil {
			return nil, err
		}
		args = []string{ChildCommand}
	}
	ch, err := startChild(exe, args, cfg)
	if err != nil {
		return nil, err
	}
	undo, err := netcfg.Setup(t.Net)
	if err != nil {
		_ = ch.stop()
		_ = netcfg.RemoveLink(t.Net.Iface)
		return nil, err
	}
	s := &xraySession{ch: ch, undo: undo, iface: t.Net.Iface, done: make(chan struct{}), quit: make(chan struct{})}
	go s.watch(bypass)
	return s, nil
}

type xraySession struct {
	ch    *child
	undo  func() error
	iface string
	// done closes when the tunnel must restart: the network changed or
	// the Xray child died.
	done     chan struct{}
	quit     chan struct{}
	quitOnce sync.Once
}

func (s *xraySession) watch(bypass map[string]any) {
	t := time.NewTicker(netCheckEvery)
	defer t.Stop()
	for {
		select {
		case <-s.quit:
			return
		case <-s.ch.exited:
			if !s.ch.stopping.Load() {
				log.Print("xray child exited unexpectedly")
				close(s.done)
			}
			return
		case <-t.C:
			if netcfg.Changed(bypass) {
				close(s.done)
				return
			}
		}
	}
}

func (s *xraySession) Done() <-chan struct{} { return s.done }

func (s *xraySession) Stop() error {
	s.quitOnce.Do(func() { close(s.quit) })
	// Routes and DNS first, while the TUN still exists, then the process.
	err := s.undo()
	if cerr := s.ch.stop(); err == nil {
		err = cerr
	}
	if lerr := netcfg.RemoveLink(s.iface); err == nil {
		err = lerr
	}
	return err
}

func (s *xraySession) Traffic() (up, down int64) { return s.ch.up.Load(), s.ch.down.Load() }

// StartSOCKS implements ProxyStarter with a SOCKS inbound on a free
// loopback port.
func StartSOCKS(profile []byte) (string, func() error, error) {
	// Like the tunnel's own sockets: bypass the TUN when connected. If no
	// bypass is possible (another VPN on macOS), probe through it anyway.
	if bypass, berr := netcfg.Bypass(netcfg.Default); berr == nil {
		var err error
		if profile, err = xray.WithBypass(profile, bypass); err != nil {
			return "", nil, err
		}
	}
	// The port is free when picked, but may be taken before Xray binds it.
	var lastErr error
	for range 3 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return "", nil, err
		}
		port := uint16(l.Addr().(*net.TCPAddr).Port)
		_ = l.Close()
		cfg, err := xray.WithSOCKS(profile, "127.0.0.1", port, "none")
		if err != nil {
			return "", nil, err
		}
		inst, err := xray.StartJSON(cfg)
		if err == nil {
			return fmt.Sprintf("socks5h://127.0.0.1:%d", port), inst.Close, nil
		}
		lastErr = err
	}
	return "", nil, lastErr
}

// fetchTimeout bounds one download attempt; a blocked domain otherwise
// hangs in the TLS handshake and the fallbacks below multiply the wait.
const fetchTimeout = 20 * time.Second

// Fetch implements Fetcher with device headers (unless disabled).
func Fetch(ctx context.Context, s store.Subscription, proxy string) (*sub.Subscription, error) {
	opt := sub.Options{UserAgent: s.UserAgent, Proxy: proxy}
	if !s.NoHWID {
		dev, err := device.Current()
		if err != nil {
			return nil, fmt.Errorf("device id: %w", err)
		}
		opt.Headers = dev.Headers()
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	return sub.Fetch(ctx, s.URL, opt)
}

// CheckTunnel implements TunnelChecker: one request with the system's
// routing, which sends it into the tunnel while connected.
func CheckTunnel(ctx context.Context) (time.Duration, error) {
	client := &http.Client{Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, probeURL, http.NoBody)
	if err != nil {
		return 0, err
	}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return 0, fmt.Errorf("check: HTTP %d", resp.StatusCode)
	}
	return time.Since(start), nil
}

// probeURL answers 204 quickly from anywhere; the usual connectivity check.
const probeURL = "https://www.gstatic.com/generate_204"

// Probe implements Prober: one HTTPS request through a temporary SOCKS
// proxy on the profile, timed end to end (includes the proxy handshake,
// which is what users feel on a new connection).
func Probe(ctx context.Context, profile []byte) (time.Duration, error) {
	proxyURL, stop, err := StartSOCKS(profile)
	if err != nil {
		return 0, err
	}
	defer func() { _ = stop() }()
	return probeVia(ctx, proxyURL)
}

// resolveServers looks up the profile's proxy server domains through the
// system resolver while it still works normally (before the tunnel points
// the system DNS at itself). Names that fail are left to Xray.
func resolveServers(profile []byte) map[string][]string {
	hosts := map[string][]string{}
	for _, name := range xray.ServerDomains(profile) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		addrs, err := net.DefaultResolver.LookupIPAddr(ctx, name)
		cancel()
		if err != nil {
			log.Printf("resolve server %s: %v", name, err)
			continue
		}
		var v4, v6 []string
		for _, a := range addrs {
			if a.IP.To4() != nil {
				v4 = append(v4, a.IP.String())
			} else {
				v6 = append(v6, a.IP.String())
			}
		}
		// Xray picks one at random: prefer IPv4 only, IPv6 may be routed
		// into the tunnel or unavailable on this network.
		if len(v4) > 0 {
			hosts[name] = v4
		} else if len(v6) > 0 {
			hosts[name] = v6
		}
	}
	return hosts
}

// xrayVersion is the embedded Xray-core module version, for the status.
var xrayVersion = func() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/xtls/xray-core" {
				return d.Version
			}
		}
	}
	return ""
}()
