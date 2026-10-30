// SPDX-License-Identifier: GPL-3.0-or-later

// Package safe cleans text that comes from a provider (server names,
// titles, announcements, error messages) before it reaches a terminal or
// a log. A name may carry escape sequences that rewrite the screen or set
// the clipboard, or bidi marks that reorder what the user reads.
package safe

import (
	"strings"
	"unicode"
)

// Text returns s as one clean line: control characters (ESC, CR, LF, tab,
// C1) become spaces, bidi overrides and zero-width spaces are dropped.
func Text(s string) string { return clean(s, false) }

// Multiline is Text that keeps line feeds.
func Multiline(s string) string { return clean(s, true) }

func clean(s string, keepNL bool) string {
	if !needsCleaning(s) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n' && keepNL:
			b.WriteRune(r)
		case unicode.IsControl(r):
			b.WriteByte(' ')
		case invisible(r):
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func needsCleaning(s string) bool {
	for _, r := range s {
		if unicode.IsControl(r) || invisible(r) {
			return true
		}
	}
	return false
}

// invisible reports bidi controls and zero-width characters that change
// what is shown without being shown themselves. ZWJ and ZWNJ stay: emoji
// sequences and some scripts need them.
func invisible(r rune) bool {
	switch {
	case r == 0x200B, r == 0x2060, r == 0xFEFF, r == 0x200E, r == 0x200F:
		return true
	case r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}
