// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/i18n"
	"github.com/anton2901/tuntop/internal/version"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Esc menu

type menuItem int

const (
	miToggle menuItem = iota
	miUpdate
	miAddSub
	miSettings
	miHelp
	miQuit
)

func (m Model) menuLabels() []string {
	on := m.t.S("Turn VPN on")
	if m.status.Connected {
		on = m.t.S("Turn VPN off")
	}
	return []string{
		on, m.t.S("Update subscriptions"), m.t.S("Add subscription"),
		m.t.S("Settings"), m.t.S("Help"), m.t.S("Quit"),
	}
}

// menuItemsTop is the line of the first menu item within menuLines: the
// logo, the version line and two blank lines come first.
func (m Model) menuItemsTop() int { return len(logo(m.th, m.width)) + 3 }

func (m Model) menuLines() []string {
	th := m.th
	lg := logo(th, m.width)
	w := 0
	for _, l := range lg {
		w = max(w, lipgloss.Width(l))
	}
	w = max(w, 30)
	out := append([]string{}, lg...)
	out = append(out, centerIn(th.dim().Render("v"+version.Version), w), "", "")
	for i, l := range m.menuLabels() {
		text := "  " + l + "  "
		if i == m.menuSel {
			text = th.sel().Render("▶ " + l + " ◀")
		} else {
			text = th.text().Render(text)
		}
		out = append(out, centerIn(text, w))
	}
	out = append(out, "", centerIn(th.dim().Render(m.t.S("↑↓ choose · Enter open · Esc back")), w))
	return out
}

func (m Model) keyMenu(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.menuLabels())
	switch k.String() {
	case "esc", "q":
		m.overlay = ovNone
	case "up", "k":
		m.menuSel = (m.menuSel + n - 1) % n
	case "down", "j", "tab":
		m.menuSel = (m.menuSel + 1) % n
	case "enter", " ":
		return m.activateMenu(menuItem(m.menuSel))
	}
	return m, nil
}

func (m Model) activateMenu(it menuItem) (tea.Model, tea.Cmd) {
	m.overlay = ovNone
	switch it {
	case miToggle:
		return m, m.toggle()
	case miUpdate:
		return m, m.update()
	case miAddSub:
		return m.openAddSub()
	case miSettings:
		m.overlay, m.setSel = ovSettings, 0
	case miHelp:
		m.overlay = ovHelp
	case miQuit:
		return m, tea.Quit
	}
	return m, nil
}

// --- settings

type setting struct {
	label  string
	value  string
	change func(m *Model, dir int) tea.Cmd
}

// autoTestChoices are the offered latency auto-test intervals, minutes.
var autoTestChoices = []int{0, 10, 30, 60, 360}

var langs = []string{"auto", "en", "ru"}

func (m Model) settings() []setting {
	onOff := func(on bool) string {
		if on {
			return m.t.S("on")
		}
		return m.t.S("off")
	}
	shown := func(hidden bool) string {
		if hidden {
			return m.t.S("hidden")
		}
		return m.t.S("shown")
	}
	langName := map[string]string{"auto": m.t.S("system"), "en": "English", "ru": "Русский"}
	autoTest := m.t.S("manual")
	if n := m.svc.AutoTestMinutes; n > 0 {
		autoTest = m.t.F("every %s", fmtMinutes(m.t, n))
	}
	return []setting{
		{m.t.S("Language"), langName[m.cfg.Lang], func(m *Model, d int) tea.Cmd {
			m.cfg.Lang = cycle(langs, m.cfg.Lang, d)
			return nil
		}},
		{m.t.S("Theme"), m.th.Name, func(m *Model, d int) tea.Cmd {
			names := make([]string, len(Themes))
			for i, t := range Themes {
				names[i] = t.Name
			}
			m.cfg.Theme = cycle(names, m.cfg.Theme, d)
			return nil
		}},
		{m.t.S("Latency test"), autoTest, func(m *Model, d int) tea.Cmd {
			i := 0
			for j, v := range autoTestChoices {
				if v == m.svc.AutoTestMinutes {
					i = j
				}
			}
			n := autoTestChoices[(i+d+len(autoTestChoices))%len(autoTestChoices)]
			m.svc.AutoTestMinutes = n // shown at once; the service confirms on refresh
			return m.saveService()
		}},
		{m.t.S("Kill switch"), onOff(m.svc.KillSwitch), func(m *Model, _ int) tea.Cmd {
			m.svc.KillSwitch = !m.svc.KillSwitch
			return m.saveService()
		}},
		{m.t.S("Status panel"), shown(m.cfg.HideStatus), func(m *Model, _ int) tea.Cmd {
			m.cfg.HideStatus = !m.cfg.HideStatus
			return nil
		}},
		{m.t.S("Traffic panel"), shown(m.cfg.HideTraffic), func(m *Model, _ int) tea.Cmd {
			m.cfg.HideTraffic = !m.cfg.HideTraffic
			return nil
		}},
	}
}

