// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/anton2901/tuntop/internal/xray"
)

// LocalProxy starts a profile as a local proxy: SOCKS5 on socksPort and
// HTTP on socksPort+1, loopback only. Nothing in the system changes.
type LocalProxy func(profile []byte, socksPort uint16) (Session, error)

// Checker is implemented by sessions that cannot be checked through the
// system's routing (proxy mode): the check goes through their own port.
type Checker interface {
	Check(ctx context.Context) (time.Duration, error)
}

// StartLocalProxy implements LocalProxy with Xray in this process (no TUN,
// so no need for a child process).
func StartLocalProxy(profile []byte, socksPort uint16) (Session, error) {
	cfg, err := xray.WithLocalProxy(profile, "127.0.0.1", socksPort, socksPort+1, "warning")
	if err != nil {
		return nil, err
	}
	inst, err := xray.StartJSON(cfg)
	if err != nil {
		return nil, err
	}
	return &proxySession{inst: inst, proxyURL: fmt.Sprintf("socks5h://127.0.0.1:%d", socksPort)}, nil
}

type proxySession struct {
	inst     *xray.Instance
	proxyURL string
}

func (s *proxySession) Stop() error               { return s.inst.Close() }
func (s *proxySession) Traffic() (up, down int64) { return s.inst.Traffic() }
func (s *proxySession) Done() <-chan struct{}     { return nil }
func (s *proxySession) Check(ctx context.Context) (time.Duration, error) {
	return probeVia(ctx, s.proxyURL)
}

// probeVia times one request to probeURL through a proxy URL.
func probeVia(ctx context.Context, proxyURL string) (time.Duration, error) {
	pu, err := url.Parse(proxyURL)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(pu), DisableKeepAlives: true}}
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
		return 0, fmt.Errorf("probe: HTTP %d", resp.StatusCode)
	}
	return time.Since(start), nil
}
