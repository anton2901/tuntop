// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/i18n"
)

type fakeClient struct {
	servers   []api.Server
	status    api.Status
	connected []string
	tested    [][]string
	events    []api.Event
	moved     []string
	svc       api.Settings
}

func (f *fakeClient) Settings(context.Context) (api.Settings, error) { return f.svc, nil }
func (f *fakeClient) SetSettings(_ context.Context, s api.Settings) (api.Settings, error) {
	f.svc = s
	return s, nil
}

func (f *fakeClient) Status(context.Context) (api.Status, error) { return f.status, nil }

func (f *fakeClient) Traffic(context.Context) (api.Traffic, error) {
	if !f.status.Connected {
		return api.Traffic{}, nil
	}
	return api.Traffic{Connected: true, DownTotal: 3 << 20, DownRate: 512 << 10, UpTotal: 200 << 10, UpRate: 1024}, nil
}

func (f *fakeClient) Servers(context.Context) ([]api.Server, error) {
	return f.servers, nil
}

func (f *fakeClient) Connect(_ context.Context, s string) (api.Status, error) {
	f.connected = append(f.connected, s)
	f.status = api.Status{Connected: true, Server: &f.servers[0], Since: time.Now()}
	return f.status, nil
}

func (f *fakeClient) Disconnect(context.Context) (api.Status, error) {
	f.status = api.Status{}
	return f.status, nil
}

func (f *fakeClient) Subscriptions(context.Context) ([]api.Subscription, error) {
	return []api.Subscription{{ID: "a", Name: "Provider", Servers: 2, Download: 1 << 30, Expire: time.Now().Add(72 * time.Hour)}}, nil
}

func (f *fakeClient) AddSubscription(context.Context, api.AddSubscriptionRequest) (api.Subscription, error) {
	return api.Subscription{Name: "New", Servers: 3}, nil
}
func (f *fakeClient) RemoveSubscription(context.Context, string) error { return nil }
func (f *fakeClient) Update(context.Context, string) ([]api.Subscription, error) {
	return nil, nil
}

func (f *fakeClient) SetFavorite(_ context.Context, q string, on bool) (api.Server, error) {
	for i := range f.servers {
		if strings.Contains(f.servers[i].Name, q) {
			f.servers[i].Favorite = on
			return f.servers[i], nil
		}
	}
	return api.Server{}, errors.New("no server")
}

func (f *fakeClient) MoveSubscription(_ context.Context, id string, to int) ([]api.Subscription, error) {
	f.moved = append(f.moved, fmt.Sprintf("%s→%d", id, to))
	return nil, nil
}

func (f *fakeClient) AddLinks(context.Context, []string) ([]api.Server, error) { return nil, nil }
func (f *fakeClient) RemoveServer(context.Context, string) error               { return nil }

func (f *fakeClient) Test(_ context.Context, keys []string) ([]api.Server, error) {
	f.tested = append(f.tested, keys)
	return f.servers, nil
}

func (f *fakeClient) Events(ctx context.Context, fn func(api.Event)) error {
	for _, e := range f.events {
		fn(e)
	}
	f.events = nil
	<-ctx.Done()
	return ctx.Err()
}

func newFake() *fakeClient {
	return &fakeClient{servers: []api.Server{
		{Index: 1, Key: "a:1", Name: "🇳🇱 Netherlands", Proto: "vless reality tcp", SubName: "Provider", Latency: 120},
		{Index: 2, Key: "a:2", Name: "🇩🇪 Germany", Group: true, Proxies: 3, SubName: "Provider", Latency: -1},
	}}
}

func run(t *testing.T, f *fakeClient, lang i18n.Lang, args ...string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	c := &CLI{C: f, T: i18n.T{Lang: lang}, In: strings.NewReader(""), Out: &out, Err: &out}
	code := c.Run(context.Background(), args)
	return out.String(), code
}

func TestAutoConnectCommand(t *testing.T) {
	f := newFake()
	f.svc.KillSwitch = true
	if out, code := run(t, f, i18n.RU, "autoconnect", "on"); code != 0 || !f.svc.AutoConnect || !f.svc.KillSwitch || !strings.Contains(out, "включено") {
		t.Fatalf("autoconnect on: %d %q %+v", code, out, f.svc)
	}
	if _, code := run(t, f, i18n.EN, "autoconnect", "later"); code != 1 {
		t.Fatal("bad argument accepted")
	}
	c := &CLI{T: i18n.T{Lang: i18n.RU}}
	if title, body, urgent := c.describe(api.Event{Kind: api.EventUnreachable, Server: "NL", Error: "timeout"}); title != "Сервер VPN не отвечает" || body != "NL: timeout" || !urgent {
		t.Fatalf("unreachable: %q %q %v", title, body, urgent)
	}
	if title, _, _ := c.describe(api.Event{Kind: api.EventRecovered, Server: "NL"}); title != "VPN снова работает" {
		t.Fatalf("recovered: %q", title)
	}
}

func TestKillSwitchCommand(t *testing.T) {
	f := newFake()
	f.svc.AutoTestMinutes = 30
	if out, code := run(t, f, i18n.RU, "killswitch", "on"); code != 0 || !f.svc.KillSwitch || f.svc.AutoTestMinutes != 30 || !strings.Contains(out, "включена") {
		t.Fatalf("killswitch on: %d %q %+v", code, out, f.svc)
	}
	if _, code := run(t, f, i18n.EN, "killswitch", "maybe"); code != 1 {
		t.Fatal("bad argument accepted")
	}
	f.status = api.Status{KillSwitch: true}
	if out, _ := run(t, f, i18n.RU, "status"); !strings.Contains(out, "БЛОКИРОВКА") {
		t.Fatalf("blocked status:\n%s", out)
	}
	f.status = api.Status{Connected: true, Server: &f.servers[0], Since: time.Now(), KillSwitch: true}
	if out, _ := run(t, f, i18n.EN, "status"); !strings.Contains(out, "kill:    on") {
		t.Fatalf("connected status:\n%s", out)
	}
}

