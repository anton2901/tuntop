// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const mainTable = unix.RT_TABLE_MAIN

// System access goes through these hooks so tests can replace it: netlink
// for interfaces, routes and rules, commands for resolved and nftables.
var (
	nl = struct {
		linkByName   func(string) (netlink.Link, error)
		linkSetUp    func(netlink.Link) error
		linkDel      func(netlink.Link) error
		addrReplace  func(netlink.Link, *netlink.Addr) error
		routeReplace func(*netlink.Route) error
		ruleAdd      func(*netlink.Rule) error
		ruleDel      func(*netlink.Rule) error
		ruleList     func(int) ([]netlink.Rule, error)
	}{
		netlink.LinkByName, netlink.LinkSetUp, netlink.LinkDel, netlink.AddrReplace,
		netlink.RouteReplace, netlink.RuleAdd, netlink.RuleDel, netlink.RuleList,
	}
	lookPath = exec.LookPath
	// resolvedStub exists while systemd-resolved manages DNS.
	resolvedStub = "/run/systemd/resolve/stub-resolv.conf"
	procSys      = "/proc/sys"
)

// Private ranges that never go into the tunnel (besides DNS).
var bypass = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
}

// Setup configures routing into c.Iface and returns a function that undoes
// it. It waits briefly for Xray to create the interface.
func Setup(c Config) (cleanup func() error, err error) {
	link, err := waitLink(c.Iface, 5*time.Second)
	if err != nil {
		return nil, err
	}
	// Stale rules from a crashed run would shadow the new ones.
	_ = deleteRules(c)

	addr := &netlink.Addr{IPNet: &net.IPNet{IP: c.Addr.Addr().AsSlice(), Mask: net.CIDRMask(c.Addr.Bits(), 32)}}
	if err := nl.addrReplace(link, addr); err != nil {
		return nil, fmt.Errorf("assign %s to %s: %w", c.Addr, c.Iface, err)
	}
	if err := nl.linkSetUp(link); err != nil {
		return nil, fmt.Errorf("bring %s up: %w", c.Iface, err)
	}
	// Replies arrive on the TUN while the main table points elsewhere;
	// strict reverse-path filtering would drop them.
	_ = os.WriteFile(fmt.Sprintf("%s/net/ipv4/conf/%s/rp_filter", procSys, c.Iface), []byte("2"), 0o644)

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		Table:     c.Table,
		Scope:     netlink.SCOPE_LINK,
	}
	if err := nl.routeReplace(route); err != nil {
		return nil, fmt.Errorf("default route via %s: %w", c.Iface, err)
	}

	undo := func() error { return deleteRules(c) }
	for _, r := range rules(c) {
		if err := nl.ruleAdd(r); err != nil {
			_ = undo()
			return nil, fmt.Errorf("add ip rule %v: %w", r, err)
		}
	}
	if err := setResolved(c); err != nil {
		_ = undo()
		return nil, err
	}
	// resolved drops per-link settings by itself when the interface goes.
	return undo, nil
}

// setResolved points systemd-resolved at the TUN, if resolved is in use.
// Without it, resolved's link-bound sockets would send queries straight to
// the LAN resolver, past the tunnel.
func setResolved(c Config) error {
	if _, err := os.Stat(resolvedStub); err != nil {
		return nil // resolved not running; dport 53 rule catches plain resolv.conf
	}
	for _, args := range [][]string{
		{"dns", c.Iface, c.DNS.String()},
		{"domain", c.Iface, "~."},
		{"default-route", c.Iface, "yes"},
	} {
		if _, err := runInput("", "resolvectl", args...); err != nil {
			return err
		}
	}
	return nil
}

func rules(c Config) []*netlink.Rule {
	var out []*netlink.Rule
	add := func(pref, table int, f func(*netlink.Rule)) {
		r := netlink.NewRule()
		r.Family = unix.AF_INET
		r.Priority = pref
		r.Table = table
		f(r)
		out = append(out, r)
	}
	p := c.Priority
	add(p, mainTable, func(r *netlink.Rule) { r.Mark = c.Mark })
	add(p+1, c.Table, func(r *netlink.Rule) {
		r.IPProto = unix.IPPROTO_UDP
		r.Dport = netlink.NewRulePortRange(53, 53)
	})
	for _, b := range bypass {
		add(p+2, mainTable, func(r *netlink.Rule) {
			r.Dst = &net.IPNet{IP: b.Addr().AsSlice(), Mask: net.CIDRMask(b.Bits(), 32)}
		})
	}
	add(p+9, c.Table, func(*netlink.Rule) {})
	return out
}

// deleteRules removes every IPv4 rule in our priority range.
func deleteRules(c Config) error {
	list, err := nl.ruleList(unix.AF_INET)
	if err != nil {
		return fmt.Errorf("list ip rules: %w", err)
	}
	var errs []error
	for i := range list {
		r := list[i]
		if r.Priority >= c.Priority && r.Priority <= c.Priority+9 {
			if err := nl.ruleDel(&r); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func waitLink(name string, timeout time.Duration) (netlink.Link, error) {
	deadline := time.Now().Add(timeout)
	for {
		link, err := nl.linkByName(name)
		if err == nil {
			return link, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("interface %s did not appear: %w", name, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// RemoveLink waits for Xray to release the TUN interface after it stops
// and deletes it if it lingers; a new instance cannot create an interface
// with the same name while the old one exists.
func RemoveLink(name string) error {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := nl.linkByName(name); err != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	link, err := nl.linkByName(name)
	if err != nil {
		return nil
	}
	if err := nl.linkDel(link); err != nil {
		return fmt.Errorf("delete %s: %w", name, err)
	}
	return nil
}

// Bypass is the sockopt that keeps Xray's sockets out of the TUN.
func Bypass(c Config) (map[string]any, error) {
	return map[string]any{"mark": int(c.Mark)}, nil
}

// Recover removes rules and the kill switch left by a crashed run.
func Recover(c Config) error { return errors.Join(deleteRules(c), KillSwitchOff(c)) }

// KillSwitchOn blocks outgoing traffic outside the tunnel (see killswitch.go).
func KillSwitchOn(c Config) error {
	nft, err := lookPath("nft")
	if err != nil {
		return errors.New("kill switch needs nftables: install the nftables package")
	}
	_, err = runInput(nftRules(c), nft, "-f", "-")
	return err
}

// KillSwitchOff removes the kill switch; nothing to do if it is off.
func KillSwitchOff(Config) error {
	nft, err := lookPath("nft")
	if err != nil {
		return nil // never turned on without nft
	}
	_, err = runInput("add table "+nftTable+"\ndelete table "+nftTable+"\n", nft, "-f", "-")
	return err
}

// Changed reports whether the network moved under a running tunnel so it
// must be restarted. Marks do not depend on the interface: never on Linux.
func Changed(map[string]any) bool { return false }
