// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import "github.com/charmbracelet/lipgloss"

// topHeight is the height of the status and traffic panels.
const topHeight = 7

// minTopScreen is the smallest terminal height that still shows the top
// panels; below it the server list gets the whole screen.
const minTopScreen = 18

// screen is the geometry of the main view, shared by View and the mouse
// handler so clicks map to what was drawn.
type screen struct {
	topH              int // 0 when the top panels are hidden
	statusW, trafficW int // 0 for a hidden panel
	mainY, mainH      int
	listY             int // first row of list content
	listRows          int
	tabs              [3][2]int // x ranges of the tab titles on row mainY
}

func (m Model) layout() screen {
	var s screen
	showStatus, showTraffic := !m.cfg.HideStatus, !m.cfg.HideTraffic
	if m.height >= minTopScreen && (showStatus || showTraffic) {
		s.topH = topHeight
		switch {
		case showStatus && showTraffic:
			s.statusW = max(m.width*45/100, 30)
			s.trafficW = m.width - s.statusW
		case showStatus:
			s.statusW = m.width
		default:
			s.trafficW = m.width
		}
	}
	s.mainY = s.topH
	s.mainH = max(m.height-s.topH, 3)
	// main box: top border, column header, rows…, bottom border
	s.listY = s.mainY + 2
	s.listRows = max(s.mainH-3, 1)

	x := 1 // after the corner
	for i, t := range m.tabLabels() {
		x++ // the "─" before the label
		w := lipgloss.Width(superscript[t.key]) + lipgloss.Width(t.text)
		if i < 3 {
			s.tabs[i] = [2]int{x, x + w}
		}
		x += w
	}
	return s
}

// tabLabels are the main box titles: three tabs, plus the filter.
func (m Model) tabLabels() []tabLabel {
	l := []tabLabel{
		{key: "3", text: m.t.S("servers"), active: m.tab == tabServers},
		{key: "4", text: m.t.S("subscriptions"), active: m.tab == tabSubs},
		{key: "5", text: m.t.S("log"), active: m.tab == tabLog},
	}
	if m.tab == tabServers && (m.filtering || m.filter.Value() != "") {
		text := "/" + m.filter.Value()
		if m.filtering {
			text += "▏"
		}
		l = append(l, tabLabel{text: text, active: true})
	}
	return l
}
