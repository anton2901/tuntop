// SPDX-License-Identifier: GPL-3.0-or-later

package safe

import "testing"

func TestText(t *testing.T) {
	for in, want := range map[string]string{
		"🇳🇱 Netherlands · 🟢10%":        "🇳🇱 Netherlands · 🟢10%",
		"\x1b]52;c;ZXZpbA==\x07name":   "]52;c;ZXZpbA== name",
		"a\x1b[2Jb":                    "a [2Jb",
		"line1\r\nline2":               "line1  line2",
		"evil‮gnp.exe":            "evilgnp.exe",
		"zero​width":              "zerowidth",
		"c1\u009bcode":                 "c1 code",
		"👨‍👩‍👧":            "👨‍👩‍👧",
		"  padded \x00 ":               "padded",
	} {
		if got := Text(in); got != want {
			t.Errorf("Text(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Multiline("a\nb\x1b[0m"); got != "a\nb [0m" {
		t.Errorf("Multiline = %q", got)
	}
}
