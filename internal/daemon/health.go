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
	// wakeEvery is how often the clock is compared with the wall time to
	// notice a wake-up; wakeGap beyond it means the machine slept. A tick
	// fires within wakeEvery of the wake-up, so the stale tunnel is replaced
	// in seconds, and sleeps from ~wakeGap on are noticed.
	wakeEvery = 5 * time.Second
	wakeGap   = 20 * time.Second
	// quietAfterWake keeps background downloads off the network while the
	// tunnel is being replaced.
	quietAfterWake = time.Minute
)

// RunHealth checks the tunnel until ctx ends, and restarts it after a sleep.
func (d *Daemon) RunHealth(ctx context.Context) {
	if d.check == nil {
		return
	}
	t := time.NewTicker(wakeEvery)
	defer t.Stop()
	last, lastCheck := time.Now(), time.Now()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			// Wall clock: the monotonic one stops while the machine sleeps.
			gap := now.Round(0).Sub(last.Round(0))
			last = now
			if gap > wakeGap {
				lastCheck = now
				d.resumed(ctx, gap)
				continue
			}
			if now.Round(0).Sub(lastCheck.Round(0)) >= healthEvery {
				lastCheck = now
				d.healthCheck(ctx, healthEvery)
			}
		}
	}
}

// healthCheck runs one check; gap is the time since the previous one.
func (d *Daemon) healthCheck(ctx context.Context, gap time.Duration) {
	if gap > sleepGap {
		d.resumed(ctx, gap)
		return
	}
	d.mu.Lock()
	conn := d.conn
	offs := d.offs
	if conn == nil {
		d.healthFails = 0
	}
	d.mu.Unlock()
	if conn == nil {
		return
	}

	cctx, cancel := context.WithTimeout(ctx, healthTimeout)
	dur, err := d.checkSession(cctx, conn.sess)
	cancel()

	if d.recordCheck(conn, dur, err) {
		d.failover(ctx, conn, offs)
	}
}

// resumed restarts the tunnel after a sleep: its connections are stale.
func (d *Daemon) resumed(ctx context.Context, gap time.Duration) {
	d.mu.Lock()
	conn := d.conn
	offs := d.offs
	d.quietUntil = time.Now().Add(quietAfterWake)
	d.mu.Unlock()
	if conn == nil {
		return
	}
	log.Printf("resumed after %s asleep, reconnecting", gap.Round(time.Second))
	d.reconnectRetry(ctx, conn.key, offs, "after sleep")
}

// Pauses between attempts to bring the tunnel back: the network often
// is not there yet after a wake-up or a move to another network.
var (
	retryFirst = 2 * time.Second
	retryLast  = 30 * time.Second
)

// reconnectRetry reconnects to key and, while that fails, tries again with
// a growing pause. It gives up when the user turns the VPN off (offs moved),
// something else connected meanwhile, or ctx ends. The kill switch stays on
// during the retries: the traffic must not leave outside the tunnel.
func (d *Daemon) reconnectRetry(ctx context.Context, key string, offs uint64, why string) {
	if !d.retrying.CompareAndSwap(false, true) {
		return // another loop is already at it
	}
	defer d.retrying.Store(false)
	pause := retryFirst
	for {
		_, err := d.reconnect(key, offs)
		if err == nil {
			return
		}
		d.mu.Lock()
		stop := d.offs != offs || d.conn != nil
		d.quietUntil = time.Now().Add(quietAfterWake)
		d.mu.Unlock()
		log.Printf("reconnect %s: %v (retry in %s)", why, err, pause)
		if stop {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		pause = min(pause*2, retryLast)
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
		// Nothing worked last time (the count is only reset by a success):
		// look again every failoverRetry checks, other servers may be back.
		over := d.healthFails - healthFailures
		return over > 0 && over%failoverRetry == 0 && d.st.AutoSwitch
	}
	d.recordLatencyLocked(conn.key, max(int(dur.Milliseconds()), 1))
	if d.healthFails >= healthFailures {
		log.Printf("server answers again: %s", name)
		d.emitLocked(api.Event{Kind: api.EventRecovered, Server: name, Sub: d.subNameLocked(conn.key)})
	}
	d.healthFails = 0
	return false
}

// failoverRetry is how many failed checks pass between auto-switch attempts
// while no server answers.
const failoverRetry = 5

// failoverTries is how many other servers are tried before giving up.
const failoverTries = 5

// failover moves off a dead server to one of the same subscription that
// answers through the tunnel. Servers that failed their last measurement
// are skipped; the fastest known go first. If none works, the original
// server is connected again. It stops as soon as the user connects
// elsewhere or turns the VPN off.
func (d *Daemon) failover(ctx context.Context, from *connection, offs uint64) {
	cands := d.failoverCandidates(from.key)
	cur := from.key
	for _, key := range cands {
		if !d.stillOn(cur) {
			return
		}
		if _, err := d.reconnect(key, offs); err != nil {
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
		if _, err := d.reconnect(from.key, offs); err != nil {
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
