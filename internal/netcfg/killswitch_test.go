// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"strings"
	"testing"
)

func TestNftRules(t *testing.T) {
	c := Default
	c.Iface, c.Mark = "tuntop0", 2023
	r := nftRules(c)
	for _, want := range []string{
		"add table inet tuntop\ndelete table inet tuntop\n",
		"type filter hook output priority 0; policy accept;",
		`oifname "tuntop0" accept`,
		"meta mark 2023 accept",
		"192.168.0.0/16",
		"fe80::/10",
		"udp sport 68 udp dport 67 accept",
	} {
		if !strings.Contains(r, want) {
			t.Errorf("no %q in\n%s", want, r)
		}
	}
	// Reject comes last: everything allowed is matched before it.
	if !strings.HasSuffix(strings.TrimSpace(r), "reject\n\t}\n}") {
		t.Errorf("reject is not the last rule:\n%s", r)
	}
}

func TestPfRules(t *testing.T) {
	c := Default
	c.Iface = "utun227"
	r := pfRules(c)
	lines := strings.Split(strings.TrimSpace(r), "\n")
	if lines[len(lines)-1] != "block return out quick all" {
		t.Fatalf("block is not last:\n%s", r)
	}
	for _, want := range []string{"pass out quick on utun227 all no state", "proto tcp all user root flags any", "10.0.0.0/8", "fc00::/7"} {
		if !strings.Contains(r, want) {
			t.Errorf("no %q in\n%s", want, r)
		}
	}
}

func TestParsePfToken(t *testing.T) {
	out := "No ALTQ support in kernel\nALTQ related functions disabled\npf enabled\nToken : 13897461876451092163\n"
	if got := parsePfToken(out); got != "13897461876451092163" {
		t.Fatalf("token = %q", got)
	}
	if parsePfToken("pf enabled\n") != "" {
		t.Fatal("token from nothing")
	}
}
