// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"testing"

	"github.com/anton2901/tuntop/internal/api"
)

func TestKillSwitch(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	// Turning it on while disconnected blocks nothing yet.
	if s, err := e.client.SetSettings(ctx, api.Settings{KillSwitch: true}); err != nil || !s.KillSwitch || e.ks.blocking() {
		t.Fatalf("settings: %+v, %v, blocking %v", s, err, e.ks.blocking())
	}
	st, err := e.client.Connect(ctx, "NL")
	if err != nil || !st.KillSwitch || !e.ks.blocking() {
		t.Fatalf("connect: %+v, %v", st, err)
	}
	// A failed switch leaves the machine blocked, not leaking.
	e.tun.mu.Lock()
	e.tun.fail = errors.New("server down")
	e.tun.mu.Unlock()
	if _, err = e.client.Connect(ctx, "Germany"); err == nil {
		t.Fatal("connect to a dead server succeeded")
	}
	if st, _ = e.client.Status(ctx); st.Connected || !st.KillSwitch || !e.ks.blocking() {
		t.Fatalf("failed switch: %+v, blocking %v", st, e.ks.blocking())
	}
	// Off means direct traffic.
	if st, _ = e.client.Disconnect(ctx); st.KillSwitch || e.ks.blocking() {
		t.Fatalf("off: %+v", st)
	}
	// Turning the setting off while connected unblocks at once.
	e.tun.mu.Lock()
	e.tun.fail = nil
	e.tun.mu.Unlock()
	_, _ = e.client.Connect(ctx, "NL")
	if s, _ := e.client.SetSettings(ctx, api.Settings{}); s.KillSwitch || e.ks.blocking() {
		t.Fatalf("setting off: %+v", s)
	}
	// The setting survives a restart; the block comes with the next connect.
	_, _ = e.client.SetSettings(ctx, api.Settings{KillSwitch: true})
	e.start(t)
	if s, _ := e.client.Settings(ctx); !s.KillSwitch {
		t.Fatal("setting not saved")
	}
}

func TestKillSwitchUnsupported(t *testing.T) {
	e := newEnv(t)
	e.d.ks = nil
	if _, err := e.client.SetSettings(context.Background(), api.Settings{KillSwitch: true}); err == nil {
		t.Fatal("kill switch accepted without OS support")
	}
}
