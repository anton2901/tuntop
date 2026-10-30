// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/i18n"
	"github.com/anton2901/tuntop/internal/paths"
	"github.com/anton2901/tuntop/internal/version"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type fakeClient struct {
	mu        sync.Mutex
	status    api.Status
	servers   []api.Server
	subs      []api.Subscription
	connected []string
	added     []string
	removed   []string
	tested    [][]string
	svc       api.Settings
	fail      error
	moved     []string
	addErr    error
}

func (f *fakeClient) Status(context.Context) (api.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.fail
}

func (f *fakeClient) Servers(context.Context) ([]api.Server, error) { return f.servers, f.fail }

func (f *fakeClient) Subscriptions(context.Context) ([]api.Subscription, error) {
	return f.subs, f.fail
}

func (f *fakeClient) Connect(_ context.Context, s string) (api.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.connected = append(f.connected, s)
	for i := range f.servers {
		if f.servers[i].Key == s || s == "" {
			f.status = api.Status{Connected: true, Server: &f.servers[i], Since: time.Now()}
			break
		}
	}
	return f.status, nil
}

func (f *fakeClient) Disconnect(context.Context) (api.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = api.Status{}
	return f.status, nil
}

func (f *fakeClient) AddSubscription(_ context.Context, r api.AddSubscriptionRequest) (api.Subscription, error) {
	f.added = append(f.added, r.URL)
	if f.addErr != nil {
		return api.Subscription{}, f.addErr
	}
	return api.Subscription{Name: "New", Servers: 3}, nil
}

func (f *fakeClient) RemoveSubscription(_ context.Context, id string) error {
	f.removed = append(f.removed, id)
	return nil
}

func (f *fakeClient) Update(context.Context, string) ([]api.Subscription, error) { return f.subs, nil }

func (f *fakeClient) Log(context.Context) ([]string, error) {
	return []string{"12:00:01 tuntopd starting", "12:00:05 connected: NL", "12:01:00 background update: error x"}, nil
}

func (f *fakeClient) Traffic(context.Context) (api.Traffic, error) {
	return api.Traffic{
		Connected: true, DownRate: 12 << 20, UpRate: 800 << 10, DownTotal: 1 << 30, UpTotal: 96 << 20,
		DownHist: []int64{0, 1 << 20, 4 << 20, 12 << 20}, UpHist: []int64{0, 100 << 10, 300 << 10, 800 << 10},
	}, nil
}

func (f *fakeClient) Settings(context.Context) (api.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.svc, nil
}

func (f *fakeClient) SetSettings(_ context.Context, s api.Settings) (api.Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.svc = s
	return s, nil
}

func (f *fakeClient) Test(_ context.Context, keys []string) ([]api.Server, error) {
	f.tested = append(f.tested, keys)
	return f.servers, nil
}

func (f *fakeClient) SetFavorite(_ context.Context, key string, on bool) (api.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var fav, rest []api.Server
	var found api.Server
	for _, s := range f.servers {
		if s.Key == key {
			s.Favorite = on
			found = s
		}
		if s.Favorite {
			fav = append(fav, s)
		} else {
			rest = append(rest, s)
		}
	}
	fav = append(fav, rest...)
	f.servers = fav
	for i := range f.servers {
		f.servers[i].Index = i + 1
	}
	return found, nil
}

func (f *fakeClient) MoveSubscription(_ context.Context, id string, to int) ([]api.Subscription, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.moved = append(f.moved, fmt.Sprintf("%s→%d", id, to))
	return f.subs, nil
}

func newFake() *fakeClient {
	return &fakeClient{
		servers: []api.Server{
			{Index: 1, Key: "a:1", Name: "🇳🇱 Netherlands", Proto: "vless reality tcp", SubID: "a", SubName: "Provider A", Latency: 120, LatencyHistory: []int{90, 300, -1, 1200, 120}, Address: "nl.example.com:443"},
			{Index: 2, Key: "a:2", Name: "🇩🇪 Germany", Proto: "vless reality tcp", SubID: "a", SubName: "Provider A", Latency: -1},
			{Index: 3, Key: "b:1", Name: "✨ Smart Best", Group: true, Proxies: 19, SubID: "b", SubName: "Provider B"},
		},
		subs: []api.Subscription{
			{ID: "a", Name: "Provider A", Servers: 2, URL: "https://a.example.com/s/abc…yz"},
			{ID: "b", Name: "Provider B", Servers: 1, Expire: time.Now().Add(48 * time.Hour)},
		},
	}
}

