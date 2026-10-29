// SPDX-License-Identifier: GPL-3.0-or-later

// Package daemon is tuntopd's core: it keeps subscriptions and the selected
// server, connects and disconnects the tunnel and refreshes subscriptions
// in the background. The HTTP API in http.go is a thin layer over it.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/link"
	"github.com/anton2901/tuntop/internal/store"
	"github.com/anton2901/tuntop/internal/sub"
	"github.com/anton2901/tuntop/internal/version"
)

// Tunnel starts the data path for a profile. The real one (XrayTunnel)
// runs Xray in TUN mode; tests use a fake.
type Tunnel interface {
	Start(profile []byte) (Session, error)
}

// Session is a running tunnel.
type Session interface {
	Stop() error
	// Traffic returns bytes up and down since the session started.
	Traffic() (up, down int64)
	// Done closes when the tunnel must be restarted (the network changed);
	// nil means never.
	Done() <-chan struct{}
}

// Fetcher downloads a subscription, optionally through a proxy URL.
type Fetcher func(ctx context.Context, s store.Subscription, proxy string) (*sub.Subscription, error)

// ProxyStarter runs a profile as a local SOCKS proxy (used to download
// subscriptions that are blocked on direct connections) and returns its
// URL and a stop function.
type ProxyStarter func(profile []byte) (proxyURL string, stop func() error, err error)

// Prober measures the delay of one request through a profile.
type Prober func(ctx context.Context, profile []byte) (time.Duration, error)

// TunnelChecker measures one request through the running tunnel (the
// system's own routing), see RunHealth.
type TunnelChecker func(ctx context.Context) (time.Duration, error)

// KillSwitch blocks traffic outside the tunnel at the OS level. Its rules
// outlive the process: a crash leaves the machine blocked, not leaking.
type KillSwitch interface {
	On() error
	Off() error
}

// Deps are the daemon's side-effecting parts; tests replace them.
type Deps struct {
	// KillSwitch is nil where the OS has none; the setting is refused.
	KillSwitch KillSwitch
	// Check, if set, watches the running tunnel (RunHealth).
	Check  TunnelChecker
	Tunnel Tunnel
	Fetch  Fetcher
	Proxy  ProxyStarter
	Probe  Prober
	// Log, if set, is served by the API (GET /v1/log).
	Log *LogRing
}

// Daemon is safe for concurrent use.
type Daemon struct {
	store  store.Store
	tunnel Tunnel
	fetch  Fetcher
	proxy  ProxyStarter
	probe  Prober
	logs   *LogRing
	check  TunnelChecker
	// healthFails counts failed tunnel checks in a row.
	healthFails int
	// userOff: the user turned the VPN off since start; auto-connect stops.
	userOff bool
	ks      KillSwitch
	// ksOn: the kill switch rules are in place.
	ksOn bool

	mu     sync.Mutex
	st     *store.State
	parsed map[string]*sub.Subscription // by subscription id
	conn   *connection
	// lastErr is the last connect failure.
	lastErr string
	// updating serializes subscription downloads.
	updating sync.Mutex
	// latency holds recent probe results per server key (ms, -1 failed),
	// oldest first, at most latencyKeep.
	latency map[string][]int
	// lastAutoTest is when the background latency test last ran.
	lastAutoTest time.Time
	// listeners receive events (see Subscribe); guarded by mu.
	listeners map[chan api.Event]struct{}
	// expiryNotified remembers the day an expiry warning was sent per sub.
	expiryNotified map[string]string
	// traffic history, one sample per second; see RunTraffic.
	upHist, downHist []int64
	lastUp, lastDown int64
}

// recordLatencyLocked adds a measurement (ms, -1 failed) to a server's
// history.
func (d *Daemon) recordLatencyLocked(key string, ms int) {
	h := d.latency[key]
	h = append(h, ms)
	d.latency[key] = h[max(len(h)-latencyKeep, 0):]
}

// Sizes of the kept histories.
const (
	latencyKeep = 12
	trafficKeep = 300
)