// saveService sends the service settings; a refusal shows as an error and
// the next refresh puts the real value back.
func (m *Model) saveService() tea.Cmd {
	c, s := m.c, m.svc
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := c.SetSettings(ctx, s); err != nil {
			return doneMsg{err: err}
		}
		return nil
	}
}

func cycle(list []string, cur string, dir int) string {
	i := 0
	for j, v := range list {
		if v == cur {
			i = j
		}
	}
	return list[(i+dir+len(list))%len(list)]
}

// settingsTop is the line of the first setting within settingsLines.
const settingsTop = 2

const settingsWidth = 50

func (m Model) settingsLines() []string {
	th := m.th
	var body []string
	body = append(body, "")
	for i, s := range m.settings() {
		val := "‹ " + s.value + " ›"
		line := " " + fit(s.label, 20) + padLeft(val, settingsWidth-2-22) + " "
		if i == m.setSel {
			body = append(body, th.sel().Render(line))
		} else {
			body = append(body, th.text().Render(" "+fit(s.label, 20))+th.title().Render(padLeft(val, settingsWidth-2-22))+" ")
		}
	}
	body = append(body, "", " "+th.dim().Render(m.t.S("Saved automatically.")))
	b := boxSpec{
		w: settingsWidth, h: len(body) + 2, color: th.BoxMain,
		tabs:  []tabLabel{{text: m.t.S("Settings"), active: true}},
		hints: []hint{{"↑↓", ""}, {"←→", m.t.S("change")}, {"esc", m.t.S("back")}},
		body:  body,
	}
	return b.render(th)
}

func (m Model) keySettings(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.settings())
	switch k.String() {
	case "esc", "q":
		m.overlay = ovMenu
	case "up", "k":
		m.setSel = (m.setSel + n - 1) % n
	case "down", "j", "tab":
		m.setSel = (m.setSel + 1) % n
	case "left", "h":
		return m, m.changeSetting(-1)
	case "right", "l", "enter", " ":
		return m, m.changeSetting(1)
	}
	return m, nil
}

func (m *Model) changeSetting(dir int) tea.Cmd {
	cmd := m.settings()[m.setSel].change(m, dir)
	m.applyConfig()
	m.saveConfig()
	m.clamp()
	return cmd
}

func fmtMinutes(t i18n.T, n int) string {
	if n%60 == 0 {
		return t.F("%d h", n/60)
	}
	return t.F("%d min", n)
}

// --- help, info, dialogs

func (m Model) helpLines() []string {
	th := m.th
	rows := [][2]string{
		{"↑↓ PgUp PgDn", "move"},
		{"Enter", "connect to the server under the cursor"},
		{"o / Space", "turn the VPN on (last server) or off"},
		{"t / T", "measure latency: all / the server under the cursor"},
		{"/", "filter servers by name; Esc clears"},
		{"i", "server details"},
		{"*", "add to or remove from favorites (listed first)"},
		{"u", "update subscriptions now"},
		{"3 4 5, Tab", "servers, subscriptions, log"},
		{"1 2", "show or hide the status and traffic panels"},
		{"a / x", "add / remove a subscription (subscriptions tab)"},
		{"Shift+↑↓ K J", "move a subscription up / down"},
		{"Esc", "menu: settings, theme, language"},
		{"q", "quit (the VPN keeps its state)"},
		{"", ""},
		{"●  ◆", "connected / selected (used by o)"},
		{"mouse", "click: select, double click: connect, wheel: scroll"},
	}
	w := min(m.width-4, 70)
	var body []string
	for _, r := range rows {
		if r[0] == "" {
			body = append(body, "")
			continue
		}
		body = append(body, " "+th.hi().Render(fmt.Sprintf("%-13s", r[0]))+" "+th.text().Render(m.t.S(r[1])))
	}
	return boxSpec{
		w: w, h: len(body) + 2, color: th.BoxMain,
		tabs:  []tabLabel{{text: m.t.S("Keys"), active: true}},
		hints: []hint{{m.t.S("any key"), m.t.S("back")}},
		body:  body,
	}.render(th)
}