// run feeds msg to the model and executes resulting commands until none
// are left (ticks and spinners excluded), like the Bubble Tea loop would.
func run(t *testing.T, m Model, msgs ...tea.Msg) Model {
	t.Helper()
	queue := msgs
	for len(queue) > 0 {
		msg := queue[0]
		queue = queue[1:]
		next, cmd := m.Update(msg)
		m = next.(Model)
		if cmd == nil {
			continue
		}
		out := cmd()
		switch out := out.(type) {
		case tea.BatchMsg:
			for _, c := range out {
				if c != nil {
					if r := c(); !isLoop(r) {
						queue = append(queue, r)
					}
				}
			}
		default:
			if !isLoop(out) && out != nil {
				queue = append(queue, out)
			}
		}
	}
	return m
}

// isLoop reports background messages (refresh ticks, spinner, cursor
// blink) that would otherwise keep the loop running forever.
func isLoop(msg tea.Msg) bool {
	switch msg.(type) {
	case nil, tickMsg, spinner.TickMsg:
		return true
	}
	return strings.Contains(strings.ToLower(fmt.Sprintf("%T", msg)), "blink")
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func loaded(t *testing.T, f *fakeClient) Model {
	t.Helper()
	m := New(f, i18n.EN, Config{})
	return run(t, m, tea.WindowSizeMsg{Width: 100, Height: 30}, m.refresh()())
}

func TestFavoriteMovesToTop(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("down"), key("down"), key("*"))
	if !f.servers[0].Favorite || f.servers[0].Key != "b:1" {
		t.Fatalf("servers = %+v", f.servers)
	}
	if vis := m.visibleServers(); m.cursor != 0 || vis[0].Key != "b:1" {
		t.Fatalf("cursor did not follow the server: %d", m.cursor)
	}
	v := m.View()
	if !strings.Contains(v, "★ Favorites") || !strings.Contains(v, "Smart Best · Provider B") {
		t.Fatalf("no favorites section:\n%s", v)
	}
	m = run(t, m, key("*"))
	if f.servers[0].Favorite || strings.Contains(m.View(), "★ Favorites") {
		t.Fatal("favorite not removed")
	}
}

func TestKillSwitchSettingAndBlockedStatus(t *testing.T) {
	f := newFake()
	f.svc.AutoTestMinutes = 60
	m := loaded(t, f)
	m.overlay = ovSettings
	for i, s := range m.settings() {
		if s.label == "Kill switch" {
			m.setSel = i
		}
	}
	m = run(t, m, key("enter"))
	if !f.svc.KillSwitch || f.svc.AutoTestMinutes != 60 {
		t.Fatalf("settings sent: %+v", f.svc)
	}
	f.status = api.Status{KillSwitch: true}
	m.overlay = ovNone
	m = run(t, m, m.refresh()())
	if v := m.View(); !strings.Contains(v, "BLOCKED") {
		t.Fatalf("no blocked state:\n%s", v)
	}
	// o unblocks (off), not reconnect.
	m = run(t, m, key("o"))
	if len(f.connected) != 0 || f.status.KillSwitch {
		t.Fatalf("o while blocked: connected %v, status %+v", f.connected, f.status)
	}
}

