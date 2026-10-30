// SPDX-License-Identifier: GPL-3.0-or-later

package diag

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRedact(t *testing.T) {
	secrets := []string{
		"vless://3f1c2a9e-1111-4222-8333-444455556666@example.org:443?pbk=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA#name",
		"3f1c2a9e-1111-4222-8333-444455556666",
		"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"203.0.113.77",
		"2001:db8:1:2::5",
		"hy2://pass@host:1",
	}
	for _, s := range secrets {
		in := "error dialing " + s + " failed"
		if out := Redact(in); strings.Contains(out, s) {
			t.Errorf("Redact(%q) = %q leaks", in, out)
		}
	}
	if out := Redact("GET https://sub.example.org/sub/secrettoken123?x=1 done"); out != "GET https://sub.example.org/… done" {
		t.Errorf("url: %q", out)
	}
}

func TestRedactKeeps(t *testing.T) {
	for _, s := range []string{
		"127.0.0.1:10808", "192.168.1.5 via 10.0.0.1", "fe80::1 dev tuntop0", "::1", "15:04:05",
		"tuntop 0.3.0-rc.2 (abc1234)", "connected: NL Amsterdam (120ms)",
	} {
		if out := Redact(s); out != s {
			t.Errorf("Redact(%q) = %q, want unchanged", s, out)
		}
	}
	a, b := Redact("203.0.113.77"), Redact("route to 203.0.113.77")
	if !strings.HasSuffix(b, a) || !strings.HasPrefix(a, "203.0.x.x#") {
		t.Errorf("same address must mask the same way: %q %q", a, b)
	}
}

func TestRotatingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "log")
	r, err := OpenRotating(p, 20, 3)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	for _, l := range []string{"aaaaaaaaaaaa\n", "bbbbbbbbbbbb\n", "cccccccccccc\n", "dddddddddddd\n", "eeeeeeeeeeee\n"} {
		if _, err := r.Write([]byte(l)); err != nil {
			t.Fatal(err)
		}
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	if read(p) != "eeeeeeeeeeee\n" || read(p+".1") != "dddddddddddd\n" || read(p+".2") != "cccccccccccc\n" {
		t.Fatalf("files: %q %q %q", read(p), read(p+".1"), read(p+".2"))
	}
	if _, err := os.Stat(p + ".3"); err == nil {
		t.Fatal("kept more files than asked")
	}
}

func TestStamped(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	w := Stamped{W: &buf, Now: func() time.Time { return now }}
	in := "server 203.0.113.77 down\nsecond line\n"
	if n, err := w.Write([]byte(in)); err != nil || n != len(in) {
		t.Fatalf("n=%d err=%v", n, err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "2026-10-30T12:00:00.000Z server 203.0.x.x#") || !strings.HasSuffix(lines[1], " second line") {
		t.Fatalf("got %q", lines)
	}
}

func TestTail(t *testing.T) {
	p := filepath.Join(t.TempDir(), "log")
	_ = os.WriteFile(p+".1", []byte("1\n2\n"), 0o644)
	_ = os.WriteFile(p, []byte("3\n4\n"), 0o644)
	if got := strings.Join(Tail(p, 3), ","); got != "2,3,4" {
		t.Fatalf("got %q", got)
	}
	_ = os.WriteFile(p+".2", []byte("0\n"), 0o644)
	if got := strings.Join(Tail(p, 9), ","); got != "0,1,2,3,4" {
		t.Fatalf("with .2: %q", got)
	}
	if Tail(filepath.Join(t.TempDir(), "none"), 5) != nil {
		t.Fatal("missing file must give no lines")
	}
}