type connection struct {
	key   string
	since time.Time
	sess  Session
}

// New loads state and cached subscriptions.
func New(st store.Store, deps Deps) (*Daemon, error) {
	s, err := st.Load()
	if err != nil {
		return nil, err
	}
	d := &Daemon{
		store: st, tunnel: deps.Tunnel, ks: deps.KillSwitch, check: deps.Check, fetch: deps.Fetch, proxy: deps.Proxy, probe: deps.Probe, logs: deps.Log,
		st: s, parsed: map[string]*sub.Subscription{}, latency: map[string][]int{},
		listeners: map[chan api.Event]struct{}{}, expiryNotified: map[string]string{},
	}
	for _, sb := range s.Subscriptions {
		body, err := st.Body(sb.ID)
		if err != nil || body == nil {
			continue
		}
		if p, err := sub.Parse(body, nil); err == nil {
			d.parsed[sb.ID] = p
		} else {
			log.Printf("subscription %s: cached body unusable: %v", sb.ID, err)
		}
	}
	return d, nil
}

// Status reports the current state.
func (d *Daemon) Status() api.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.statusLocked()
}

func (d *Daemon) statusLocked() api.Status {
	s := api.Status{
		Version: version.Version, Build: version.String(), Xray: xrayVersion,
		LastError: d.lastErr, KillSwitch: d.ksOn,
	}
	servers := d.serversLocked()
	find := func(key string) *api.Server {
		for i := range servers {
			if servers[i].Key == key {
				return &servers[i]
			}
		}
		return nil
	}
	if d.conn != nil {
		s.Connected = true
		s.Since = d.conn.since
		s.Server = find(d.conn.key)
		if s.Server == nil { // its subscription was updated or removed meanwhile
			s.Server = &api.Server{Key: d.conn.key, Name: "(no longer in the list)"}
		}
	}
	s.Selected = find(d.st.Selected)
	return s
}

// Servers lists all servers: favorites first, then subscriptions in their
// order, manual links last.
func (d *Daemon) Servers() []api.Server {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.serversLocked()
}

func (d *Daemon) serversLocked() []api.Server {
	var out []api.Server
	add := func(sb store.Subscription) {
		p := d.parsed[sb.ID]
		if p == nil {
			return
		}
		for _, e := range p.Entries {
			out = append(out, api.Server{
				Index:   len(out) + 1,
				Key:     sb.ID + ":" + e.Key,
				Name:    strings.TrimSpace(e.Name),
				Group:   e.Kind == sub.KindXrayGroup,
				Proxies: e.Proxies,
				Proto:   e.Proto,
				SubID:   sb.ID,
				SubName: sb.Name(),
				Address: e.Address,
			})
			if h := d.latency[sb.ID+":"+e.Key]; len(h) > 0 {
				out[len(out)-1].Latency = h[len(h)-1]
				out[len(out)-1].LatencyHistory = append([]int(nil), h...)
			}
		}
	}
	var manual *store.Subscription
	for i, sb := range d.st.Subscriptions {
		if sb.ID == store.ManualID {
			manual = &d.st.Subscriptions[i]
			continue
		}
		add(sb)
	}
	if manual != nil {
		add(*manual)
	}
	return d.favoritesFirstLocked(out)
}

// favoritesFirstLocked marks favorites and moves them to the top, keeping
// the list order within both parts, and renumbers.
func (d *Daemon) favoritesFirstLocked(list []api.Server) []api.Server {
	fav := map[string]bool{}
	for _, k := range d.st.Favorites {
		fav[k] = true
	}
	out := make([]api.Server, 0, len(list))
	for _, s := range list {
		if fav[s.Key] {
			s.Favorite = true
			out = append(out, s)
		}
	}
	for _, s := range list {
		if !fav[s.Key] {
			out = append(out, s)
		}
	}
	for i := range out {
		out[i].Index = i + 1
	}
	return out
}

