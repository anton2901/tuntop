// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Braille cells filled from the bottom (download, grows up) and from the
// top (upload, grows down), 0–4 dot rows, as in btop's net graph.
var (
	fillUp   = []string{" ", "⣀", "⣤", "⣶", "⣿"}
	fillDown = []string{" ", "⠉", "⠛", "⠿", "⣿"}
)

// minScale keeps an idle line from looking busy: 64 KiB/s full height.
const minScale = 64 << 10

// netGraph draws download (2 rows, growing up) above upload (2 rows,
// growing down), one column per second, newest on the right, both on the
// same scale. It returns 4 lines of width w and the scale used.
func netGraph(th Theme, down, up []int64, w int) ([]string, int64) {
	down, up = tail(down, w), tail(up, w)
	scale := int64(minScale)
	for i := range down {
		scale = max(scale, down[i], up[i])
	}
	level := func(v int64) int { // 0..8
		return int((v*8 + scale - 1) / scale)
	}
	rows := make([]strings.Builder, 4)
	pad := w - len(down)
	for r := range rows {
		rows[r].WriteString(strings.Repeat(" ", pad))
	}
	st := func(c lipgloss.TerminalColor) lipgloss.Style { return lipgloss.NewStyle().Foreground(c) }
	base := th.dim().Faint(true)
	for i := range down {
		d, u := level(down[i]), level(up[i])
		// download: row 1 is the lower half, row 0 the upper
		lower, upper := min(d, 4), max(d-4, 0)
		if lower == 0 {
			rows[1].WriteString(base.Render("⣀"))
		} else {
			rows[1].WriteString(st(th.Down[min(d/4, 1)]).Render(fillUp[lower]))
		}
		rows[0].WriteString(st(th.Down[2]).Render(fillUp[upper]))
		// upload mirrored: row 2 next to the middle, row 3 further down
		near, far := min(u, 4), max(u-4, 0)
		if near == 0 {
			rows[2].WriteString(base.Render("⠉"))
		} else {
			rows[2].WriteString(st(th.Up[min(u/4, 1)]).Render(fillDown[near]))
		}
		rows[3].WriteString(st(th.Up[2]).Render(fillDown[far]))
	}
	out := make([]string, 4)
	for i := range rows {
		out[i] = rows[i].String()
	}
	return out, scale
}

func tail(s []int64, n int) []int64 {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// sparkLevels draw latency history on a fixed scale; taller is slower.
var (
	sparkLevels = []rune("▁▂▃▄▅▆▇█")
	sparkLimits = []int{150, 250, 350, 500, 700, 1000, 1500} // ms, upper bounds
)

// spark renders the last w latency measurements (ms, -1 failed).
func spark(th Theme, hist []int, w int) string {
	if len(hist) > w {
		hist = hist[len(hist)-w:]
	}
	var b strings.Builder
	b.WriteString(strings.Repeat(" ", w-len(hist)))
	for _, v := range hist {
		switch {
		case v < 0:
			b.WriteString(th.err().Render("×"))
		case v == 0:
			b.WriteString(" ")
		default:
			i := len(sparkLimits)
			for j, lim := range sparkLimits {
				if v < lim {
					i = j
					break
				}
			}
			b.WriteString(latencyStyle(th, v).Render(string(sparkLevels[i])))
		}
	}
	return b.String()
}

func latencyStyle(th Theme, ms int) lipgloss.Style {
	switch {
	case ms < 400:
		return th.ok()
	case ms < 1000:
		return th.warn()
	default:
		return th.err()
	}
}
