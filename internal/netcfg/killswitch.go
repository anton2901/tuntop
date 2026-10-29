// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

// Kill switch: while it is on, traffic may leave the machine only through
// the tunnel. It stays on across server switches, reconnects and a crash
// of the service (the rules live in the kernel), and goes off with "tuntop
// off" or the next start of the service (Recover).
//
// Always allowed: loopback, the TUN itself, the LAN and link-local ranges,
// DHCP, and the tunnel's own sockets.
//
//   - Linux: an nftables table "inet tuntop" with an output chain. The
//     tunnel's sockets carry Config.Mark, so the rule is exact.
//   - macOS: pf rules in the anchor "com.apple/tuntop", which the system
//     ruleset evaluates. Sockets have no marks there; traffic of root
//     processes is let through (Xray runs as root), so the switch protects
//     user applications, not system daemons.
//
// The rule texts are built here, without a build tag, so their tests run
// on every OS.

import (
	"bytes"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

// runInput executes a command with stdin; a test hook.
var runInput = func(stdin, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		return out.String(), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

// localV4 and localV6 are destinations that never need the tunnel.
var (
	localV4 = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("100.64.0.0/10"),
		netip.MustParsePrefix("127.0.0.0/8"),
		netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
		netip.MustParsePrefix("224.0.0.0/4"),
		netip.MustParsePrefix("255.255.255.255/32"),
	}
	localV6 = []netip.Prefix{
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("fe80::/10"),
		netip.MustParsePrefix("fc00::/7"),
		netip.MustParsePrefix("ff00::/8"),
	}
)

func joinPrefixes(ps []netip.Prefix) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = p.String()
	}
	return strings.Join(s, ", ")
}

// nftTable is the nftables table holding the kill switch.
const nftTable = "inet tuntop"

// nftRules is the nft script that (re)creates the kill switch table.
// "add" then "delete" makes it idempotent: the table may not exist yet.
func nftRules(c Config) string {
	return fmt.Sprintf(`add table %[1]s
delete table %[1]s
table %[1]s {
	chain killswitch {
		type filter hook output priority 0; policy accept;
		oifname "lo" accept
		oifname %[2]q accept
		meta mark %[3]d accept
		ip daddr { %[4]s } accept
		ip6 daddr { %[5]s } accept
		udp sport 68 udp dport 67 accept
		reject
	}
}
`, nftTable, c.Iface, c.Mark, joinPrefixes(localV4), joinPrefixes(localV6))
}

// pfRules are the kill switch rules for pf.
func pfRules(c Config) string {
	// pf's TCP pass rules default to "flags S/SA keep state": they match
	// only new connections, and the ones open before the switch (ssh from
	// the LAN) would hit the block. Local and tunnel traffic passes
	// statelessly; root's TCP with "flags any".
	return fmt.Sprintf(`pass out quick on lo0 all no state
pass out quick on %s all no state
pass out quick inet from any to { %s } no state
pass out quick inet6 from any to { %s } no state
pass out quick proto udp from any port 68 to any port 67
pass out quick proto tcp all user root flags any
pass out quick proto udp all user root
block return out quick all
`, c.Iface, joinPrefixes(localV4), joinPrefixes(localV6))
}