// SetFavorite marks or unmarks the server matching q (see resolveLocked).
func (d *Daemon) SetFavorite(q string, on bool) (api.Server, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if strings.TrimSpace(q) == "" {
		return api.Server{}, errors.New("which server?")
	}
	key, err := d.resolveLocked(q)
	if err != nil {
		return api.Server{}, err
	}
	favs := d.st.Favorites[:0:0]
	for _, k := range d.st.Favorites {
		if k != key {
			favs = append(favs, k)
		}
	}
	if on {
		favs = append(favs, key)
	}
	d.st.Favorites = favs
	if err := d.store.Save(d.st); err != nil {
		return api.Server{}, err
	}
	for _, s := range d.serversLocked() {
		if s.Key == key {
			return s, nil
		}
	}
	return api.Server{}, fmt.Errorf("no server %q", key)
}

// entryLocked finds the entry for a server key.
func (d *Daemon) entryLocked(key string) (sub.Entry, bool) {
	subID, entryKey, ok := strings.Cut(key, ":")
	if !ok {
		return sub.Entry{}, false
	}
	p := d.parsed[subID]
	if p == nil {
		return sub.Entry{}, false
	}
	for _, e := range p.Entries {
		if e.Key == entryKey {
			return e, true
		}
	}
	return sub.Entry{}, false
}

// resolveLocked turns a user query into a server key: exact key, list
// number, or a unique case-insensitive part of the name.
func (d *Daemon) resolveLocked(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		if d.st.Selected == "" {
			return "", errors.New("no server selected yet: choose one, e.g. \"tuntop on 1\"")
		}
		return d.st.Selected, nil
	}
	servers := d.serversLocked()
	if n, err := strconv.Atoi(q); err == nil {
		if n < 1 || n > len(servers) {
			return "", fmt.Errorf("server number %d out of range 1..%d", n, len(servers))
		}
		return servers[n-1].Key, nil
	}
	var matches []api.Server
	for _, s := range servers {
		if s.Key == q {
			return s.Key, nil
		}
		if strings.Contains(strings.ToLower(s.Name), strings.ToLower(q)) {
			matches = append(matches, s)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no server matches %q", q)
	case 1:
		return matches[0].Key, nil
	default:
		return "", fmt.Errorf("%q matches %d servers, be more specific or use the number", q, len(matches))
	}
}

// Connect connects to the server matching q (see resolveLocked), replacing
// any current connection, and remembers it as selected.
func (d *Daemon) Connect(q string) (api.Status, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	key, err := d.resolveLocked(q)
	if err != nil {
		return d.statusLocked(), err
	}
	e, ok := d.entryLocked(key)
	if !ok {
		return d.statusLocked(), errors.New("selected server is no longer in its subscription; choose another")
	}
	profile, err := e.Profile()
	if err != nil {
		return d.statusLocked(), err
	}
	d.disconnectLocked()
	sess, err := d.tunnel.Start(profile)
	if err != nil {
		d.lastErr = err.Error()
		d.emitLocked(api.Event{Kind: api.EventConnectFailed, Server: e.Name, Error: err.Error()})
		return d.statusLocked(), fmt.Errorf("connect: %w", err)
	}
	d.lastErr = ""
	d.conn = &connection{key: key, since: time.Now(), sess: sess}
	if d.st.KillSwitch {
		d.killSwitchLocked(true)
	}
	d.lastUp, d.lastDown = 0, 0
	go d.restartOnChange(sess)
	if d.st.Selected != key {
		d.st.Selected = key
		if err := d.store.Save(d.st); err != nil {
			log.Printf("save state: %v", err)
		}
	}
	log.Printf("connected: %s", e.Name)
	d.emitLocked(api.Event{Kind: api.EventConnected, Server: strings.TrimSpace(e.Name), Sub: d.subNameLocked(key)})
	return d.statusLocked(), nil
}

