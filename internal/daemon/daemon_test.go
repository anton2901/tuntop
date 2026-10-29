// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/store"
	"github.com/anton2901/tuntop/internal/sub"
)

// All values are fake.
const id1 = "00000000-0000-0000-0000-000000000001"

func vless(host, name string) string {
	return "vless://" + id1 + "@" + host + ":443?security=none#" + url.PathEscape(name)
}

type fakeTunnel struct {
	mu       sync.Mutex
	running  int
	started  [][]byte
	fail     error
	sessions []*fakeSession
}

func (f *fakeTunnel) Start(profile []byte) (Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	f.running++
	f.started = append(f.started, profile)
	s := &fakeSession{t: f, done: make(chan struct{})}
	f.sessions = append(f.sessions, s)
	return s, nil
}

// fakeSession moves 1000 bytes up and 5000 down per Traffic call.
type fakeSession struct {
	t        *fakeTunnel
	up, down int64
	done     chan struct{}
}

func (s *fakeSession) Stop() error {
	s.t.mu.Lock()
	s.t.running--
	s.t.mu.Unlock()
	return nil
}

func (s *fakeSession) Done() <-chan struct{} { return s.done }

func (s *fakeSession) Traffic() (int64, int64) {
	s.up += 1000
	s.down += 5000
	return s.up, s.down
}

// fakeProvider serves bodies by URL; blocked URLs fail unless fetched
// through a proxy.
type fakeProvider struct {
	mu      sync.Mutex
	bodies  map[string]string
	blocked map[string]bool
	proxied []string
}

func (p *fakeProvider) fetch(_ context.Context, s store.Subscription, proxy string) (*sub.Subscription, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blocked[s.URL] && proxy == "" {
		return nil, errors.New("TLS handshake timeout")
	}
	if proxy != "" {
		p.proxied = append(p.proxied, s.URL)
	}
	body, ok := p.bodies[s.URL]
	if !ok {
		return nil, errors.New("HTTP 404")
	}
	h := http.Header{}
	h.Set("Profile-Title", "Provider "+s.URL[len(s.URL)-1:])
	h.Set("Profile-Update-Interval", "1")
	parsed, err := sub.Parse([]byte(body), h)
	if parsed != nil {
		parsed.Body = []byte(body)
	}
	return parsed, err
}

// fakeProbe: servers whose profile mentions "de." fail, others take 42 ms.
func fakeProbe(_ context.Context, profile []byte) (time.Duration, error) {
	if strings.Contains(string(profile), "de.example.com") {
		return 0, errors.New("timeout")
	}
	return 42 * time.Millisecond, nil
}

func fakeProxy(_ []byte) (string, func() error, error) {
	return "socks5h://127.0.0.1:1", func() error { return nil }, nil
}

// fakeKS records the kill switch state like the kernel would keep it.
type fakeKS struct {
	mu sync.Mutex
	on bool
}

func (k *fakeKS) On() error  { k.mu.Lock(); k.on = true; k.mu.Unlock(); return nil }
func (k *fakeKS) Off() error { k.mu.Lock(); k.on = false; k.mu.Unlock(); return nil }
func (k *fakeKS) blocking() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.on
}

type env struct {
	ks     *fakeKS
	dir    string
	tun    *fakeTunnel
	prov   *fakeProvider
	d      *Daemon
	client *api.Client
}

func newEnv(t *testing.T) *env {
	t.Helper()
	// unix socket paths are limited to ~104 bytes on macOS; t.TempDir is long.
	dir, err := os.MkdirTemp("/tmp", "tuntop")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	e := &env{
		dir: dir,
		tun: &fakeTunnel{},
		ks:  &fakeKS{},
		prov: &fakeProvider{
			bodies: map[string]string{
				"https://a.example.com/s/a": vless("nl.example.com", "🇳🇱 NL · 🟢10%") + "\n" + vless("de.example.com", "🇩🇪 Germany"),
				"https://b.example.com/s/b": vless("fi.example.com", "🇫🇮 Finland"),
			},
			blocked: map[string]bool{},
		},
	}
	e.start(t)
	return e
}