func TestAddSubscriptionFailureShown(t *testing.T) {
	f := newFake()
	f.addErr = errors.New("provider says «Превышен лимит устройств»: subscription has no usable servers")
	m := loaded(t, f)
	m = run(t, m, key("4"), key("a"), tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://x.example.com/s/1")}, key("enter"))
	// At 100 columns the provider's words are still on screen.
	if !m.msgErr || !strings.HasPrefix(m.msg, "Subscription not added: ") || !strings.Contains(m.View(), "Превышен лимит устройств") {
		t.Fatalf("msg %q\n%s", m.msg, m.View())
	}
}

func TestLongErrorIsCutNotLost(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m.setMsg("Subscription not added: provider says «Приложение не поддерживается», «Или включите «отправить HWID»»: subscription has no usable servers", true)
	v := m.View()
	if !strings.Contains(v, "Subscription not added: provider says") || !strings.Contains(v, "…") {
		t.Fatalf("error not shown:\n%s", v)
	}
	for _, l := range strings.Split(v, "\n") {
		if w := lipgloss.Width(l); w > m.width {
			t.Fatalf("line wider than the screen (%d): %q", w, l)
		}
	}
}

func TestRussianLayoutKeys(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	// "е" is the t key: measure latency.
	m = run(t, m, key("е"))
	if len(f.tested) != 1 {
		t.Fatalf("е did not start a latency test: %v", f.tested)
	}
	// "," is the "?" key: help.
	m = run(t, m, key(","))
	if m.overlay != ovHelp {
		t.Fatalf("overlay %v after ,", m.overlay)
	}
	m = run(t, m, key("x"))
	// "4", then "ф" (a): the add window; typed text stays Cyrillic.
	m = run(t, m, key("4"), key("ф"))
	if m.overlay != ovAddSub {
		t.Fatalf("ф did not open the add window: %v", m.overlay)
	}
	m = run(t, m, key("ж"))
	if m.input.Value() != "ж" {
		t.Fatalf("input = %q", m.input.Value())
	}
	m = run(t, m, key("esc"))
	// The filter keeps Cyrillic too.
	m = run(t, m, key("3"), key("."), key("Г"))
	if !m.filtering || m.filter.Value() != "Г" {
		t.Fatalf("filtering %v, filter %q", m.filtering, m.filter.Value())
	}
}

func TestAboutWindow(t *testing.T) {
	f := newFake()
	f.status = api.Status{Version: "9.9.9", Build: "9.9.9 (commit abc, built today)", Xray: "v1.260327.0"}
	m := loaded(t, f)
	m = run(t, m, key("esc"))
	for i, l := range m.menuLabels() {
		if l == "About" {
			m.menuSel = i
		}
	}
	m = run(t, m, key("enter"))
	if m.overlay != ovAbout {
		t.Fatalf("overlay = %v", m.overlay)
	}
	v := m.View()
	for _, want := range []string{"tuntop " + version.Version, "9.9.9 (commit abc, built today)", "v1.260327.0", "GPL-3.0-or-later", "github.com/anton2901/tuntop", "differ"} {
		if !strings.Contains(v, want) {
			t.Errorf("no %q in:\n%s", want, v)
		}
	}
	m = run(t, m, key("x"))
	if m.overlay != ovNone {
		t.Fatal("any key does not close it")
	}
}

func TestMoveSubscription(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("4"), key("K")) // first one: cannot go up
	if len(f.moved) != 0 {
		t.Fatalf("moved past the top: %v", f.moved)
	}
	m = run(t, m, key("J"))
	if len(f.moved) != 1 || f.moved[0] != "a→2" || m.subCursor != 1 {
		t.Fatalf("moved = %v, cursor %d", f.moved, m.subCursor)
	}
}

func TestConnectServerUnderCursor(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("down"), key("enter"))
	if len(f.connected) != 1 || f.connected[0] != "a:2" {
		t.Fatalf("connected = %v", f.connected)
	}
	if !m.status.Connected || !strings.Contains(m.View(), "ON") || !strings.Contains(m.msg, "Germany") {
		t.Fatalf("after connect: msg %q\n%s", m.msg, m.View())
	}
	// o toggles off.
	m = run(t, m, key("o"))
	if m.status.Connected {
		t.Fatal("o did not disconnect")
	}
}

func TestFilter(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("/"), key("s"), key("m"), key("a"), key("enter"))
	if vis := m.visibleServers(); len(vis) != 1 || vis[0].Key != "b:1" {
		t.Fatalf("filtered = %+v", vis)
	}
	m = run(t, m, key("enter"))
	if len(f.connected) != 1 || f.connected[0] != "b:1" {
		t.Fatalf("connected = %v", f.connected)
	}
	m = run(t, m, key("esc"))
	if len(m.visibleServers()) != 3 {
		t.Fatal("esc did not clear the filter")
	}
	// Typed fast or pasted: one message with several runes.
	m = run(t, m, key("/germ"))
	if vis := m.visibleServers(); len(vis) != 1 || vis[0].Key != "a:2" || !m.filtering {
		t.Fatalf("batched filter = %+v, filtering %v", vis, m.filtering)
	}
}