// restartOnChange reconnects when the session reports a network change,
// unless the user switched or disconnected meanwhile.
func (d *Daemon) restartOnChange(sess Session) {
	done := sess.Done()
	if done == nil {
		return
	}
	<-done
	d.mu.Lock()
	current := d.conn != nil && d.conn.sess == sess
	var key string
	if current {
		key = d.conn.key // a subscription update may have remapped it
	}
	d.mu.Unlock()
	if !current {
		return
	}
	// The session says why it ended (Xray exited, or the network changed).
	log.Print("tunnel stopped, reconnecting")
	if _, err := d.Connect(key); err != nil {
		log.Printf("reconnect: %v", err)
	}
}

// Disconnect stops the tunnel; normal routing returns.
func (d *Daemon) Disconnect() api.Status {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.userOff = true
	if d.conn != nil {
		d.disconnectLocked()
		d.emitLocked(api.Event{Kind: api.EventDisconnected})
	}
	// Off means direct traffic, also after a failed reconnect left the
	// kill switch blocking.
	d.killSwitchLocked(false)
	return d.statusLocked()
}

// killSwitchLocked puts the kill switch rules in place or removes them.
// A failure to block is reported as the last error: the tunnel runs, but
// unprotected.
func (d *Daemon) killSwitchLocked(on bool) {
	if d.ks == nil || d.ksOn == on {
		return
	}
	f := d.ks.Off
	if on {
		f = d.ks.On
	}
	if err := f(); err != nil {
		log.Printf("kill switch: %v", err)
		d.lastErr = "kill switch: " + err.Error()
		return
	}
	d.ksOn = on
	if on {
		log.Print("kill switch on: traffic outside the tunnel is blocked")
	} else {
		log.Print("kill switch off")
	}
}

// Subscribe returns a channel of events and a function to stop receiving.
// Slow listeners miss events rather than block the daemon.
func (d *Daemon) Subscribe() (<-chan api.Event, func()) {
	ch := make(chan api.Event, 16)
	d.mu.Lock()
	d.listeners[ch] = struct{}{}
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		delete(d.listeners, ch)
		d.mu.Unlock()
	}
}

func (d *Daemon) emitLocked(e api.Event) {
	e.Time = time.Now()
	for ch := range d.listeners {
		select {
		case ch <- e:
		default:
		}
	}
}

func (d *Daemon) subNameLocked(key string) string {
	id, _, _ := strings.Cut(key, ":")
	if i := d.subIndexLocked(id); i >= 0 {
		return d.st.Subscriptions[i].Name()
	}
	return ""
}

func (d *Daemon) disconnectLocked() {
	if d.conn == nil {
		return
	}
	if err := d.conn.sess.Stop(); err != nil {
		log.Printf("disconnect: %v", err)
	}
	d.conn = nil
	log.Print("disconnected")
}

// Close disconnects; call on shutdown.
func (d *Daemon) Close() {
	d.Disconnect()
}

// Subscriptions lists subscriptions (manual links excluded when empty).
func (d *Daemon) Subscriptions() []api.Subscription {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]api.Subscription, 0, len(d.st.Subscriptions))
	var manual *api.Subscription
	for _, sb := range d.st.Subscriptions {
		a := d.apiSubLocked(sb)
		if sb.ID == store.ManualID {
			manual = &a
			continue
		}
		out = append(out, a)
	}
	if manual != nil {
		out = append(out, *manual)
	}
	return out
}

// MoveSubscription puts subscription id at position to (1-based, among
// subscriptions; manual links always stay last).
func (d *Daemon) MoveSubscription(id string, to int) ([]api.Subscription, error) {
	d.mu.Lock()
	i := d.subIndexLocked(id)
	if i < 0 || id == store.ManualID {
		d.mu.Unlock()
		return nil, fmt.Errorf("no subscription %q", id)
	}
	sb := d.st.Subscriptions[i]
	var rest []store.Subscription
	for j, r := range d.st.Subscriptions {
		if j != i {
			rest = append(rest, r)
		}
	}
	// The position counts regular subscriptions only.
	at, n := len(rest), 0
	for j, r := range rest {
		if r.ID == store.ManualID {
			continue
		}
		if n++; n == to {
			at = j
			break
		}
	}
	if to <= 1 {
		at = 0
	}
	d.st.Subscriptions = append(append(rest[:at:at], sb), rest[at:]...)
	err := d.store.Save(d.st)
	d.mu.Unlock()
	return d.Subscriptions(), err
}

