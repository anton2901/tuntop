// SPDX-License-Identifier: GPL-3.0-or-later

// Package tui is tuntop's terminal interface, laid out after btop: bordered
// panels with titles and key hints in the borders, an Esc menu with a big
// logo, themes and mouse support. It is a client of tuntopd like the CLI:
// every action goes through the control API, so quitting the TUI leaves
// the VPN as it is.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/i18n"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// Client is the part of api.Client the TUI uses.
type Client interface {
	Status(ctx context.Context) (api.Status, error)
	Servers(ctx context.Context) ([]api.Server, error)
	Connect(ctx context.Context, server string) (api.Status, error)
	Disconnect(ctx context.Context) (api.Status, error)
	Subscriptions(ctx context.Context) ([]api.Subscription, error)
	AddSubscription(ctx context.Context, req api.AddSubscriptionRequest) (api.Subscription, error)
	RemoveSubscription(ctx context.Context, id string) error
	Update(ctx context.Context, id string) ([]api.Subscription, error)
	Test(ctx context.Context, keys []string) ([]api.Server, error)
	Log(ctx context.Context) ([]string, error)
	Traffic(ctx context.Context) (api.Traffic, error)
	Settings(ctx context.Context) (api.Settings, error)
	SetSettings(ctx context.Context, s api.Settings) (api.Settings, error)
	SetFavorite(ctx context.Context, server string, on bool) (api.Server, error)
	MoveSubscription(ctx context.Context, id string, to int) ([]api.Subscription, error)
}

// Run starts the TUI and blocks until the user quits. lang is the detected
// system language, used when the config says "auto".
func Run(c Client, lang i18n.Lang) error {
	path := ConfigPath()
	m := New(c, lang, LoadConfig(path))
	m.cfgPath = path
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

type tab int

const (
	tabServers tab = iota
	tabSubs
	tabLog
)

// overlayKind is the window drawn over the panels, if any.
type overlayKind int

const (
	ovNone overlayKind = iota
	ovMenu
	ovSettings
	ovHelp
	ovInfo
	ovAddSub
	ovConfirm
	ovAbout
)

// refreshEvery is how often state is re-read from tuntopd; the traffic graph
// moves at this pace.
const refreshEvery = time.Second

// doubleClick is the max interval between clicks of a double click.
const doubleClick = 400 * time.Millisecond

// Model is the Bubble Tea model.
type Model struct {
	c       Client
	sysLang i18n.Lang
	t       i18n.T
	cfg     Config
	cfgPath string // "" in tests: settings are not saved
	th      Theme
	title   string // last window title sent to the terminal

	width, height int

	status    api.Status
	servers   []api.Server
	subs      []api.Subscription
	logs      []string
	traffic   api.Traffic
	svc       api.Settings
	connected bool // false until the first successful read from tuntopd
	daemonErr string

	tab     tab
	overlay overlayKind

	cursor int // index into visibleServers()
	// follow is a server key the cursor should land on after the next
	// refresh (the list reorders when favorites change).
	follow    string
	offset    int // first visible row of the server list
	subCursor int
	logOffset int // lines scrolled up from the end of the log
	menuSel   int
	setSel    int

	filtering bool
	filter    textinput.Model
	input     textinput.Model
	spinner   spinner.Model
	busy      string // what is running, "" when idle

	msg    string
	msgErr bool

	lastClick    time.Time
	lastClickRow int
}

// New returns the initial model.
func New(c Client, sysLang i18n.Lang, cfg Config) Model {
	m := Model{c: c, sysLang: sysLang, cfg: cfg, width: 80, height: 24}
	m.applyConfig()
	f := textinput.New()
	f.Prompt = ""
	// A steady cursor: no blink timers, nothing to redraw while idle.
	f.Cursor.SetMode(cursor.CursorStatic)
	in := textinput.New()
	in.Prompt = "> "
	in.Placeholder = "https://…"
	// The URL carries the subscription token: do not show it on screen.
	in.EchoMode = textinput.EchoPassword
	in.EchoCharacter = '•'
	in.Cursor.SetMode(cursor.CursorStatic)
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	m.filter, m.input, m.spinner = f, in, sp
	return m
}

// applyConfig derives language and theme from cfg.
func (m *Model) applyConfig() {
	lang := m.sysLang
	switch m.cfg.Lang {
	case "en":
		lang = i18n.EN
	case "ru":
		lang = i18n.RU
	}
	m.t = i18n.T{Lang: lang}
	m.th = themeByName(m.cfg.Theme)
	m.cfg.Theme = m.th.Name
}

func (m *Model) saveConfig() {
	if m.cfgPath == "" {
		return
	}
	if err := m.cfg.Save(m.cfgPath); err != nil {
		m.setMsg(m.t.F("Settings not saved: %s", err), true)
	}
}

// --- messages

type (
	// snapshotMsg is a full re-read of tuntopd's state.
	snapshotMsg struct {
		status  api.Status
		servers []api.Server
		subs    []api.Subscription
		logs    []string
		traffic api.Traffic
		svc     api.Settings
	}
	tickMsg struct{}
	// doneMsg ends a busy operation; err is shown, text otherwise.
	doneMsg struct {
		text string
		err  error
	}
	daemonErrMsg struct{ err error }
)

// Init loads everything and starts the refresh loop.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.refresh(), tick(), m.spinner.Tick)
}