func (e *env) start(t *testing.T) {
	t.Helper()
	d, err := New(store.Store{Dir: filepath.Join(e.dir, "state")}, Deps{
		Tunnel: e.tun, KillSwitch: e.ks, Fetch: e.prov.fetch, Proxy: fakeProxy, Probe: fakeProbe,
	})
	if err != nil {
		t.Fatal(err)
	}
	sock := filepath.Join(e.dir, "s.sock")
	l, err := ListenUnix(sock, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: d.Handler(), ReadHeaderTimeout: time.Second}
	go func() { _ = srv.Serve(l) }()
	t.Cleanup(func() { _ = srv.Close(); d.Close() })
	e.d, e.client = d, api.NewClient(sock)
}

func TestSubscribeConnectAndPersist(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	s, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"})
	if err != nil {
		t.Fatal(err)
	}
	if s.Servers != 2 || s.Name != "Provider a" {
		t.Fatalf("subscription = %+v", s)
	}
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err == nil {
		t.Fatal("duplicate subscription accepted")
	}
	if _, err := e.client.Connect(ctx, ""); err == nil {
		t.Fatal("connect with nothing selected succeeded")
	}

	st, err := e.client.Connect(ctx, "germ")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Connected || st.Server.Name != "🇩🇪 Germany" || e.tun.running != 1 {
		t.Fatalf("status = %+v, running = %d", st, e.tun.running)
	}
	// Switching keeps exactly one tunnel.
	if st, err = e.client.Connect(ctx, "1"); err != nil || !strings.HasPrefix(st.Server.Name, "🇳🇱") || e.tun.running != 1 {
		t.Fatalf("switch: %+v, %v, running %d", st, err, e.tun.running)
	}
	if st, _ = e.client.Disconnect(ctx); st.Connected || e.tun.running != 0 || st.Selected == nil {
		t.Fatalf("disconnect: %+v, running %d", st, e.tun.running)
	}

	// A new daemon on the same state reconnects to the selected server
	// from the cached body, without downloading.
	e.prov.bodies = nil
	e.start(t)
	if st, err = e.client.Connect(ctx, ""); err != nil || !strings.HasPrefix(st.Server.Name, "🇳🇱") {
		t.Fatalf("after restart: %+v, %v", st, err)
	}
}

func TestSelectionSurvivesReorderAndRename(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	// Provider re-sorts and changes the load suffix.
	e.prov.bodies["https://a.example.com/s/a"] = vless("de.example.com", "🇩🇪 Germany") + "\n" + vless("nl.example.com", "🇳🇱 NL · 🔴90%")
	if _, err := e.client.Update(ctx, ""); err != nil {
		t.Fatal(err)
	}
	_, _ = e.client.Disconnect(ctx)
	st, err := e.client.Connect(ctx, "")
	if err != nil || st.Server.Name != "🇳🇱 NL · 🔴90%" || st.Server.Index != 2 {
		t.Fatalf("reconnect after re-sort: %+v, %v", st, err)
	}
}

func TestNoProxyRetryForStubSubscription(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	e.prov.bodies["https://c.example.com/s/c"] = "vless://" + id1 + "@stub.example.com:1?security=none#" + url.PathEscape("Включите отправку HWID")
	_, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://c.example.com/s/c"})
	if err == nil || !strings.Contains(err.Error(), "HWID") || strings.Contains(err.Error(), "other servers") {
		t.Fatalf("err = %v", err)
	}
	if len(e.prov.proxied) != 0 {
		t.Fatalf("retried through a proxy: %v", e.prov.proxied)
	}
}

func TestBlockedSubscriptionViaOtherServers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	e.prov.blocked["https://b.example.com/s/b"] = true

	// Nothing to proxy through yet.
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://b.example.com/s/b"}); err == nil {
		t.Fatal("blocked subscription added without any proxy")
	}
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	s, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://b.example.com/s/b"})
	if err != nil || s.Servers != 1 {
		t.Fatalf("blocked subscription: %+v, %v", s, err)
	}
	if len(e.prov.proxied) != 1 {
		t.Fatalf("proxied downloads = %v", e.prov.proxied)
	}
	servers, _ := e.client.Servers(ctx)
	if len(servers) != 3 || servers[2].Name != "🇫🇮 Finland" {
		t.Fatalf("servers = %+v", servers)
	}
}