func (d *Daemon) apiSubLocked(sb store.Subscription) api.Subscription {
	a := api.Subscription{
		ID: sb.ID, URL: maskURL(sb.URL), Name: sb.Name(), UpdatedAt: sb.UpdatedAt, Error: sb.LastError,
		Upload: sb.Info.Upload, Download: sb.Info.Download, Total: sb.Info.Total, Expire: sb.Info.Expire,
		Announce: sb.Info.Announce, SupportURL: sb.Info.SupportURL,
	}
	if p := d.parsed[sb.ID]; p != nil {
		a.Servers = len(p.Entries)
	}
	return a
}

// AddSubscription downloads a new subscription and stores it.
func (d *Daemon) AddSubscription(ctx context.Context, req api.AddSubscriptionRequest) (api.Subscription, error) {
	u, err := url.Parse(strings.TrimSpace(req.URL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return api.Subscription{}, errors.New("subscription must be an http(s) URL")
	}
	d.mu.Lock()
	for _, sb := range d.st.Subscriptions {
		if sb.URL == u.String() {
			d.mu.Unlock()
			return api.Subscription{}, errors.New("this subscription is already added")
		}
	}
	d.mu.Unlock()

	sb := store.Subscription{ID: store.NewID(), URL: u.String(), UserAgent: req.UserAgent, NoHWID: req.NoHWID}
	parsed, err := d.download(ctx, sb)
	if err != nil {
		log.Printf("add subscription %s: %v", maskURL(sb.URL), err)
		return api.Subscription{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.store.SaveBody(sb.ID, parsed.Body); err != nil {
		return api.Subscription{}, err
	}
	sb.Info, sb.UpdatedAt = parsed.Info, time.Now()
	d.st.Subscriptions = append(d.st.Subscriptions, sb)
	d.parsed[sb.ID] = parsed
	if err := d.store.Save(d.st); err != nil {
		return api.Subscription{}, err
	}
	return d.apiSubLocked(sb), nil
}

// RemoveSubscription deletes a subscription and its cached servers.
func (d *Daemon) RemoveSubscription(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.subIndexLocked(id)
	if i < 0 {
		return fmt.Errorf("no subscription %q", id)
	}
	d.st.Subscriptions = append(d.st.Subscriptions[:i], d.st.Subscriptions[i+1:]...)
	delete(d.parsed, id)
	if strings.HasPrefix(d.st.Selected, id+":") {
		d.st.Selected = ""
	}
	favs := d.st.Favorites[:0:0]
	for _, k := range d.st.Favorites {
		if !strings.HasPrefix(k, id+":") {
			favs = append(favs, k)
		}
	}
	d.st.Favorites = favs
	_ = d.store.DeleteBody(id)
	return d.store.Save(d.st)
}

func (d *Daemon) subIndexLocked(id string) int {
	for i, sb := range d.st.Subscriptions {
		if sb.ID == id {
			return i
		}
	}
	return -1
}

// Update re-downloads one subscription, or all when id is empty. The
// current connection keeps running; a new server list applies on the next
// connect.
func (d *Daemon) Update(ctx context.Context, id string) ([]api.Subscription, error) {
	d.mu.Lock()
	var todo []store.Subscription
	for _, sb := range d.st.Subscriptions {
		if sb.ID != store.ManualID && (id == "" || sb.ID == id) {
			todo = append(todo, sb)
		}
	}
	d.mu.Unlock()
	if id != "" && len(todo) == 0 {
		return nil, fmt.Errorf("no subscription %q", id)
	}

	var errs []error
	for _, sb := range todo {
		parsed, err := d.download(ctx, sb)
		d.mu.Lock()
		i := d.subIndexLocked(sb.ID)
		if i < 0 { // removed meanwhile
			d.mu.Unlock()
			continue
		}
		cur := &d.st.Subscriptions[i]
		if err != nil {
			cur.LastError = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", cur.Name(), err))
		} else if err := d.store.SaveBody(sb.ID, parsed.Body); err != nil {
			errs = append(errs, err)
		} else {
			cur.Info, cur.UpdatedAt, cur.LastError = parsed.Info, time.Now(), ""
			d.remapLocked(sb.ID, d.parsed[sb.ID], parsed)
			d.parsed[sb.ID] = parsed
		}
		_ = d.store.Save(d.st)
		d.mu.Unlock()
	}
	return d.Subscriptions(), errors.Join(errs...)
}

// download fetches a subscription directly (through the tunnel when
// connected); if that fails while disconnected, it retries through a
// temporary local proxy built from servers of other subscriptions. Some
// providers' subscription domains are blocked by SNI on direct connections.
func (d *Daemon) download(ctx context.Context, sb store.Subscription) (*sub.Subscription, error) {
	d.updating.Lock()
	defer d.updating.Unlock()

	parsed, err := d.fetch(ctx, sb, "")
	if err == nil {
		return parsed, nil
	}
	if errors.Is(err, sub.ErrNoServers) {
		return nil, err // it downloaded fine; another route gets the same answer
	}
	d.mu.Lock()
	connected := d.conn != nil
	var candidates [][]byte
	for _, s := range d.st.Subscriptions {
		p := d.parsed[s.ID]
		if s.ID == sb.ID || p == nil {
			continue
		}
		for _, e := range p.Entries {
			if prof, perr := e.Profile(); perr == nil {
				candidates = append(candidates, prof)
			}
			if len(candidates) >= 2 {
				break
			}
		}
	}
	d.mu.Unlock()
	if connected || d.proxy == nil || len(candidates) == 0 {
		return nil, err
	}
	for _, prof := range candidates {
		proxyURL, stop, perr := d.proxy(prof)
		if perr != nil {
			continue
		}
		parsed, perr = d.fetch(ctx, sb, proxyURL)
		_ = stop()
		if perr == nil {
			log.Printf("subscription %s downloaded through a temporary proxy (direct: %v)", sb.ID, err)
			return parsed, nil
		}
	}
	return nil, fmt.Errorf("%w (also failed through other servers)", err)
}

// probeParallel limits concurrent probes: each runs its own Xray instance.
const probeParallel = 6

// Test measures latency of the given servers (all when keys is empty) and
// returns the server list with results. Probes bypass the TUN, so results
// are the same whether or not the VPN is on.
func (d *Daemon) Test(ctx context.Context, keys []string) ([]api.Server, error) {
	if d.probe == nil {
		return nil, errors.New("latency test is not available")
	}
	type job struct {
		key     string
		profile []byte
	}
	d.mu.Lock()
	want := map[string]bool{}
	for _, k := range keys {
		want[k] = true
	}
	var jobs []job
	for _, s := range d.serversLocked() {
		if len(keys) > 0 && !want[s.Key] {
			continue
		}
		if e, ok := d.entryLocked(s.Key); ok {
			if p, err := e.Profile(); err == nil {
				jobs = append(jobs, job{s.Key, p})
			}
		}
	}
	d.mu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, probeParallel)
	for _, j := range jobs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			ms := -1
			if dur, err := d.probe(pctx, j.profile); err == nil {
				ms = max(int(dur.Milliseconds()), 1)
			}
			d.mu.Lock()
			d.recordLatencyLocked(j.key, ms)
			d.mu.Unlock()
		}()
	}
	wg.Wait()
	return d.Servers(), ctx.Err()
}