func TestLatencyKeys(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("t"))
	m = run(t, m, key("T"))
	if len(f.tested) != 2 || f.tested[0] != nil || len(f.tested[1]) != 1 || f.tested[1][0] != "a:1" {
		t.Fatalf("tested = %v", f.tested)
	}
	v := m.View()
	if !strings.Contains(v, "120 ms") || !strings.Contains(v, "✗") {
		t.Fatalf("latency not shown:\n%s", v)
	}
}

func TestSubscriptionsAddAndRemove(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("tab"), key("a"))
	for _, r := range "https://sub.example.com/x" {
		m = run(t, m, key(string(r)))
	}
	if strings.Contains(m.View(), "sub.example.com") {
		t.Fatal("subscription URL is visible while typing")
	}
	m = run(t, m, key("enter"))
	if len(f.added) != 1 || f.added[0] != "https://sub.example.com/x" || !strings.Contains(m.msg, "New") {
		t.Fatalf("added = %v, msg %q", f.added, m.msg)
	}
	m = run(t, m, key("down"), key("x"))
	if !strings.Contains(m.View(), "Remove subscription Provider B?") || m.overlay != ovConfirm {
		t.Fatalf("no confirmation:\n%s", m.View())
	}
	m = run(t, m, key("n"))
	if len(f.removed) != 0 {
		t.Fatal("removed without confirmation")
	}
	_ = run(t, m, key("x"), key("y"))
	if len(f.removed) != 1 || f.removed[0] != "b" {
		t.Fatalf("removed = %v", f.removed)
	}
}

func TestDaemonDown(t *testing.T) {
	f := newFake()
	f.fail = errors.New("tuntopd is not running")
	m := New(f, i18n.RU, Config{})
	m = run(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}, m.refresh()())
	v := m.View()
	if !strings.Contains(v, "недоступна") || !strings.Contains(v, paths.StartHint) {
		t.Fatalf("view:\n%s", v)
	}
}