func tick() tea.Cmd {
	return tea.Tick(refreshEvery, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) refresh() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		var s snapshotMsg
		var err error
		if s.status, err = m.c.Status(ctx); err != nil {
			return daemonErrMsg{err}
		}
		if s.servers, err = m.c.Servers(ctx); err != nil {
			return daemonErrMsg{err}
		}
		if s.subs, err = m.c.Subscriptions(ctx); err != nil {
			return daemonErrMsg{err}
		}
		// Older daemons lack these: not fatal.
		s.logs, _ = m.c.Log(ctx)
		s.traffic, _ = m.c.Traffic(ctx)
		s.svc, _ = m.c.Settings(ctx)
		return s
	}
}

// do runs a slow API call in the background with a busy indicator.
func (m *Model) do(busy string, f func(ctx context.Context) (string, error)) tea.Cmd {
	if m.busy != "" {
		m.setMsg(m.t.F("Please wait: %s", m.busy), true)
		return nil
	}
	m.busy = busy
	m.msg = ""
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		text, err := f(ctx)
		return doneMsg{text, err}
	}
}

func (m *Model) setMsg(s string, isErr bool) {
	m.msg, m.msgErr = s, isErr
}

// --- update

// Update handles messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()
		return m, nil
	case snapshotMsg:
		m.status, m.servers, m.subs, m.logs = msg.status, msg.servers, msg.subs, msg.logs
		m.traffic, m.svc = msg.traffic, msg.svc
		m.connected, m.daemonErr = true, ""
		if m.follow != "" {
			for i, s := range m.visibleServers() {
				if s.Key == m.follow {
					m.cursor = i
				}
			}
			m.follow = ""
		}
		m.clamp()
		return m, m.setTitle()
	case daemonErrMsg:
		m.connected, m.daemonErr = false, msg.err.Error()
		return m, nil
	case tickMsg:
		return m, tea.Batch(m.refresh(), tick())
	case doneMsg:
		m.busy = ""
		if msg.err != nil {
			m.setMsg(msg.err.Error(), true)
		} else {
			m.setMsg(msg.text, false)
		}
		return m, m.refresh()
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.MouseMsg:
		return m.mouse(msg)
	case tea.KeyMsg:
		// A result message stays only until the next key press.
		if m.busy == "" {
			m.msg = ""
		}
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() == "ctrl+c" {
		return m, tea.Quit
	}
	// Text inputs take keys as typed, the confirm dialog knows "д"/"н";
	// everything else goes by key position.
	if m.overlay != ovAddSub && m.overlay != ovConfirm && !m.filtering {
		k = latinKey(k)
	}
	switch m.overlay {
	case ovMenu:
		return m.keyMenu(k)
	case ovSettings:
		return m.keySettings(k)
	case ovAddSub:
		return m.keyAddSub(k)
	case ovConfirm:
		return m.keyConfirm(k)
	case ovHelp, ovInfo, ovAbout:
		m.overlay = ovNone
		return m, nil
	}
	if m.filtering {
		return m.keyFilter(k)
	}

	switch k.String() {
	case "q":
		return m, tea.Quit
	case "esc":
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			m.clamp()
			return m, nil
		}
		m.overlay, m.menuSel = ovMenu, 0
		return m, nil
	case "?", "f1":
		m.overlay = ovHelp
		return m, nil
	case "tab":
		m.tab = (m.tab + 1) % 3
		return m, nil
	case "shift+tab":
		m.tab = (m.tab + 2) % 3
		return m, nil
	case "3":
		m.tab = tabServers
		return m, nil
	case "4":
		m.tab = tabSubs
		return m, nil
	case "5":
		m.tab = tabLog
		return m, nil
	case "1":
		m.cfg.HideStatus = !m.cfg.HideStatus
		m.saveConfig()
		m.clamp()
		return m, nil
	case "2":
		m.cfg.HideTraffic = !m.cfg.HideTraffic
		m.saveConfig()
		m.clamp()
		return m, nil
	case "r":
		return m, m.refresh()
	case "o", " ":
		return m, m.toggle()
	case "u":
		return m, m.update()
	}
	switch m.tab {
	case tabServers:
		return m.keyServers(k)
	case tabSubs:
		return m.keySubs(k)
	default:
		return m.keyLog(k)
	}
}