// Traffic reports throughput and history.
func (d *Daemon) Traffic() api.Traffic {
	d.mu.Lock()
	defer d.mu.Unlock()
	t := api.Traffic{
		Connected: d.conn != nil,
		UpHist:    append([]int64{}, d.upHist...),
		DownHist:  append([]int64{}, d.downHist...),
		UpTotal:   d.lastUp,
		DownTotal: d.lastDown,
	}
	if n := len(d.upHist); n > 0 {
		t.UpRate, t.DownRate = d.upHist[n-1], d.downHist[n-1]
	}
	return t
}

// sampleTraffic records one second of throughput.
func (d *Daemon) sampleTraffic() {
	d.mu.Lock()
	defer d.mu.Unlock()
	var up, down int64
	if d.conn != nil {
		u, dn := d.conn.sess.Traffic()
		up, down = max(u-d.lastUp, 0), max(dn-d.lastDown, 0)
		d.lastUp, d.lastDown = u, dn
	}
	d.upHist = append(d.upHist, up)
	d.downHist = append(d.downHist, down)
	if over := len(d.upHist) - trafficKeep; over > 0 {
		d.upHist = d.upHist[over:]
		d.downHist = d.downHist[over:]
	}
}

// RunTraffic samples throughput every second until ctx ends.
func (d *Daemon) RunTraffic(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.sampleTraffic()
		}
	}
}

