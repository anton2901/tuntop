// SPDX-License-Identifier: GPL-3.0-or-later

// Package i18n translates user-facing strings. Messages are keyed by their
// English text (gettext style), so English needs no table and a missing
// translation falls back to readable English.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Lang is a supported interface language.
type Lang string

// Supported languages.
const (
	EN Lang = "en"
	RU Lang = "ru"
)

// Detect picks the language: $TUNTOP_LANG, then the usual locale variables.
func Detect() Lang {
	for _, v := range []string{"TUNTOP_LANG", "LC_ALL", "LC_MESSAGES", "LANG", "LANGUAGE"} {
		val := strings.ToLower(os.Getenv(v))
		if val == "" || val == "c" || val == "posix" {
			continue
		}
		if strings.HasPrefix(val, "ru") {
			return RU
		}
		return EN
	}
	return EN
}

// T translates into one language.
type T struct {
	Lang Lang
}

// S returns the translation of an English message.
func (t T) S(msg string) string {
	if t.Lang == RU {
		if tr, ok := ru[msg]; ok {
			return tr
		}
	}
	return msg
}

// F translates a format string and applies args.
func (t T) F(format string, args ...any) string {
	return fmt.Sprintf(t.S(format), args...)
}

// Translated reports whether msg has a translation into lang (English
// always does). Used by coverage tests.
func Translated(lang Lang, msg string) bool {
	if lang != RU {
		return true
	}
	_, ok := ru[msg]
	return ok
}
