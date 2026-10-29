// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
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