func (m Model) keyServers(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	vis := m.visibleServers()
	switch k.String() {
	case "up", "k":
		m.cursor--
	case "down", "j":
		m.cursor++
	case "pgup":
		m.cursor -= m.layout().listRows
	case "pgdown":
		m.cursor += m.layout().listRows
	case "home", "g":
		m.cursor = 0
	case "end", "G":
		m.cursor = len(vis) - 1
	case "/", "f":
		m.filtering = true
		return m, m.filter.Focus()
	case "enter":
		return m, m.connectCursor()
	case "z", "left":
		fold := true
		m.toggleFold(false, &fold)
	case "right":
		fold := false
		m.toggleFold(false, &fold)
	case "Z":
		m.toggleFold(true, nil)
	case "s":
		m.cycleSort()
	case "i":
		if m.cursor < len(vis) && !m.foldedAtCursor() {
			m.overlay = ovInfo
		}
	case "t":
		return m, m.test(nil)
	case "T":
		if m.cursor < len(vis) && !m.foldedAtCursor() {
			return m, m.test([]string{vis[m.cursor].Key})
		}
	case "*":
		if m.cursor < len(vis) && !m.foldedAtCursor() {
			return m, m.favorite(vis[m.cursor])
		}
	default:
		// Fast typing or a paste arrives as one message: "/smart".
		if r := k.Runes; k.Type == tea.KeyRunes && len(r) > 1 && r[0] == '/' {
			m.filtering = true
			m.filter.SetValue(string(r[1:]))
			m.filter.CursorEnd()
			m.cursor, m.offset = 0, 0
			return m, m.filter.Focus()
		}
	}
	m.clamp()
	return m, nil
}

func (m Model) keySubs(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "up", "k":
		m.subCursor--
	case "down", "j":
		m.subCursor++
	case "a":
		return m.openAddSub()
	case "x", "delete":
		if m.subCursor < len(m.subs) {
			m.overlay = ovConfirm
		}
	case "shift+up", "K":
		return m, m.moveSub(-1)
	case "shift+down", "J":
		return m, m.moveSub(1)
	}
	m.clamp()
	return m, nil
}

