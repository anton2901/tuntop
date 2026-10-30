// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anton2901/tuntop/internal/api"
)

// fakeCheck answers tunnel checks with err (nil: 37 ms).
type fakeCheck struct {
	mu  sync.Mutex
	err error
	n   int
}

func (f *fakeCheck) check(context.Context) (time.Duration, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	if f.err != nil {
		return 0, f.err
	}
	return 37 * time.Millisecond, nil
}

func (f *fakeCheck) fail(err error) {
	f.mu.Lock()
	f.err = err
	f.mu.Unlock()
}

func TestHealthCheck(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	fc := &fakeCheck{}
	e.d.check = fc.check
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	events, stop := e.d.Subscribe()
	defer stop()

	// Disconnected: nothing is checked.
	e.d.healthCheck(ctx, healthEvery)
	if fc.n != 0 {
		t.Fatal("checked while disconnected")
	}
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	<-events // connected

	// A good check is the server's latest latency.
	e.d.healthCheck(ctx, healthEvery)
	if st, _ := e.client.Status(ctx); st.Server.Latency != 37 {
		t.Fatalf("latency after check = %d", st.Server.Latency)
	}

	// Failures are reported once, after healthFailures in a row.
	fc.fail(errors.New("timeout"))
	for i := 0; i < healthFailures+2; i++ {
		e.d.healthCheck(ctx, healthEvery)
	}
	select {
	case ev := <-events:
		if ev.Kind != api.EventUnreachable || ev.Server != "🇳🇱 NL · 🟢10%" || ev.Error != "timeout" {
			t.Fatalf("event = %+v", ev)
		}
	default:
		t.Fatal("no unreachable event")
	}
	select {
	case ev := <-events:
		t.Fatalf("second event %+v", ev)
	default:
	}
	if st, _ := e.client.Status(ctx); st.Server.Latency != -1 {
		t.Fatalf("latency after failures = %d", st.Server.Latency)
	}

	// Recovery is reported too.
	fc.fail(nil)
	e.d.healthCheck(ctx, healthEvery)
	if ev := <-events; ev.Kind != api.EventRecovered {
		t.Fatalf("event = %+v", ev)
	}

	// A long gap means the machine slept: the tunnel restarts.
	e.tun.mu.Lock()
	before := len(e.tun.sessions)
	e.tun.mu.Unlock()
	e.d.healthCheck(ctx, 2*time.Hour)
	e.tun.mu.Lock()
	after, running := len(e.tun.sessions), e.tun.running
	e.tun.mu.Unlock()
	if after != before+1 || running != 1 {
		t.Fatalf("sessions %d → %d, running %d", before, after, running)
	}
}

func TestAutoConnect(t *testing.T) {
	oldRetry := autoConnectRetry
	autoConnectRetry = time.Millisecond
	t.Cleanup(func() { autoConnectRetry = oldRetry })

	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	// Off by default: nothing happens.
	e.d.RunAutoConnect(ctx)
	if st, _ := e.client.Status(ctx); st.Connected {
		t.Fatal("connected with auto-connect off")
	}
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	if s, err := e.client.SetSettings(ctx, api.Settings{AutoConnect: true}); err != nil || !s.AutoConnect {
		t.Fatalf("settings: %+v, %v", s, err)
	}

	// A restart while the network is not up yet: the first tries fail.
	e.tun.mu.Lock()
	e.tun.fail = errors.New("network is unreachable")
	e.tun.mu.Unlock()
	e.start(t)
	go func() {
		time.Sleep(20 * time.Millisecond)
		e.tun.mu.Lock()
		e.tun.fail = nil
		e.tun.mu.Unlock()
	}()
	autoConnectTries = 1000
	t.Cleanup(func() { autoConnectTries = 12 })
	e.d.RunAutoConnect(ctx)
	if st, _ := e.client.Status(ctx); !st.Connected || st.Server.Name != "🇳🇱 NL · 🟢10%" {
		t.Fatalf("after auto-connect: %+v", st)
	}

	// Turning the VPN off stops further attempts.
	e.start(t)
	_, _ = e.client.Disconnect(ctx)
	e.d.RunAutoConnect(ctx)
	if st, _ := e.client.Status(ctx); st.Connected {
		t.Fatal("auto-connected after the user turned the VPN off")
	}
}