// Settings returns the service settings.
func (d *Daemon) Settings() api.Settings {
	d.mu.Lock()
	defer d.mu.Unlock()
	return api.Settings{AutoTestMinutes: d.st.AutoTestMinutes, KillSwitch: d.st.KillSwitch, AutoConnect: d.st.AutoConnect}
}

// SetSettings validates and saves the service settings.
func (d *Daemon) SetSettings(s api.Settings) (api.Settings, error) {
	if s.AutoTestMinutes < 0 || s.AutoTestMinutes > 24*60 {
		return d.Settings(), errors.New("auto test interval must be 0 (off) to 1440 minutes")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if s.KillSwitch && d.ks == nil {
		return api.Settings{AutoTestMinutes: d.st.AutoTestMinutes, AutoConnect: d.st.AutoConnect}, errors.New("kill switch is not supported on this system")
	}
	if s.AutoTestMinutes != d.st.AutoTestMinutes {
		d.lastAutoTest = time.Now() // next run one full interval from now
	}
	d.st.AutoTestMinutes = s.AutoTestMinutes
	d.st.KillSwitch = s.KillSwitch
	d.st.AutoConnect = s.AutoConnect
	switch {
	case !s.KillSwitch:
		d.killSwitchLocked(false)
	case d.conn != nil:
		d.killSwitchLocked(true)
	}
	return api.Settings{AutoTestMinutes: d.st.AutoTestMinutes, KillSwitch: d.st.KillSwitch, AutoConnect: d.st.AutoConnect}, d.store.Save(d.st)
}

// autoTestDue reports whether the periodic latency test should run.
func (d *Daemon) autoTestDue(now time.Time) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	every := time.Duration(d.st.AutoTestMinutes) * time.Minute
	if every <= 0 || now.Sub(d.lastAutoTest) < every {
		return false
	}
	d.lastAutoTest = now
	return true
}

// RunAutoTest measures all servers when the configured interval passes.
func (d *Daemon) RunAutoTest(ctx context.Context) {
	d.mu.Lock()
	d.lastAutoTest = time.Now()
	d.mu.Unlock()
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			if d.autoTestDue(now) {
				if _, err := d.Test(ctx, nil); err != nil {
					log.Printf("auto latency test: %v", err)
				}
			}
		}
	}
}

// AddLinks validates and stores individual share links.
func (d *Daemon) AddLinks(links []string) ([]api.Server, error) {
	var clean []string
	for _, l := range links {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if _, err := link.Parse(l); err != nil {
			return nil, err
		}
		clean = append(clean, l)
	}
	if len(clean) == 0 {
		return nil, errors.New("no links given")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	body, err := d.store.Body(store.ManualID)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSpace(string(body))
	for _, l := range clean {
		if !strings.Contains("\n"+text+"\n", "\n"+l+"\n") {
			text += "\n" + l
		}
	}
	if err := d.setManualLocked(strings.TrimSpace(text)); err != nil {
		return nil, err
	}
	return d.serversLocked(), nil
}

// RemoveServer deletes a manually added link by server key.
func (d *Daemon) RemoveServer(key string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	subID, entryKey, _ := strings.Cut(key, ":")
	if subID != store.ManualID {
		return errors.New("only manually added links can be removed; remove the subscription instead")
	}
	body, err := d.store.Body(store.ManualID)
	if err != nil {
		return err
	}
	var keep []string
	found := false
	for _, l := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if p, err := sub.Parse([]byte(l), nil); err == nil && len(p.Entries) == 1 && p.Entries[0].Key == entryKey {
			found = true
			continue
		}
		keep = append(keep, l)
	}
	if !found {
		return fmt.Errorf("no manual server %q", key)
	}
	return d.setManualLocked(strings.Join(keep, "\n"))
}

