// SPDX-License-Identifier: GPL-3.0-or-later

// Package netcfg routes system traffic into tuntop's TUN interface.
//
// Xray creates the TUN device but leaves addressing and routing to the OS.
// Xray's own sockets must not enter the TUN (a loop); Bypass tells how to
// mark them on this OS.
//
// On Linux this package uses policy routing:
//
//	pref P+0  fwmark Mark          → main   (Xray's own sockets: no loop)
//	pref P+1  udp dport 53         → Table  (DNS, even to LAN resolvers)
//	pref P+2… to private/LAN nets  → main   (LAN, ssh from home stay direct)
//	pref P+9  all                  → Table  (everything else)
//	Table:    default dev <Iface>
//
// systemd-resolved binds its upstream sockets to a link, which bypasses
// policy routing, so it is also told to send every query (routing domain
// "~.") to the DNS address inside the TUN.
//
// Removing the rules restores normal routing; the TUN device itself goes
// away when Xray stops.
//
// On macOS there are no socket marks: Xray's sockets are bound to the
// physical interface (IP_BOUND_IF), and routes 0/1 and 128/1 (::/1 and
// 8000::/1 for IPv6) through the utun take everything else. The primary
// network service's DNS is pointed at the TUN and restored afterwards; the
// saved original survives a crash in StateDir and Recover puts it back.
package netcfg

import (
	"net/netip"
	"runtime"
)

// Config describes the routing setup.
type Config struct {
	// Iface is the TUN interface name, created by Xray.
	Iface string
	// Addr is assigned to Iface; any private /30 not used elsewhere.
	Addr netip.Prefix
	// DNS is the resolver address announced to the system. It is the other
	// host of Addr's /30, so queries enter the TUN and reach Xray's DNS.
	DNS netip.Addr
	// Table is the routing table holding the default route via Iface.
	Table int
	// Mark is the SO_MARK Xray puts on its outbound sockets.
	Mark uint32
	// Priority is the first ip-rule priority used; P..P+9 are reserved.
	Priority int
	// StateDir keeps what must be restored after a crash (macOS DNS).
	StateDir string
}

// Default is the configuration tuntop uses. Table and marks differ from the
// old sing-box setup on the test machine (table 2022) to avoid clashes.
// macOS requires utunN names; 227 is unlikely to be taken.
var Default = Config{
	Iface:    map[bool]string{true: "utun227", false: "tuntop0"}[runtime.GOOS == "darwin"],
	Addr:     netip.MustParsePrefix("172.23.0.1/30"),
	DNS:      netip.MustParseAddr("172.23.0.2"),
	Table:    2023,
	Mark:     2023,
	Priority: 9100,
}