func TestAutoSwitch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	// The NL server is dead; Germany answers.
	e.d.check = func(context.Context) (time.Duration, error) {
		if strings.HasPrefix(e.d.name(e.d.currentKey()), "🇳🇱") {
			return 0, errors.New("timeout")
		}
		return 20 * time.Millisecond, nil
	}
	events, stop := e.d.Subscribe()
	defer stop()
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	<-events // connected

	// Off by default: the dead server is only reported.
	for i := 0; i < healthFailures; i++ {
		e.d.healthCheck(ctx, healthEvery)
	}
	if ev := <-events; ev.Kind != api.EventUnreachable {
		t.Fatalf("event = %+v", ev)
	}
	if st, _ := e.client.Status(ctx); !strings.HasPrefix(st.Server.Name, "🇳🇱") {
		t.Fatalf("switched with auto-switch off: %s", st.Server.Name)
	}

	s, _ := e.client.Settings(ctx)
	s.AutoSwitch = true
	if _, err := e.client.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	// A fresh failure series triggers the switch.
	e.d.mu.Lock()
	e.d.healthFails = 0
	e.d.mu.Unlock()
	for i := 0; i < healthFailures; i++ {
		e.d.healthCheck(ctx, healthEvery)
	}
	var sw api.Event
	for ev := range events {
		if ev.Kind == api.EventSwitched {
			sw = ev
			break
		}
	}
	if !strings.HasPrefix(sw.From, "🇳🇱") || sw.Server != "🇩🇪 Germany" {
		t.Fatalf("switched event = %+v", sw)
	}
	if st, _ := e.client.Status(ctx); st.Server.Name != "🇩🇪 Germany" {
		t.Fatalf("server = %s", st.Server.Name)
	}
}

// With nothing answering, auto-switch looks again every failoverRetry checks.
func TestAutoSwitchRetries(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	e.d.mu.Lock()
	e.d.st.AutoSwitch = true
	conn := e.d.conn
	e.d.mu.Unlock()
	fail := errors.New("timeout")
	var starts []int
	for i := 1; i <= healthFailures+2*failoverRetry; i++ {
		if e.d.recordCheck(conn, 0, fail) {
			starts = append(starts, i)
		}
	}
	want := []int{healthFailures, healthFailures + failoverRetry, healthFailures + 2*failoverRetry}
	if len(starts) != len(want) || starts[0] != want[0] || starts[1] != want[1] || starts[2] != want[2] {
		t.Fatalf("attempts at checks %v, want %v", starts, want)
	}
}

func TestProxyMode(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	if s, _ := e.client.Settings(ctx); s.Mode != api.ModeTUN || s.ProxyPort != api.DefaultProxyPort {
		t.Fatalf("defaults = %+v", s)
	}
	st, err := e.client.Connect(ctx, "NL")
	if err != nil || st.Mode != api.ModeTUN || st.ProxyPort != 0 {
		t.Fatalf("tun status = %+v, %v", st, err)
	}

	// Kill switch on, then proxy mode: connected sessions are replaced
	// and the kill switch rules go away.
	s, _ := e.client.Settings(ctx)
	s.KillSwitch = true
	if _, err := e.client.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	s.Mode = api.ModeProxy
	s.ProxyPort = 17900
	if _, err := e.client.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	st, _ = e.client.Status(ctx)
	if !st.Connected || st.Mode != api.ModeProxy || st.ProxyPort != 17900 || st.KillSwitch {
		t.Fatalf("proxy status = %+v", st)
	}
	e.mu.Lock()
	ports := e.proxyPorts
	e.mu.Unlock()
	if len(ports) != 1 || ports[0] != 17900 || e.tun.running != 1 || e.ks.blocking() {
		t.Fatalf("ports %v, running %d, kill switch %v", ports, e.tun.running, e.ks.blocking())
	}

	// Back to TUN: the kill switch returns with the reconnect.
	s.Mode = api.ModeTUN
	if _, err := e.client.SetSettings(ctx, s); err != nil {
		t.Fatal(err)
	}
	if st, _ = e.client.Status(ctx); st.Mode != api.ModeTUN || !st.KillSwitch {
		t.Fatalf("tun again = %+v", st)
	}

	for _, bad := range []api.Settings{{Mode: "vpn"}, {Mode: api.ModeProxy, ProxyPort: 80}} {
		if _, err := e.client.SetSettings(ctx, bad); err == nil {
			t.Errorf("SetSettings(%+v) succeeded", bad)
		}
	}
}

// A background reconnect that started before the user pressed "off" must
// not bring the tunnel back (failover, wake from sleep, network change).
func TestOffBeatsBackgroundReconnect(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	e.d.mu.Lock()
	offs, key := e.d.offs, e.d.conn.key
	e.d.mu.Unlock()

	if _, err := e.client.Disconnect(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.d.reconnect(key, offs); err == nil {
		t.Fatal("reconnect after off succeeded")
	}
	if st, _ := e.client.Status(ctx); st.Connected {
		t.Fatal("reconnected after the user turned the VPN off")
	}
	// A reconnect that read the counter after "off" is fine.
	e.d.mu.Lock()
	offs = e.d.offs
	e.d.mu.Unlock()
	if _, err := e.d.reconnect(key, offs); err != nil {
		t.Fatal(err)
	}
}