func TestTinyTerminalDoesNotPanic(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	for _, sz := range [][2]int{{1, 1}, {10, 3}, {20, 5}, {30, 8}} {
		m = run(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		for _, tb := range []tab{tabServers, tabSubs, tabLog} {
			m.tab = tb
			_ = m.View()
		}
	}
}

// Every line must be exactly as wide as the terminal, or the screen tears.
func TestExactGeometry(t *testing.T) {
	f := newFake()
	f.status = api.Status{Connected: true, Server: &f.servers[0], Since: time.Now(), Selected: &f.servers[0]}
	for _, lang := range []i18n.Lang{i18n.EN, i18n.RU} {
		m := New(f, lang, Config{})
		m = run(t, m, m.refresh()())
		var sizes [][2]int
		for w := 20; w <= 120; w++ {
			for _, h := range []int{15, 24, 40} {
				sizes = append(sizes, [2]int{w, h})
			}
		}
		sizes = append(sizes, [2]int{60, 20}, [2]int{100, 15})
		for _, sz := range sizes {
			m = run(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			for _, ov := range []overlayKind{ovNone, ovMenu, ovSettings, ovHelp, ovInfo, ovAddSub, ovConfirm} {
				tabs := []tab{tabServers}
				if ov == ovNone { // overlays do not depend on the tab
					tabs = []tab{tabServers, tabSubs, tabLog}
				}
				for _, tb := range tabs {
					m.tab, m.overlay = tb, ov
					lines := strings.Split(m.View(), "\n")
					if len(lines) != sz[1] {
						t.Fatalf("%v %dx%d tab %d overlay %d: %d lines", lang, sz[0], sz[1], tb, ov, len(lines))
					}
					for i, l := range lines {
						if w := lipgloss.Width(l); w != sz[0] {
							t.Fatalf("%v %dx%d tab %d overlay %d line %d: width %d\n%s", lang, sz[0], sz[1], tb, ov, i, w, l)
						}
					}
				}
			}
		}
	}
}

func TestMenuAndSettings(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m.cfgPath = filepath.Join(t.TempDir(), "tui.json")
	m = run(t, m, key("esc"))
	if m.overlay != ovMenu || !strings.Contains(m.View(), "█") {
		t.Fatal("esc did not open the menu with the logo")
	}
	m = run(t, m, key("down"), key("down"), key("down"), key("enter"))
	if m.overlay != ovSettings {
		t.Fatalf("overlay = %d, want settings", m.overlay)
	}
	m = run(t, m, key("down"), key("right"))
	if m.th.Name != Themes[1].Name {
		t.Fatalf("theme = %s", m.th.Name)
	}
	if c := LoadConfig(m.cfgPath); c.Theme != Themes[1].Name {
		t.Fatalf("saved config = %+v", c)
	}
	m = run(t, m, key("up"), key("right"), key("right")) // language: auto → en → ru
	if m.t.Lang != i18n.RU || !strings.Contains(m.View(), "Настройки") {
		t.Fatalf("language not switched: %v", m.t.Lang)
	}
	m = run(t, m, key("esc"), key("esc"))
	if m.overlay != ovNone {
		t.Fatal("esc esc did not close settings and menu")
	}
	// Menu item 0 toggles the VPN.
	_ = run(t, m, key("esc"), key("enter"))
	if len(f.connected) != 1 {
		t.Fatalf("menu toggle: connected = %v", f.connected)
	}
}

func TestMouse(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	lay := m.layout()
	click := func(x, y int) tea.MouseMsg {
		return tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}
	}
	// Rows: header "Provider A", server 1, server 2, header B, server 3.
	m = run(t, m, click(10, lay.listY+2))
	if m.cursor != 1 || len(f.connected) != 0 {
		t.Fatalf("single click: cursor %d, connected %v", m.cursor, f.connected)
	}
	m = run(t, m, click(10, lay.listY+2))
	if len(f.connected) != 1 || f.connected[0] != "a:2" {
		t.Fatalf("double click: connected %v", f.connected)
	}
	m = run(t, m, tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if m.cursor != 2 {
		t.Fatalf("wheel: cursor %d", m.cursor)
	}
	m = run(t, m, click(lay.tabs[1][0], lay.mainY))
	if m.tab != tabSubs {
		t.Fatalf("tab click: tab %d", m.tab)
	}
	m = run(t, m, click(lay.tabs[2][0]+1, lay.mainY))
	if m.tab != tabLog || !strings.Contains(m.View(), "connected: NL") {
		t.Fatalf("log tab: %d\n%s", m.tab, m.View())
	}
	// Esc menu: click the "Settings" item.
	m = run(t, m, key("esc"))
	_, y0, _ := m.overlayOrigin()
	m = run(t, m, click(m.width/2, y0+m.menuItemsTop()+int(miSettings)))
	if m.overlay != ovSettings {
		t.Fatalf("menu click: overlay %d", m.overlay)
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	f := newFake()
	for i := 4; i <= 60; i++ {
		f.servers = append(f.servers, api.Server{Index: i, Key: "b:" + strings.Repeat("x", i), Name: "Server", SubID: "b", SubName: "Provider B"})
	}
	m := loaded(t, f)
	m = run(t, m, key("G"))
	if !strings.Contains(m.View(), "60 ") {
		t.Fatalf("last server not visible after G:\n%s", m.View())
	}
}

// Every message the TUI passes through the translator must have a Russian
// translation.
func TestRussianCoverage(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	res := []*regexp.Regexp{
		regexp.MustCompile(`t\.[SF]\("((?:[^"\\]|\\.)+)"`),
		regexp.MustCompile(`k\("[^"]+", "([^"]+)"\)`),
		regexp.MustCompile(`\{"[^"]+", "([^"]+)"\}`),
		regexp.MustCompile(`row\("([^"]+)"`),
	}
	var missing []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, re := range res {
			for _, m := range re.FindAllStringSubmatch(string(src), -1) {
				if !i18n.Translated(i18n.RU, m[1]) {
					missing = append(missing, m[1])
				}
			}
		}
	}
	if len(missing) > 0 {
		t.Fatalf("missing Russian translations: %q", missing)
	}
}

func TestTrafficPanelAndSparkline(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	v := m.View()
	for _, want := range []string{"▼ 12.0 MB/s", "▲ 800 KB/s", "1.0 GB", "⣿", "×", "▁"} {
		if !strings.Contains(v, want) {
			t.Fatalf("view lacks %q:\n%s", want, v)
		}
	}
	m = run(t, m, key("i"))
	if !strings.Contains(m.View(), "nl.example.com:443") {
		t.Fatal("server info lacks the address")
	}
}

func TestAutoTestSetting(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("esc"), key("down"), key("down"), key("down"), key("enter"))
	m = run(t, m, key("down"), key("down"), key("right"))
	if f.svc.AutoTestMinutes != 10 {
		t.Fatalf("service setting = %+v", f.svc)
	}
	if !strings.Contains(m.View(), "every 10 min") {
		t.Fatalf("settings view:\n%s", m.View())
	}
}

// No terminal size may panic, and the main view stays exact where it is
// drawn at all (overlays are checked at fixed sizes in TestExactGeometry).
func TestEveryWidthRenders(t *testing.T) {
	f := newFake()
	f.status = api.Status{Connected: true, Server: &f.servers[0], Since: time.Now(), Selected: &f.servers[0]}
	m := New(f, i18n.EN, Config{})
	m = run(t, m, m.refresh()())
	for w := 20; w <= 120; w++ {
		for _, h := range []int{5, 15, 18, 24, 40} {
			m = run(t, m, tea.WindowSizeMsg{Width: w, Height: h})
			lines := strings.Split(m.View(), "\n")
			if w < 33 || h < 15 {
				continue
			}
			if len(lines) != h {
				t.Fatalf("%dx%d: %d lines", w, h, len(lines))
			}
			for i, l := range lines {
				if got := lipgloss.Width(l); got != w {
					t.Fatalf("%dx%d line %d: width %d", w, h, i, got)
				}
			}
		}
	}
}

func TestFoldGroup(t *testing.T) {
	f := newFake()
	m := loaded(t, f)
	m = run(t, m, key("z"))
	if n := len(m.visibleServers()); n != 2 {
		t.Fatalf("visible after fold = %d, want 2", n)
	}
	if v := m.View(); !strings.Contains(v, "▸ Provider A") || strings.Contains(v, "Germany") {
		t.Fatalf("folded view:\n%s", v)
	}
	// Enter on the folded row opens the group instead of connecting.
	m = run(t, m, key("enter"))
	if len(f.connected) != 0 || len(m.visibleServers()) != 3 {
		t.Fatalf("enter on folded row: connected %v, visible %d", f.connected, len(m.visibleServers()))
	}
	// Z folds everything, a second Z opens everything.
	m = run(t, m, key("Z"))
	if len(m.visibleServers()) != 2 {
		t.Fatalf("Z: visible = %d, want 2", len(m.visibleServers()))
	}
	m = run(t, m, key("Z"))
	if len(m.visibleServers()) != 3 {
		t.Fatalf("second Z: visible = %d, want 3", len(m.visibleServers()))
	}
}

func TestSortKeepsCursorServer(t *testing.T) {
	m := loaded(t, newFake())
	m = run(t, m, key("down")) // Germany
	m = run(t, m, key("s"))    // by latency: failed last
	vis := m.visibleServers()
	if vis[0].Key != "a:1" || vis[m.cursor].Key != "a:2" {
		t.Fatalf("latency sort: %v, cursor on %s", vis, vis[m.cursor].Key)
	}
	m = run(t, m, key("s")) // by name
	if vis = m.visibleServers(); vis[0].Key != "a:2" || vis[m.cursor].Key != "a:2" {
		t.Fatalf("name sort: first %s, cursor on %s", vis[0].Key, vis[m.cursor].Key)
	}
	m = run(t, m, key("s")) // back to the subscription order
	if m.visibleServers()[0].Key != "a:1" || m.cfg.Sort != sortOrder {
		t.Fatal("third s did not restore the order")
	}
}

func TestGroupHeaderShowsSubscriptionFacts(t *testing.T) {
	f := newFake()
	f.subs[1].Total, f.subs[1].Download = 100<<30, 25<<30
	m := loaded(t, f)
	v := m.View()
	if !strings.Contains(v, "Provider B — traffic 25.0 GB of 100 GB") || !strings.Contains(v, "days left") {
		t.Fatalf("header facts missing:\n%s", v)
	}
}