func TestFavoritesAndMove(t *testing.T) {
	f := newFake()
	if out, code := run(t, f, i18n.RU, "fav", "Germany"); code != 0 || !strings.Contains(out, "в избранном") {
		t.Fatalf("fav: %d %q", code, out)
	}
	f.servers = []api.Server{f.servers[1], f.servers[0]}
	out, _ := run(t, f, i18n.RU, "servers")
	if !strings.Contains(out, "★ Избранное\n") || !strings.Contains(out, "🇩🇪 Germany  (Provider)") {
		t.Fatalf("servers with a favorite:\n%s", out)
	}
	if _, code := run(t, f, i18n.EN, "fav"); code != 1 {
		t.Fatal("fav without a server succeeded")
	}
	if _, code := run(t, f, i18n.EN, "sub", "mv", "1", "2"); code != 0 || len(f.moved) != 1 || f.moved[0] != "a→2" {
		t.Fatalf("sub mv: %d %v", code, f.moved)
	}
	if _, code := run(t, f, i18n.EN, "sub", "mv", "1", "top"); code != 1 {
		t.Fatal("sub mv with a bad position succeeded")
	}
}

func TestCommands(t *testing.T) {
	f := newFake()
	if out, code := run(t, f, i18n.RU, "status"); code != 0 || !strings.Contains(out, "выключен") {
		t.Fatalf("status: %d %q", code, out)
	}
	if out, code := run(t, f, i18n.RU, "on", "germ"); code != 0 || f.connected[0] != "germ" || !strings.Contains(out, "VPN включён") {
		t.Fatalf("on: %d %q %v", code, out, f.connected)
	}
	if out, _ := run(t, f, i18n.RU, "status"); !strings.Contains(out, "трафик:  ↓ 3.0 МБ (512 КБ/с)  ↑ 200 КБ (1.0 КБ/с)") {
		t.Fatalf("status while connected:\n%s", out)
	}
	if out, _ := run(t, f, i18n.EN, "servers"); !strings.Contains(out, "→   1. 🇳🇱 Netherlands") || !strings.Contains(out, "group of 3") {
		t.Fatalf("servers:\n%s", out)
	}
	if out, code := run(t, f, i18n.RU, "test", "2"); code != 0 || f.tested[0][0] != "a:2" || !strings.Contains(out, "120 мс") {
		t.Fatalf("test: %d %q %v", code, out, f.tested)
	}
	if _, code := run(t, f, i18n.EN, "test", "nothing-like-this"); code != 1 {
		t.Fatal("test with unknown server succeeded")
	}
	if out, _ := run(t, f, i18n.RU, "sub"); !strings.Contains(out, "серверов: 2") || !strings.Contains(out, "без лимита") {
		t.Fatalf("sub:\n%s", out)
	}
	if out, code := run(t, f, i18n.RU, "bogus"); code != 2 || !strings.Contains(out, "использование") {
		t.Fatalf("unknown: %d %q", code, out)
	}
}

func TestNotify(t *testing.T) {
	f := newFake()
	f.events = []api.Event{
		{Kind: api.EventConnected, Server: "🇳🇱 NL", Sub: "Provider"},
		{Kind: api.EventConnectFailed, Server: "🇩🇪 DE", Error: "boom"},
		{Kind: "something_new"},
		{Kind: api.EventExpiring, Sub: "Provider", Days: 2},
	}
	type note struct {
		title, body string
		urgent      bool
	}
	var got []note
	Notify = func(title, body string, urgent bool) error {
		got = append(got, note{title, body, urgent})
		return nil
	}
	defer func() { Notify = nil }()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	c := &CLI{C: f, T: i18n.T{Lang: i18n.RU}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	if code := c.Run(ctx, []string{"notify"}); code != 0 {
		t.Fatalf("notify exit %d", code)
	}
	if len(got) != 3 || got[0].title != "VPN включён" || got[0].body != "🇳🇱 NL · Provider" ||
		!got[1].urgent || !strings.Contains(got[2].body, "осталось дней: 2") {
		t.Fatalf("notifications = %+v", got)
	}
}

// Every message passed through the translator must have a Russian one.
func TestRussianCoverage(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	res := []*regexp.Regexp{
		regexp.MustCompile(`\b(?:t|T)\.[SF]\("((?:[^"\\]|\\.)+)"`),
		regexp.MustCompile("\\b(?:t|T)\\.[SF]\\(`([^`]+)`"),
		regexp.MustCompile(`c\.printf\("((?:[^"\\]|\\.)+)"`),
	}
	missing := []string{}
	if !i18n.Translated(i18n.RU, usage) {
		missing = append(missing, "usage")
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, re := range res {
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				msg := m[1]
				if i != 1 {
					if u, err := strconv.Unquote(`"` + msg + `"`); err == nil {
						msg = u
					}
				}
				if !i18n.Translated(i18n.RU, msg) {
					missing = append(missing, msg)
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing Russian translations: %q", missing)
	}
}

func TestAppleString(t *testing.T) {
	if got := appleString(`say "hi" \ bye`); got != `"say \"hi\" \\ bye"` {
		t.Fatal(got)
	}
}
