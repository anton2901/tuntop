// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"log"
	"time"

	"github.com/anton2901/tuntop/internal/api"
)

// Health of the running tunnel: once a minute a small request goes through
// it. The delay becomes the server's latest latency (the status shows a
// fresh figure, not the last periodic test), and several failures in a
// row are reported once, as is the recovery. A long gap between checks
// means the machine slept: its connections are stale, so the tunnel is
// restarted.
const (
	healthEvery    = time.Minute
	healthTimeout  = 15 * time.Second
	healthFailures = 3
	// sleepGap between two checks means a sleep, not a slow tick.
	sleepGap = 3 * healthEvery
)

// RunHealth checks the tunnel until ctx ends.
func (d *Daemon) RunHealth(ctx context.Context) {
	if d.check == nil {
		return
	}
	t := time.NewTicker(healthEvery)
	defer t.Stop()
	last := time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			// Wall clock: the monotonic one stops while the machine sleeps.
			gap := now.Round(0).Sub(last.Round(0))
			last = now
			d.healthCheck(ctx, gap)
		}
	}
}

// healthCheck runs one check; gap is the time since the previous one.
func (d *Daemon) healthCheck(ctx context.Context, gap time.Duration) {
	d.mu.Lock()
	conn := d.conn
	if conn == nil {
		d.healthFails = 0
	}
	d.mu.Unlock()
	if conn == nil {
		return
	}
	if gap > sleepGap {
		log.Printf("resumed after %s asleep, reconnecting", gap.Round(time.Minute))
		if _, err := d.Connect(conn.key); err != nil {
			log.Printf("reconnect after sleep: %v", err)
		}
		return
	}

	cctx, cancel := context.WithTimeout(ctx, healthTimeout)
	dur, err := d.check(cctx)
	cancel()

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != conn { // switched or turned off meanwhile
		return
	}
	name := d.serverNameLocked(conn.key)
	if err != nil {
		d.recordLatencyLocked(conn.key, -1)
		d.healthFails++
		if d.healthFails == healthFailures {
			log.Printf("server not answering: %s: %v", name, err)
			d.emitLocked(api.Event{Kind: api.EventUnreachable, Server: name, Sub: d.subNameLocked(conn.key), Error: err.Error()})
		}
		return
	}
	d.recordLatencyLocked(conn.key, max(int(dur.Milliseconds()), 1))
	if d.healthFails >= healthFailures {
		log.Printf("server answers again: %s", name)
		d.emitLocked(api.Event{Kind: api.EventRecovered, Server: name, Sub: d.subNameLocked(conn.key)})
	}
	d.healthFails = 0
}

// serverNameLocked is the name of a server key, or the key itself.
func (d *Daemon) serverNameLocked(key string) string {
	if e, ok := d.entryLocked(key); ok {
		return e.Name
	}
	return key
}

// Auto-connect at start: the network may come up after the service, so
// a failed attempt is retried for a while.
var (
	autoConnectRetry = 10 * time.Second
	autoConnectTries = 12
)

// RunAutoConnect connects to the selected server if the setting asks for
// it. It gives up after autoConnectTries, or once the user connects or
// turns the VPN off.
func (d *Daemon) RunAutoConnect(ctx context.Context) {
	d.mu.Lock()
	want := d.st.AutoConnect && d.st.Selected != ""
	d.mu.Unlock()
	if !want {
		return
	}
	for i := 0; i < autoConnectTries; i++ {
		d.mu.Lock()
		done := d.conn != nil || d.userOff
		d.mu.Unlock()
		if done {
			return
		}
		_, err := d.Connect("")
		if err == nil {
			log.Print("auto-connected")
			return
		}
		log.Printf("auto-connect: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(autoConnectRetry):
		}
	}
}