func TestConnectFailureReported(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	e.tun.fail = errors.New("interface tuntop0 did not appear")
	if _, err := e.client.Connect(ctx, "1"); err == nil || !strings.Contains(err.Error(), "tuntop0") {
		t.Fatalf("err = %v", err)
	}
	st, _ := e.client.Status(ctx)
	if st.Connected || st.LastError == "" {
		t.Fatalf("status = %+v", st)
	}
}

func TestManualLinks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddLinks(ctx, []string{"not a link"}); err == nil {
		t.Fatal("garbage accepted")
	}
	servers, err := e.client.AddLinks(ctx, []string{vless("x.example.com", "Mine"), vless("y.example.com", "Mine 2")})
	if err != nil || len(servers) != 2 || servers[0].SubID != store.ManualID {
		t.Fatalf("servers = %+v, %v", servers, err)
	}
	if err := e.client.RemoveServer(ctx, servers[0].Key); err != nil {
		t.Fatal(err)
	}
	if servers, _ = e.client.Servers(ctx); len(servers) != 1 || servers[0].Name != "Mine 2" {
		t.Fatalf("after remove: %+v", servers)
	}
	if err := e.client.RemoveServer(ctx, servers[0].Key); err != nil {
		t.Fatal(err)
	}
	if subs, _ := e.client.Subscriptions(ctx); len(subs) != 0 {
		t.Fatalf("empty manual list not dropped: %+v", subs)
	}
}

func TestRemoveSubscriptionClearsSelection(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s, _ := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"})
	_, _ = e.client.Connect(ctx, "1")
	_, _ = e.client.Disconnect(ctx)
	if err := e.client.RemoveSubscription(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	st, _ := e.client.Status(ctx)
	if st.Selected != nil {
		t.Fatalf("selection kept: %+v", st.Selected)
	}
}

func TestDueSubscriptions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s, _ := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"})
	now := time.Now()
	if due := e.d.dueSubscriptions(now); len(due) != 0 {
		t.Fatalf("fresh subscription due: %v", due)
	}
	if due := e.d.dueSubscriptions(now.Add(61 * time.Minute)); len(due) != 1 || due[0] != s.ID {
		t.Fatalf("hourly subscription not due: %v", due)
	}
}

func TestMaskURL(t *testing.T) {
	if got := maskURL("https://sub.example.com/api/sub/AbCdEfGhIjKlMnOp"); got != "https://sub.example.com/api/sub/AbC…Op" {
		t.Fatal(got)
	}
}

func TestLatency(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	servers, _ := e.client.Servers(ctx)
	if servers[0].Latency != 0 {
		t.Fatalf("latency before test = %d", servers[0].Latency)
	}
	// One server only.
	servers, err := e.client.Test(ctx, []string{servers[0].Key})
	if err != nil || servers[0].Latency != 42 || servers[1].Latency != 0 {
		t.Fatalf("single test: %+v, %v", servers, err)
	}
	servers, err = e.client.Test(ctx, nil)
	if err != nil || servers[0].Latency != 42 || servers[1].Latency != -1 {
		t.Fatalf("all: %+v, %v", servers, err)
	}
}

func TestLogRing(t *testing.T) {
	r := NewLogRing(3)
	for _, l := range []string{"a\n", "b\nc\n", "d\n"} {
		_, _ = r.Write([]byte(l))
	}
	got := r.Lines()
	if len(got) != 3 || !strings.HasSuffix(got[0], " b") || !strings.HasSuffix(got[2], " d") {
		t.Fatalf("lines = %q", got)
	}
	var nilRing *LogRing
	if nilRing.Lines() == nil {
		t.Fatal("nil ring must return an empty list, not null")
	}
}

