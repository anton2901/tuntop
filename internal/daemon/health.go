// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"log"
	"sort"
	"strings"
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
	dur, err := d.checkSession(cctx, conn.sess)
	cancel()

	if d.recordCheck(conn, dur, err) {
		d.failover(ctx, conn)
	}
}

// recordCheck stores the result of one check. It reports whether the
// server just became unreachable and the user wants to move off it.
func (d *Daemon) recordCheck(conn *connection, dur time.Duration, err error) (failover bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn != conn { // switched or turned off meanwhile
		return false
	}
	name := d.serverNameLocked(conn.key)
	if err != nil {
		d.recordLatencyLocked(conn.key, -1)
		d.healthFails++
		if d.healthFails == healthFailures {
			log.Printf("server not answering: %s: %v", name, err)
			d.emitLocked(api.Event{Kind: api.EventUnreachable, Server: name, Sub: d.subNameLocked(conn.key), Error: err.Error()})
			return d.st.AutoSwitch
		}
		return false
	}
	d.recordLatencyLocked(conn.key, max(int(dur.Milliseconds()), 1))
	if d.healthFails >= healthFailures {
		log.Printf("server answers again: %s", name)
		d.emitLocked(api.Event{Kind: api.EventRecovered, Server: name, Sub: d.subNameLocked(conn.key)})
	}
	d.healthFails = 0
	return false
}

// failoverTries is how many other servers are tried before giving up.
const failoverTries = 5

// failover moves off a dead server to one of the same subscription that
// answers through the tunnel. Servers that failed their last measurement
// are skipped; the fastest known go first. If none works, the original
// server is connected again. It stops as soon as the user connects
// elsewhere or turns the VPN off.
func (d *Daemon) failover(ctx context.Context, from *connection) {
	cands := d.failoverCandidates(from.key)
	cur := from.key
	for _, key := range cands {
		if !d.stillOn(cur) {
			return
		}
		if _, err := d.Connect(key); err != nil {
			log.Printf("auto-switch: %s: %v", d.name(key), err)
			cur = d.currentKey()
			continue
		}
		cur = key
		cctx, cancel := context.WithTimeout(ctx, healthTimeout)
		d.mu.Lock()
		var sess Session
		if d.conn != nil {
			sess = d.conn.sess
		}
		d.mu.Unlock()
		if sess == nil { // turned off meanwhile
			cancel()
			return
		}
		dur, err := d.checkSession(cctx, sess)
		cancel()
		d.mu.Lock()
		if err != nil {
			d.recordLatencyLocked(key, -1)
			d.mu.Unlock()
			log.Printf("auto-switch: %s does not answer: %v", d.name(key), err)
			continue
		}
		d.recordLatencyLocked(key, max(int(dur.Milliseconds()), 1))
		d.healthFails = 0
		d.emitLocked(api.Event{
			Kind: api.EventSwitched, Server: d.serverNameLocked(key),
			From: d.serverNameLocked(from.key), Sub: d.subNameLocked(key),
		})
		d.mu.Unlock()
		log.Printf("auto-switch: %s → %s", d.name(from.key), d.name(key))
		return
	}
	if len(cands) > 0 && d.stillOn(cur) {
		log.Printf("auto-switch: no working server found, back to %s", d.name(from.key))
		if _, err := d.Connect(from.key); err != nil {
			log.Printf("auto-switch: reconnect: %v", err)
		}
	}
}

// failoverCandidates lists up to failoverTries servers of the subscription
// of key, except key: measured ones by delay, then not yet measured ones.
func (d *Daemon) failoverCandidates(key string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	subID, _, _ := strings.Cut(key, ":")
	var known, unknown []api.Server
	for _, s := range d.serversLocked() {
		if s.SubID != subID || s.Key == key || s.Latency < 0 {
			continue
		}
		if s.Latency > 0 {
			known = append(known, s)
		} else {
			unknown = append(unknown, s)
		}
	}
	sort.SliceStable(known, func(i, j int) bool { return known[i].Latency < known[j].Latency })
	var out []string
	for _, s := range append(known, unknown...) {
		if len(out) == failoverTries {
			break
		}
		out = append(out, s.Key)
	}
	return out
}

// stillOn reports whether the tunnel is up on key.
func (d *Daemon) stillOn(key string) bool {
	return d.currentKey() == key
}

func (d *Daemon) currentKey() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.conn == nil {
		return ""
	}
	return d.conn.key
}

func (d *Daemon) name(key string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.serverNameLocked(key)
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