func (m Model) infoLines() []string {
	th := m.th
	vis := m.visibleServers()
	if m.cursor >= len(vis) {
		return nil
	}
	s := vis[m.cursor]
	state := m.t.S("not connected")
	switch {
	case m.status.Connected && m.status.Server != nil && m.status.Server.Key == s.Key:
		state = th.ok().Render("● " + m.t.S("connected"))
	case m.status.Selected != nil && m.status.Selected.Key == s.Key:
		state = "◆ " + m.t.S("selected")
	}
	kind := s.Proto
	if s.Group {
		kind = m.t.F("provider group of %d servers: picks the best one itself", s.Proxies)
	}
	lat := m.t.S("not measured")
	if s.Latency != 0 {
		lat = m.latency(s.Latency)
	}
	row := func(k, v string) string { return " " + th.dim().Render(fit(m.t.S(k), 14)) + " " + th.text().Render(v) }
	body := []string{
		" " + th.title().Render(s.Name), "",
		row("Subscription", s.SubName),
		row("Type", kind),
		row("Address", s.Address),
		row("Latency", lat),
		row("State", state),
		row("Number", fmt.Sprint(s.Index)),
		row("Key", s.Key),
	}
	return boxSpec{
		w: min(m.width-4, 70), h: len(body) + 2, color: th.BoxMain,
		tabs:  []tabLabel{{text: m.t.S("server"), active: true}},
		hints: []hint{{"↵", m.t.S("connect")}, {m.t.S("any key"), m.t.S("back")}},
		body:  body,
	}.render(th)
}

func (m Model) addSubLines() []string {
	th := m.th
	w := min(m.width-4, 64)
	m.input.Width = w - 8
	body := []string{
		"",
		" " + th.text().Render(m.t.S("Paste the subscription URL (Ctrl+Shift+V).")),
		" " + th.dim().Render(m.t.S("It is hidden on screen: it works like a password.")),
		"",
		" " + m.input.View(),
		"",
	}
	return boxSpec{
		w: w, h: len(body) + 2, color: th.BoxMain,
		tabs:  []tabLabel{{text: m.t.S("Add subscription"), active: true}},
		hints: []hint{{"↵", m.t.S("add")}, {"esc", m.t.S("cancel")}},
		body:  body,
	}.render(th)
}

func (m Model) confirmLines() []string {
	th := m.th
	if m.subCursor >= len(m.subs) {
		return nil
	}
	body := []string{
		"",
		" " + th.text().Render(m.t.F("Remove subscription %s?", m.subs[m.subCursor].Name)),
		" " + th.dim().Render(m.t.S("Its servers disappear from the list.")),
		"",
	}
	return boxSpec{
		w: min(m.width-4, 60), h: len(body) + 2, color: th.Err,
		tabs:  []tabLabel{{text: m.t.S("Remove"), active: true}},
		hints: []hint{{"y", m.t.S("yes")}, {"n", m.t.S("no")}},
		body:  body,
	}.render(th)
}

func (m Model) overlayLines() []string {
	switch m.overlay {
	case ovMenu:
		return m.menuLines()
	case ovSettings:
		return m.settingsLines()
	case ovHelp:
		return m.helpLines()
	case ovInfo:
		return m.infoLines()
	case ovAddSub:
		return m.addSubLines()
	case ovConfirm:
		return m.confirmLines()
	}
	return nil
}

// overlayOrigin is where overlay() puts the overlay's top-left corner.
func (m Model) overlayOrigin() (x, y int, lines []string) {
	lines = m.overlayLines()
	w := 0
	for _, l := range lines {
		w = max(w, lipgloss.Width(l))
	}
	return max((m.width-w)/2, 0), max((m.height-len(lines))/2, 0), lines
}

func centerIn(s string, w int) string {
	pad := max((w-lipgloss.Width(s))/2, 0)
	return strings.Repeat(" ", pad) + s + strings.Repeat(" ", max(w-pad-lipgloss.Width(s), 0))
}