func TestTrafficSampling(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	e.d.sampleTraffic() // disconnected: zero sample
	if _, err := e.client.Connect(ctx, "1"); err != nil {
		t.Fatal(err)
	}
	e.d.sampleTraffic()
	e.d.sampleTraffic()
	tr, err := e.client.Traffic(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Connected || tr.DownRate != 5000 || tr.UpRate != 1000 || tr.DownTotal != 10000 ||
		len(tr.DownHist) != 3 || tr.DownHist[0] != 0 {
		t.Fatalf("traffic = %+v", tr)
	}
}

func TestSettingsAndAutoTest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.SetSettings(ctx, api.Settings{AutoTestMinutes: -1}); err == nil {
		t.Fatal("negative interval accepted")
	}
	if e.d.autoTestDue(time.Now().Add(time.Hour)) {
		t.Fatal("auto test due while disabled")
	}
	s, err := e.client.SetSettings(ctx, api.Settings{AutoTestMinutes: 10})
	if err != nil || s.AutoTestMinutes != 10 {
		t.Fatalf("set: %+v, %v", s, err)
	}
	now := time.Now()
	if e.d.autoTestDue(now.Add(5*time.Minute)) || !e.d.autoTestDue(now.Add(11*time.Minute)) {
		t.Fatal("auto test schedule wrong")
	}
	// Persisted.
	e.start(t)
	if s, _ = e.client.Settings(ctx); s.AutoTestMinutes != 10 {
		t.Fatalf("after restart: %+v", s)
	}
}

func TestLatencyHistory(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < latencyKeep+3; i++ {
		if _, err := e.client.Test(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	servers, _ := e.client.Servers(ctx)
	if h := servers[0].LatencyHistory; len(h) != latencyKeep || h[0] != 42 {
		t.Fatalf("history = %v", h)
	}
	if servers[0].Address != "nl.example.com:443" {
		t.Fatalf("address = %q", servers[0].Address)
	}
}

func TestEvents(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	got := make(chan api.Event, 10)
	go func() { _ = e.client.Events(ctx, func(ev api.Event) { got <- ev }) }()
	// Wait until the stream is subscribed.
	for i := 0; ; i++ {
		e.d.mu.Lock()
		n := len(e.d.listeners)
		e.d.mu.Unlock()
		if n > 0 {
			break
		}
		if i > 200 {
			t.Fatal("event stream did not subscribe")
		}
		time.Sleep(5 * time.Millisecond)
	}
	_, _ = e.client.Connect(ctx, "1")
	_, _ = e.client.Disconnect(ctx)
	e.tun.fail = errors.New("boom")
	_, _ = e.client.Connect(ctx, "1")

	want := []string{api.EventConnected, api.EventDisconnected, api.EventConnectFailed}
	for _, k := range want {
		select {
		case ev := <-got:
			if ev.Kind != k {
				t.Fatalf("event %q, want %q", ev.Kind, k)
			}
			if k == api.EventConnected && (ev.Server == "" || ev.Sub != "Provider a") {
				t.Fatalf("connected event = %+v", ev)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("no %q event", k)
		}
	}
}

func TestExpiryWarning(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s, _ := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"})
	ch, stop := e.d.Subscribe()
	defer stop()
	now := time.Now()
	e.d.mu.Lock()
	e.d.st.Subscriptions[e.d.subIndexLocked(s.ID)].Info.Expire = now.Add(50 * time.Hour)
	e.d.mu.Unlock()
	e.d.warnExpiring(now)
	e.d.warnExpiring(now) // same day: once
	select {
	case ev := <-ch:
		if ev.Kind != api.EventExpiring || ev.Days != 2 {
			t.Fatalf("event = %+v", ev)
		}
	default:
		t.Fatal("no expiry event")
	}
	select {
	case ev := <-ch:
		t.Fatalf("second warning the same day: %+v", ev)
	default:
	}
}

func TestReconnectOnNetworkChange(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.Connect(ctx, "2"); err != nil {
		t.Fatal(err)
	}
	e.tun.mu.Lock()
	first := e.tun.sessions[0]
	e.tun.mu.Unlock()
	close(first.done) // Wi-Fi → cable
	for i := 0; ; i++ {
		e.tun.mu.Lock()
		n, running := len(e.tun.sessions), e.tun.running
		e.tun.mu.Unlock()
		if n == 2 && running == 1 {
			break
		}
		if i > 200 {
			t.Fatalf("no reconnect: sessions %d running %d", n, running)
		}
		time.Sleep(5 * time.Millisecond)
	}
	st, _ := e.client.Status(ctx)
	if !st.Connected || st.Server.Index != 2 {
		t.Fatalf("after reconnect: %+v", st)
	}
}
