// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// glyphs are 5×5 pixel letters of the logo; each pixel is drawn as two
// full blocks, so a letter is 10 cells wide and 5 rows tall.
var glyphs = map[rune][5]string{
	'T': {"#####", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", ".###."},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#"},
	'O': {".###.", "#...#", "#...#", "#...#", ".###."},
	'P': {"####.", "#...#", "####.", "#....", "#...."},
}

// logoWord is drawn by logo; logoWidth is its width in cells.
const (
	logoWord  = "TUNTOP"
	logoWidth = len(logoWord)*12 - 2 + 1
)

// logo renders the name in the theme's logo gradient, with a dark drop
// shadow one cell right and down, like btop's title. Narrower than the
// pixel logo, it falls back to one line of bold letters.
func logo(th Theme, maxW int) []string {
	if maxW < logoWidth {
		return []string{lipgloss.NewStyle().Foreground(th.Logo[0]).Bold(true).Render(logoWord)}
	}
	const word = logoWord
	rows := make([][]rune, 6)
	w := len(word)*12 - 2 + 1
	for i := range rows {
		rows[i] = []rune(strings.Repeat(" ", w))
	}
	for li, ch := range word {
		g := glyphs[ch]
		for y := 0; y < 5; y++ {
			for x := 0; x < 5; x++ {
				if g[y][x] != '#' {
					continue
				}
				cx := li*12 + x*2
				// shadow first, then the pixel on top
				for _, d := range []int{1, 2} {
					if rows[y+1][cx+d] == ' ' {
						rows[y+1][cx+d] = '░'
					}
				}
				rows[y][cx], rows[y][cx+1] = '█', '█'
			}
		}
	}
	out := make([]string, len(rows))
	shadow := th.dim().Faint(true)
	for y, r := range rows {
		var b strings.Builder
		col := th.Logo[min(y, len(th.Logo)-1)]
		st := lipgloss.NewStyle().Foreground(col)
		for _, c := range r {
			switch c {
			case '█':
				b.WriteString(st.Render("█"))
			case '░':
				b.WriteString(shadow.Render("░"))
			default:
				b.WriteRune(' ')
			}
		}
		out[y] = b.String()
	}
	return out
}
