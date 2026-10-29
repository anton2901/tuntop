// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// wheelStep is how many rows one wheel notch scrolls.
const wheelStep = 3

func (m Model) mouse(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	if e.Action != tea.MouseActionPress {
		return m, nil
	}
	if m.overlay != ovNone {
		return m.mouseOverlay(e)
	}
	lay := m.layout()

	switch e.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		d := wheelStep
		if e.Button == tea.MouseButtonWheelUp {
			d = -d
		}
		switch m.tab {
		case tabServers:
			m.cursor += d
		case tabSubs:
			m.subCursor += d / wheelStep
		default:
			m.logOffset -= d
		}
		m.clamp()
		return m, nil
	case tea.MouseButtonLeft:
	default:
		return m, nil
	}

	// Tabs in the main box border.
	if e.Y == lay.mainY {
		for i, r := range lay.tabs {
			if e.X >= r[0] && e.X < r[1] {
				m.tab = tab(i)
				return m, nil
			}
		}
		return m, nil
	}
	switch m.tab {
	case tabServers:
		i := e.Y - lay.listY
		if i < 0 || i >= lay.listRows {
			return m, nil
		}
		rows := m.serverRows(m.visibleServers())
		r := m.offset + i
		if r >= len(rows) || rows[r].server < 0 {
			return m, nil
		}
		idx := rows[r].server
		now := time.Now()
		double := idx == m.lastClickRow && now.Sub(m.lastClick) < doubleClick
		m.cursor, m.lastClickRow, m.lastClick = idx, idx, now
		m.clamp()
		if double {
			m.lastClick = time.Time{}
			return m, m.connectCursor()
		}
	case tabSubs:
		_, owners := m.subsLines(m.width - 2)
		top := 0
		for i, o := range owners {
			if o == m.subCursor {
				top = i
				break
			}
		}
		line := m.subsStart(lay, len(owners), top) + e.Y - (lay.mainY + 1)
		if line >= 0 && line < len(owners) && owners[line] >= 0 {
			m.subCursor = owners[line]
		}
	}
	return m, nil
}

func (m Model) mouseOverlay(e tea.MouseMsg) (tea.Model, tea.Cmd) {
	x0, y0, lines := m.overlayOrigin()
	switch m.overlay {
	case ovMenu, ovSettings:
		up := e.Button == tea.MouseButtonWheelUp
		if up || e.Button == tea.MouseButtonWheelDown {
			k := tea.KeyMsg{Type: tea.KeyDown}
			if up {
				k = tea.KeyMsg{Type: tea.KeyUp}
			}
			return m.key(k)
		}
	}
	if e.Button != tea.MouseButtonLeft {
		return m, nil
	}
	inside := e.Y >= y0 && e.Y < y0+len(lines) && e.X >= x0
	switch m.overlay {
	case ovMenu:
		i := e.Y - y0 - m.menuItemsTop()
		if inside && i >= 0 && i < len(m.menuLabels()) {
			m.menuSel = i
			return m.activateMenu(menuItem(i))
		}
		if !inside {
			m.overlay = ovNone
		}
	case ovSettings:
		i := e.Y - y0 - settingsTop
		if inside && i >= 0 && i < len(m.settings()) {
			m.setSel = i
			return m, m.changeSetting(1)
		} else if !inside {
			m.overlay = ovMenu
		}
	case ovAddSub, ovConfirm:
		if !inside {
			m.overlay = ovNone
			m.input.Blur()
		}
	default:
		m.overlay = ovNone
	}
	return m, nil
}
