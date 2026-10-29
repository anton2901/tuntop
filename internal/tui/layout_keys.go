// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import tea "github.com/charmbracelet/bubbletea"

// Keys are matched by the Latin letters on the keys. With a Russian
// layout active the same keys send Cyrillic letters; latinKey turns them
// back (ЙЦУКЕН, as on PC and Mac keyboards) so "t", "a", "o" work in
// either layout. Text inputs (filter, subscription URL) get keys as typed.
var ruToLatin = func() map[rune]rune {
	const ru = "йцукенгшщзхъфывапролджэячсмитьбюё"
	const en = "qwertyuiop[]asdfghjkl;'zxcvbnm,.`"
	const ruUp = "ЙЦУКЕНГШЩЗХЪФЫВАПРОЛДЖЭЯЧСМИТЬБЮЁ"
	const enUp = "QWERTYUIOP{}ASDFGHJKL:\"ZXCVBNM<>~"
	m := map[rune]rune{}
	add := func(from, to string) {
		t := []rune(to)
		for i, r := range []rune(from) {
			m[r] = t[i]
		}
	}
	add(ru, en)
	add(ruUp, enUp)
	// Punctuation keys: "/" (filter) and "?" (help) are "." and "," there.
	m['.'] = '/'
	m[','] = '?'
	return m
}()

// latinKey maps a single Cyrillic key press to its Latin key.
func latinKey(k tea.KeyMsg) tea.KeyMsg {
	if k.Type != tea.KeyRunes || len(k.Runes) != 1 {
		return k
	}
	if r, ok := ruToLatin[k.Runes[0]]; ok {
		k.Runes = []rune{r}
	}
	return k
}
