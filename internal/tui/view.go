// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/paths"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// View renders the screen: exactly height lines of exactly width cells.
func (m Model) View() string {
	if m.width < 20 || m.height < 5 {
		return m.t.S("Window too small")
	}
	lay := m.layout()
	var lines []string
	if lay.topH > 0 {
		var st, tr []string
		if lay.statusW > 0 {
			st = m.statusBox(lay.statusW).render(m.th)
		}
		if lay.trafficW > 0 {
			tr = m.trafficBox(lay.trafficW).render(m.th)
		}
		lines = sideBySide(st, tr)
	}
	lines = append(lines, m.mainBox(lay).render(m.th)...)
	if m.overlay != ovNone {
		lines = overlay(lines, m.overlayLines(), m.width, m.th)
	}
	return strings.Join(lines, "\n")
}

// lr puts left and right text on one line of width w.
func lr(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- status panel

func (m Model) statusBox(w int) boxSpec {
	th := m.th
	in := w - 2
	var body []string
	switch {
	case !m.connected && m.daemonErr != "":
		body = []string{
			" " + th.err().Bold(true).Render("✗ "+m.t.S("tuntopd is not reachable")),
			" " + th.dim().Render(m.daemonErr),
		}
	case !m.connected:
		body = []string{" " + th.dim().Render(m.t.S("connecting to tuntopd…"))}
	case m.status.Connecting:
		body = []string{" " + th.warn().Bold(true).Render("◌ "+m.t.S("connecting…"))}
	case m.status.Connected:
		s := m.status.Server
		since := th.dim().Render(fmtDuration(time.Since(m.status.Since)) + " ")
		mode := th.dim().Render("TUN")
		if m.status.KillSwitch {
			mode += th.dim().Render(" · ") + th.ok().Render(m.t.S("kill switch"))
		}
		body = []string{
			lr(" "+th.ok().Bold(true).Render("● "+m.t.S("ON"))+"  "+mode, since, in),
			" " + th.title().Render(serverName(s)),
			" " + th.dim().Render(serverMeta(m, s)),
			" " + m.latencyLine(s),
			" " + m.subFacts(s),
		}
	case m.status.KillSwitch:
		// A reconnect failed while the kill switch was on.
		body = []string{
			" " + th.err().Bold(true).Render("■ "+m.t.S("BLOCKED")) + "  " + th.dim().Render(m.t.S("kill switch: no traffic outside the tunnel")),
			" " + th.text().Render(m.t.S("Enter: connect to a server")),
			" " + th.hi().Render("o") + th.text().Render(" "+m.t.S("turn off: traffic goes directly")),
		}
	default:
		body = []string{
			" " + th.dim().Bold(true).Render("○ "+m.t.S("OFF")) + "  " + th.dim().Render(m.t.S("traffic goes directly")),
		}
		if s := m.status.Selected; s != nil {
			body = append(body,
				" "+th.fg(th.BoxMain).Render("◆ ")+th.text().Render(s.Name),
				" "+th.dim().Render(serverMeta(m, s)),
				" "+th.hi().Render("o")+th.text().Render(" "+m.t.S("turn on")))
		} else {
			body = append(body, " "+th.text().Render(m.t.S("No server selected: pick one below and press Enter.")))
		}
	}
	if m.connected && m.status.LastError != "" && len(body) < 5 {
		for len(body) < 4 {
			body = append(body, "")
		}
		body = append(body, " "+th.err().Render(m.status.LastError))
	}
	return boxSpec{
		w: w, h: topHeight, color: th.BoxStatus,
		tabs: []tabLabel{{key: "1", text: m.t.S("status"), active: true}},
		body: body,
	}
}

func serverName(s *api.Server) string {
	if s == nil {
		return "?"
	}
	return s.Name
}

func serverMeta(m Model, s *api.Server) string {
	if s == nil {
		return ""
	}
	kind := s.Proto
	if s.Group {
		kind = m.t.F("group of %d", s.Proxies)
	}
	if s.SubName == "" {
		return kind
	}
	return s.SubName + " · " + kind
}

func (m Model) latencyLine(s *api.Server) string {
	if s == nil || s.Latency == 0 {
		return m.th.dim().Render(m.t.S("latency not measured")+" · ") + m.th.hi().Render("t")
	}
	return m.th.dim().Render(m.t.S("latency")+" ") + m.latency(s.Latency)
}

// subFacts summarizes the subscription of server s: traffic and expiry.
func (m Model) subFacts(s *api.Server) string {
	if s == nil {
		return ""
	}
	for _, sb := range m.subs {
		if sb.ID != s.SubID {
			continue
		}
		var f []string
		if used := sb.Download + sb.Upload; used > 0 || sb.Total > 0 {
			if sb.Total > 0 {
				f = append(f, m.t.F("traffic %s of %s", gb(used), gb(sb.Total)))
			} else {
				f = append(f, m.t.F("traffic %s", gb(used)))
			}
		}
		if !sb.Expire.IsZero() {
			days := int(time.Until(sb.Expire).Hours() / 24)
			txt := m.t.F("%d days left", days)
			if days < 3 {
				return m.th.dim().Render(strings.Join(f, " · ")+" · ") + m.th.warn().Render(txt)
			}
			f = append(f, txt)
		}
		return m.th.dim().Render(strings.Join(f, " · "))
	}
	return ""
}

// --- traffic panel

func (m Model) trafficBox(w int) boxSpec {
	th := m.th
	in := w - 2
	tr := m.traffic
	graph, scale := netGraph(th, tr.DownHist, tr.UpHist, in-2)
	down := th.fg(th.Down[1]).Bold(true).Render("▼ "+m.t.Rate(tr.DownRate)) +
		th.dim().Render("  "+m.t.Size(tr.DownTotal))
	up := th.fg(th.Up[1]).Bold(true).Render("▲ "+m.t.Rate(tr.UpRate)) +
		th.dim().Render("  "+m.t.Size(tr.UpTotal))
	body := make([]string, 0, 5)
	for _, g := range graph {
		body = append(body, " "+g)
	}
	body = append(body, lr(" "+down, up+" ", in))
	return boxSpec{
		w: w, h: topHeight, color: th.BoxTraffic,
		tabs:     []tabLabel{{key: "2", text: m.t.S("traffic"), active: true}},
		topRight: th.dim().Render(m.t.Rate(scale)),
		body:     body,
	}
}

// --- main panel

func (m Model) mainBox(lay screen) boxSpec {
	th := m.th
	in := m.width - 2
	b := boxSpec{w: m.width, h: lay.mainH, color: th.BoxMain, tabs: m.tabLabels()}

	switch {
	case m.busy != "":
		b.topRight = th.hi().Render(m.spinner.View()) + th.text().Render(" "+m.busy+"…")
	case m.msg != "" && m.msgErr:
		b.topRight = th.err().Render(m.msg)
	case m.msg != "":
		b.topRight = th.ok().Render(m.msg)
	}

	var hints []hint
	switch m.tab {
	case tabServers:
		b.body = m.serversBody(lay, in)
		onoff := m.t.S("on")
		if m.status.Connected {
			onoff = m.t.S("off")
		}
		// Most important first: what does not fit is dropped from the end.
		hints = []hint{
			{"↑↓", m.t.S("select")},
			{"↵", m.t.S("connect")},
			{"o", onoff},
			{"esc", m.t.S("menu")},
			{"t", m.t.S("latency")},
			{"/", m.t.S("filter")},
			{"i", m.t.S("info")},
			{"*", m.t.S("favorite")},
		}
		if n := len(m.visibleServers()); n > 0 {
			b.bottomRght = th.dim().Render(fmt.Sprintf("%d/%d", m.cursor+1, n))
		}
	case tabSubs:
		b.body = m.subsBody(lay, in)
		hints = []hint{
			{"↑↓", m.t.S("select")},
			{"a", m.t.S("add")},
			{"x", m.t.S("remove")},
			{"esc", m.t.S("menu")},
			{"u", m.t.S("update")},
			{"⇧↑↓", m.t.S("move")},
		}
	default:
		b.body = m.logBody(lay, in)
		hints = []hint{{"↑↓", m.t.S("scroll")}, {"End", m.t.S("to the end")}, {"esc", m.t.S("menu")}}
	}
	// Keep only the hints that fit, like btop drops them on narrow screens.
	room := in - lipgloss.Width(b.bottomRght) - 3
	for _, h := range hints {
		w := lipgloss.Width(h.key) + lipgloss.Width(h.text) + 4
		if room < w {
			break
		}
		b.hints = append(b.hints, h)
		room -= w
	}
	return b
}

func (m Model) serversBody(lay screen, in int) []string {
	th := m.th
	if !m.connected && m.daemonErr != "" {
		// The full-width box: the start command is too long for the status panel.
		return []string{
			"", " " + th.err().Render(m.daemonErr), "",
			" " + th.text().Render(m.t.S("Start the service:")),
			" " + th.hi().Render(paths.StartHint),
		}
	}
	vis := m.visibleServers()
	if len(vis) == 0 {
		if len(m.servers) == 0 {
			return []string{"", " " + th.text().Render(m.t.S("No servers yet. Press 4, then a, to add a subscription."))}
		}
		return []string{"", " " + th.text().Render(m.t.S("Nothing matches the filter (Esc clears it)."))}
	}
	cols := serverCols(in)
	header := th.title().Render(fmt.Sprintf("%2s%4s %s", "", "#", fit(m.t.S("Name"), cols.name))) +
		"  " + th.title().Render(padLeft(m.t.S("Type"), cols.kind)) + " "
	if cols.spark > 0 {
		header += strings.Repeat(" ", cols.spark+1)
	}
	header += th.title().Render(padLeft(m.t.S("Latency"), cols.lat))
	out := []string{header}

	cur, sel := "", ""
	if m.status.Connected && m.status.Server != nil {
		cur = m.status.Server.Key
	}
	if m.status.Selected != nil {
		sel = m.status.Selected.Key
	}
	rows := m.serverRows(vis)
	end := min(m.offset+lay.listRows, len(rows))
	for _, r := range rows[min(m.offset, len(rows)):end] {
		if r.server < 0 {
			out = append(out, " "+th.fg(th.BoxMain).Bold(true).Render(r.header))
			continue
		}
		out = append(out, m.serverLine(vis[r.server], cols, r.server == m.cursor, cur, sel, in))
	}
	return out
}

type columns struct{ name, kind, spark, lat int }

// serverCols splits the inner width: mark+number (7), name, type, latency
// history and latency. Narrow screens drop the history first.
func serverCols(in int) columns {
	c := columns{kind: 17, spark: 6, lat: 8}
	if in < 74 {
		c.spark = 0
	}
	if in < 66 {
		c.kind = 12
	}
	c.name = max(in-7-2-c.kind-1-c.spark-c.lat, 8)
	if c.spark > 0 {
		c.name--
	}
	return c
}

// sparkCol renders the history column including its leading space.
func (c columns) sparkCol(th Theme, hist []int) string {
	if c.spark == 0 {
		return ""
	}
	return " " + spark(th, hist, c.spark)
}

func (m Model) serverLine(s api.Server, c columns, atCursor bool, cur, sel string, in int) string {
	th := m.th
	mark := "  "
	switch s.Key {
	case cur:
		mark = th.ok().Bold(true).Render("● ")
	case sel:
		mark = th.fg(th.BoxMain).Bold(true).Render("◆ ")
	}
	kind := s.Proto
	if s.Group {
		kind = m.t.F("group of %d", s.Proxies)
	}
	if lipgloss.Width(kind) > c.kind {
		kind = ansi.Truncate(kind, c.kind, "…")
	}
	num := fmt.Sprintf("%4d ", s.Index)
	name := s.Name
	if s.Favorite && s.SubName != "" {
		name += " · " + s.SubName
	}
	if atCursor {
		line := ansi.Strip(mark) + num + fit(name, c.name) + "  " + padLeft(kind, c.kind) + " " +
			ansi.Strip(c.sparkCol(th, s.LatencyHistory)) + padLeft(ansi.Strip(m.latency(s.Latency)), c.lat)
		return th.sel().Render(fit(line, in))
	}
	return mark + th.dim().Render(num) + th.text().Render(fit(name, c.name)) + "  " +
		th.dim().Render(padLeft(kind, c.kind)) + " " + c.sparkCol(th, s.LatencyHistory) + padLeft(m.latency(s.Latency), c.lat)
}

func (m Model) latency(ms int) string {
	switch {
	case ms == 0:
		return ""
	case ms < 0:
		return m.th.err().Render("✗")
	}
	return latencyStyle(m.th, ms).Render(m.t.F("%d ms", ms))
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s
}

// subsBody draws the subscriptions; owners[i] is the subscription index of
// body line i (-1 for blank lines), for mouse clicks.
func (m Model) subsBody(lay screen, in int) []string {
	lines, owners := m.subsLines(in)
	// Scroll so the selected subscription is visible.
	top := 0
	for i, o := range owners {
		if o == m.subCursor {
			top = i
			break
		}
	}
	start := m.subsStart(lay, len(lines), top)
	return lines[start:min(start+lay.mainH-2, len(lines))]
}

// subsStart is the first visible line of the subscriptions list.
func (m Model) subsStart(lay screen, n, top int) int {
	return max(0, min(top, n-(lay.mainH-2)))
}

func (m Model) subsLines(in int) (lines []string, owners []int) {
	th := m.th
	if len(m.subs) == 0 {
		return []string{"", " " + th.text().Render(m.t.S("No subscriptions. Press a to add one."))}, []int{-1, -1}
	}
	add := func(owner int, s string) {
		lines = append(lines, s)
		owners = append(owners, owner)
	}
	for i, s := range m.subs {
		title := " " + s.Name + "  " + m.t.F("%d servers", s.Servers)
		if i == m.subCursor {
			add(i, th.sel().Render(fit(title, in)))
		} else {
			add(i, th.title().Render(" "+s.Name)+th.dim().Render("  "+m.t.F("%d servers", s.Servers)))
		}
		if s.URL != "" {
			add(i, "   "+th.dim().Render(s.URL))
		}
		var facts []string
		switch {
		case s.Total > 0:
			facts = append(facts, m.t.F("traffic %s of %s", gb(s.Download+s.Upload), gb(s.Total)))
		case s.Download+s.Upload > 0:
			facts = append(facts, m.t.F("traffic %s (unlimited)", gb(s.Download+s.Upload)))
		}
		if !s.Expire.IsZero() {
			days := int(time.Until(s.Expire).Hours() / 24)
			facts = append(facts, m.t.F("expires %s (%d days)", s.Expire.Format("2006-01-02"), days))
		}
		if !s.UpdatedAt.IsZero() {
			facts = append(facts, m.t.F("updated %s", s.UpdatedAt.Format("01-02 15:04")))
		}
		if len(facts) > 0 {
			add(i, "   "+th.text().Render(strings.Join(facts, " · ")))
		}
		if s.Error != "" {
			add(i, "   "+th.err().Render(m.t.F("last update failed: %s", s.Error)))
		}
		if s.Announce != "" {
			add(i, "   "+th.dim().Render(strings.ReplaceAll(s.Announce, "\n", " ")))
		}
		add(-1, "")
	}
	return lines, owners
}

func gb(b int64) string { return fmt.Sprintf("%.1f GB", float64(b)/(1<<30)) }

func (m Model) logBody(lay screen, in int) []string {
	th := m.th
	if len(m.logs) == 0 {
		return []string{"", " " + th.dim().Render(m.t.S("The service log is empty."))}
	}
	h := lay.mainH - 2
	end := len(m.logs) - m.logOffset
	start := max(end-h, 0)
	var out []string
	for _, l := range m.logs[start:end] {
		ts, text, _ := strings.Cut(l, " ")
		low := strings.ToLower(text)
		st := th.text()
		if strings.Contains(low, "error") || strings.Contains(low, "fail") {
			st = th.err()
		}
		out = append(out, " "+th.dim().Render(ts)+" "+st.Render(fit(text, max(in-len(ts)-2, 1))))
	}
	return out
}