func (m Model) keyLog(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "up", "k":
		m.logOffset++
	case "down", "j":
		m.logOffset--
	case "pgup":
		m.logOffset += m.layout().listRows
	case "pgdown":
		m.logOffset -= m.layout().listRows
	case "end", "G":
		m.logOffset = 0
	}
	m.clamp()
	return m, nil
}

func (m Model) keyFilter(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return m, nil
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filter.SetValue("")
		m.clamp()
		return m, nil
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.cursor, m.offset = 0, 0
	return m, cmd
}

func (m Model) openAddSub() (tea.Model, tea.Cmd) {
	m.overlay = ovAddSub
	m.input.SetValue("")
	return m, m.input.Focus()
}

func (m Model) keyAddSub(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.overlay = ovNone
		m.input.Blur()
		return m, nil
	case "enter":
		u := strings.TrimSpace(m.input.Value())
		m.overlay = ovNone
		m.input.Blur()
		m.input.SetValue("")
		if u == "" {
			return m, nil
		}
		m.tab = tabSubs
		return m, m.do(m.t.S("downloading subscription"), func(ctx context.Context) (string, error) {
			s, err := m.c.AddSubscription(ctx, api.AddSubscriptionRequest{URL: u})
			if err != nil {
				// The reason may be cut to fit; the service log (5) has it whole.
				return "", fmt.Errorf("%s: %w", m.t.S("Subscription not added"), err)
			}
			return m.t.F("Added %s: %d servers", s.Name, s.Servers), nil
		})
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	return m, cmd
}

func (m Model) keyConfirm(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.overlay = ovNone
	switch k.String() {
	case "y", "Y", "д", "Д", "н", "Н": // "н" is "y" on a Russian layout
	default:
		return m, nil
	}
	if m.subCursor >= len(m.subs) {
		return m, nil
	}
	s := m.subs[m.subCursor]
	return m, m.do(m.t.S("removing subscription"), func(ctx context.Context) (string, error) {
		return m.t.F("Removed %s", s.Name), m.c.RemoveSubscription(ctx, s.ID)
	})
}

// --- actions

// toggle turns the VPN off when on, and on (selected server) when off.
func (m *Model) toggle() tea.Cmd {
	if m.status.Connected || m.status.KillSwitch {
		return m.do(m.t.S("disconnecting"), func(ctx context.Context) (string, error) {
			_, err := m.c.Disconnect(ctx)
			return m.t.S("VPN off: traffic goes directly"), err
		})
	}
	return m.connect("")
}

func (m *Model) connectCursor() tea.Cmd {
	vis := m.visibleServers()
	if m.cursor >= len(vis) {
		return nil
	}
	if m.foldedAtCursor() {
		// A collapsed group's row opens the group instead.
		fold := false
		m.toggleFold(false, &fold)
		return nil
	}
	return m.connect(vis[m.cursor].Key)
}

func (m *Model) connect(key string) tea.Cmd {
	return m.do(m.t.S("connecting"), func(ctx context.Context) (string, error) {
		st, err := m.c.Connect(ctx, key)
		if err != nil {
			return "", err
		}
		return m.t.F("Connected: %s", st.Server.Name), nil
	})
}

func (m *Model) update() tea.Cmd {
	id := ""
	if m.tab == tabSubs && m.subCursor < len(m.subs) {
		id = m.subs[m.subCursor].ID
	}
	return m.do(m.t.S("updating subscriptions"), func(ctx context.Context) (string, error) {
		_, err := m.c.Update(ctx, id)
		return m.t.S("Subscriptions updated"), err
	})
}

// favorite toggles s in favorites; the cursor follows it to its new place.
func (m *Model) favorite(s api.Server) tea.Cmd {
	m.follow = s.Key
	return m.do(m.t.S("saving"), func(ctx context.Context) (string, error) {
		if _, err := m.c.SetFavorite(ctx, s.Key, !s.Favorite); err != nil {
			return "", err
		}
		if s.Favorite {
			return m.t.F("Removed from favorites: %s", s.Name), nil
		}
		return m.t.F("Added to favorites: %s", s.Name), nil
	})
}

