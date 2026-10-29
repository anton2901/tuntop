// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Rounded box parts, as in btop.
const (
	bTL, bTR, bBL, bBR = "╭", "╮", "╰", "╯"
	bH, bV             = "─", "│"
)

var superscript = map[string]string{"1": "¹", "2": "²", "3": "³", "4": "⁴", "5": "⁵"}

// tabLabel is a title in a box border: an optional hotkey digit shown as a
// superscript, and text; the active one is bright, others dimmed.
type tabLabel struct {
	key, text string
	active    bool
}

// hint is a "key description" pair drawn in a bottom border.
type hint struct{ key, text string }

// boxSpec describes one bordered panel.
type boxSpec struct {
	w, h       int
	color      lipgloss.TerminalColor
	tabs       []tabLabel // in the top border, left
	topRight   string     // already styled
	hints      []hint     // in the bottom border, left
	bottomRght string     // already styled
	body       []string   // inner lines, styled; padded/cut to fit
}

// render draws the box as exactly h lines of exactly w cells.
func (b boxSpec) render(th Theme) []string {
	if b.w < 4 || b.h < 2 {
		return nil
	}
	border := th.fg(b.color)
	inner := b.w - 2
	out := make([]string, 0, b.h)

	// Top border: ╭─¹title─title──────right─╮
	var top strings.Builder
	for _, t := range b.tabs {
		top.WriteString(border.Render(bH))
		if sup, ok := superscript[t.key]; ok {
			top.WriteString(th.hi().Render(sup))
		}
		if t.active {
			top.WriteString(th.title().Render(t.text))
		} else {
			top.WriteString(th.dim().Render(t.text))
		}
	}
	out = append(out, borderLine(border, bTL, bTR, top.String(), b.topRight, inner))

	for i := 0; i < b.h-2; i++ {
		line := ""
		if i < len(b.body) {
			line = b.body[i]
		}
		out = append(out, border.Render(bV)+fit(line, inner)+border.Render(bV))
	}

	var bottom strings.Builder
	for _, h := range b.hints {
		bottom.WriteString(border.Render(bH) + " " + th.hi().Render(h.key) + " " + th.text().Render(h.text) + " ")
	}
	out = append(out, borderLine(border, bBL, bBR, bottom.String(), b.bottomRght, inner))
	return out
}

// borderLine joins corner, left content, fill, right content and corner
// into exactly inner+2 cells, dropping content that does not fit.
func borderLine(border lipgloss.Style, l, r, left, right string, inner int) string {
	lw := lipgloss.Width(left)
	if right != "" {
		// Cut a long message (an error) rather than lose it.
		if room := inner - lw - 2; lipgloss.Width(right) > room && room >= 12 {
			right = ansi.Truncate(right, room, "…")
		}
		right = border.Render(bH) + right + border.Render(bH)
	}
	rw := lipgloss.Width(right)
	if lw+rw > inner {
		right, rw = "", 0
	}
	if lw > inner {
		left, lw = ansi.Truncate(left, inner, ""), inner
	}
	return border.Render(l) + left + border.Render(strings.Repeat(bH, inner-lw-rw)) + right + border.Render(r)
}

// fit pads or truncates a styled line to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = ansi.Truncate(s, w, "…")
	}
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// sideBySide joins two blocks of equal height horizontally.
func sideBySide(a, b []string) []string {
	out := make([]string, max(len(a), len(b)))
	for i := range out {
		if i < len(a) {
			out[i] += a[i]
		}
		if i < len(b) {
			out[i] += b[i]
		}
	}
	return out
}

// overlay draws box over base (dimmed) centered, like btop's menu.
func overlay(base, box []string, width int, th Theme) []string {
	out := make([]string, len(base))
	dim := th.dim().Faint(true)
	for i, l := range base {
		out[i] = dim.Render(ansi.Strip(l))
	}
	bw := 0
	for _, l := range box {
		bw = max(bw, lipgloss.Width(l))
	}
	x := max((width-bw)/2, 0)
	y := max((len(base)-len(box))/2, 0)
	for i, l := range box {
		row := y + i
		if row >= len(out) {
			break
		}
		bg := out[row]
		left := ansi.Truncate(bg, x, "")
		left += strings.Repeat(" ", max(x-lipgloss.Width(left), 0))
		right := ansi.TruncateLeft(bg, x+lipgloss.Width(l), "")
		out[row] = left + l + right
	}
	return out
}