func (d *Daemon) setManualLocked(text string) error {
	i := d.subIndexLocked(store.ManualID)
	if text == "" {
		delete(d.parsed, store.ManualID)
		_ = d.store.DeleteBody(store.ManualID)
		if i >= 0 {
			d.st.Subscriptions = append(d.st.Subscriptions[:i], d.st.Subscriptions[i+1:]...)
		}
		return d.store.Save(d.st)
	}
	p, err := sub.Parse([]byte(text), nil)
	if err != nil {
		return err
	}
	if err := d.store.SaveBody(store.ManualID, []byte(text)); err != nil {
		return err
	}
	d.parsed[store.ManualID] = p
	if i < 0 {
		d.st.Subscriptions = append(d.st.Subscriptions, store.Subscription{ID: store.ManualID})
		i = len(d.st.Subscriptions) - 1
	}
	d.st.Subscriptions[i].UpdatedAt = time.Now()
	return d.store.Save(d.st)
}

// Defaults for background refresh.
const (
	updateCheck       = 10 * time.Minute
	defaultInterval   = 24 * time.Hour
	minUpdateInterval = time.Hour
)

// RunUpdater refreshes subscriptions when due (the provider's
// profile-update-interval, at least hourly, 24 h if unknown) until ctx ends.
func (d *Daemon) RunUpdater(ctx context.Context) {
	t := time.NewTicker(updateCheck)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		for _, id := range d.dueSubscriptions(time.Now()) {
			if _, err := d.Update(ctx, id); err != nil {
				log.Printf("background update: %v", err)
				d.mu.Lock()
				d.emitLocked(api.Event{Kind: api.EventUpdateFailed, Sub: d.subNameLocked(id + ":"), Error: err.Error()})
				d.mu.Unlock()
			}
		}
		d.warnExpiring(time.Now())
	}
}

// expiryWarnDays is how early users hear that a subscription runs out.
const expiryWarnDays = 3

// warnExpiring emits one EventExpiring per subscription per day when it
// expires within expiryWarnDays.
func (d *Daemon) warnExpiring(now time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	today := now.Format("2006-01-02")
	for _, sb := range d.st.Subscriptions {
		exp := sb.Info.Expire
		if exp.IsZero() || exp.Sub(now) > expiryWarnDays*24*time.Hour || d.expiryNotified[sb.ID] == today {
			continue
		}
		d.expiryNotified[sb.ID] = today
		d.emitLocked(api.Event{Kind: api.EventExpiring, Sub: sb.Name(), Days: max(int(exp.Sub(now).Hours()/24), 0)})
	}
}

func (d *Daemon) dueSubscriptions(now time.Time) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	var due []string
	for _, sb := range d.st.Subscriptions {
		if sb.ID == store.ManualID {
			continue
		}
		every := sb.Info.UpdateInterval
		if every == 0 {
			every = defaultInterval
		}
		every = max(every, minUpdateInterval)
		if now.Sub(sb.UpdatedAt) >= every {
			due = append(due, sb.ID)
		}
	}
	return due
}

// maskURL hides the token part of a subscription URL.
func maskURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	parts := strings.Split(u.Path, "/")
	if last := parts[len(parts)-1]; len(last) > 6 {
		parts[len(parts)-1] = last[:3] + "…" + last[len(last)-2:]
	}
	return u.Scheme + "://" + u.Host + strings.Join(parts, "/")
}