// moveSub moves the subscription under the cursor up (-1) or down (+1).
func (m *Model) moveSub(by int) tea.Cmd {
	i := m.subCursor
	if i >= len(m.subs) || i+by < 0 || i+by >= len(m.subs) || m.subs[i].ID == api.ManualID || m.subs[i+by].ID == api.ManualID {
		return nil // manual links stay last
	}
	id := m.subs[i].ID
	m.subCursor += by
	return m.do(m.t.S("saving"), func(ctx context.Context) (string, error) {
		_, err := m.c.MoveSubscription(ctx, id, i+by+1)
		return "", err
	})
}

func (m *Model) test(keys []string) tea.Cmd {
	return m.do(m.t.S("measuring latency"), func(ctx context.Context) (string, error) {
		_, err := m.c.Test(ctx, keys)
		return m.t.S("Latency measured"), err
	})
}

// --- list helpers

func (m Model) visibleServers() []api.Server {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	if q == "" {
		return m.arrange(m.servers)
	}
	var out []api.Server
	for _, s := range m.servers {
		if strings.Contains(strings.ToLower(s.Name), q) || strings.Contains(strings.ToLower(s.SubName), q) {
			out = append(out, s)
		}
	}
	return m.arrange(out)
}

// clamp keeps cursors in range and the server cursor on screen.
func (m *Model) clamp() {
	vis := m.visibleServers()
	m.cursor = min(max(m.cursor, 0), max(len(vis)-1, 0))
	m.subCursor = min(max(m.subCursor, 0), max(len(m.subs)-1, 0))
	h := m.layout().listRows
	m.logOffset = min(max(m.logOffset, 0), max(len(m.logs)-h, 0))
	rows := m.serverRows(vis)
	cur := cursorRow(rows, m.cursor)
	// Keep the group header above the first server visible when possible.
	if cur-1 < m.offset {
		m.offset = max(cur-1, 0)
	}
	if cur >= m.offset+h {
		m.offset = cur - h + 1
	}
	m.offset = min(max(m.offset, 0), max(len(rows)-h, 0))
}

// row is a line of the server list: a subscription header or a server.
type row struct {
	header string
	server int // index into the visible servers; -1 for headers
	// folded rows stand for a collapsed group: server is its first server
	// and count how many the group has.
	folded bool
	count  int
}

// serverRows groups servers under headers: favorites, then subscriptions.
func (m Model) serverRows(vis []api.Server) []row {
	var rows []row
	last := "\x00"
	for i, s := range vis {
		group, title := groupKey(s), s.SubName
		if s.Favorite {
			title = m.t.S("★ Favorites")
		}
		if group != last {
			last = group
			if m.folding() && m.isFolded(group) {
				rows = append(rows, row{header: title, server: i, folded: true, count: m.groupSize(group)})
				continue
			}
			rows = append(rows, row{header: title, server: -1})
		}
		rows = append(rows, row{server: i})
	}
	return rows
}

func cursorRow(rows []row, cursor int) int {
	for i, r := range rows {
		if r.server == cursor {
			return i
		}
	}
	return 0
}

func fmtDuration(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%02d:%02d:%02d", s/3600, s/60%60, s%60)
}

// setTitle updates the terminal title, like btop does: the connected
// server and its latency, so the tab shows the state without switching.
func (m *Model) setTitle() tea.Cmd {
	title := "tuntop"
	if m.status.Connected && m.status.Server != nil {
		title += " · ● " + m.status.Server.Name
		if l := m.status.Server.Latency; l > 0 {
			title += " · " + m.t.F("%d ms", l)
		}
	}
	if title == m.title {
		return nil
	}
	m.title = title
	return tea.SetWindowTitle(title)
}
