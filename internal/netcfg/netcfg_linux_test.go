// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// fakeLinux replaces netlink and commands: these tests never touch the
// network of the machine they run on (they run on a test host as root
// too, see scripts/test-remote.sh).
type fakeLinux struct {
	link   netlink.Link
	addr   string
	up     bool
	routes []*netlink.Route
	rules  []netlink.Rule
	cmds   []string
	stdin  []string
	noNft  bool
}

func (f *fakeLinux) install(t *testing.T, resolved bool) {
	t.Helper()
	oldNl, oldLook, oldIn, oldStub, oldProc := nl, lookPath, runInput, resolvedStub, procSys
	t.Cleanup(func() { nl, lookPath, runInput, resolvedStub, procSys = oldNl, oldLook, oldIn, oldStub, oldProc })

	dir := t.TempDir()
	procSys = filepath.Join(dir, "proc")
	if err := os.MkdirAll(filepath.Join(procSys, "net/ipv4/conf/tuntop0"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolvedStub = filepath.Join(dir, "stub-resolv.conf")
	if resolved {
		if err := os.WriteFile(resolvedStub, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	f.link = &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: "tuntop0", Index: 7}}
	// A rule of the system's own, outside our range: must survive.
	f.rules = []netlink.Rule{{Priority: 32766, Table: mainTable}}

	nl.linkByName = func(name string) (netlink.Link, error) {
		if name == f.link.Attrs().Name {
			return f.link, nil
		}
		return nil, errors.New("Link not found")
	}
	nl.linkSetUp = func(netlink.Link) error { f.up = true; return nil }
	nl.linkDel = func(netlink.Link) error { return nil }
	nl.addrReplace = func(_ netlink.Link, a *netlink.Addr) error { f.addr = a.IPNet.String(); return nil }
	nl.routeReplace = func(r *netlink.Route) error { f.routes = append(f.routes, r); return nil }
	nl.ruleAdd = func(r *netlink.Rule) error { f.rules = append(f.rules, *r); return nil }
	nl.ruleList = func(int) ([]netlink.Rule, error) { return append([]netlink.Rule(nil), f.rules...), nil }
	nl.ruleDel = func(r *netlink.Rule) error {
		for i := range f.rules {
			if f.rules[i].Priority == r.Priority && f.rules[i].Table == r.Table {
				f.rules = append(f.rules[:i], f.rules[i+1:]...)
				return nil
			}
		}
		return errors.New("no such rule")
	}
	lookPath = func(name string) (string, error) {
		if f.noNft {
			return "", errors.New("not found")
		}
		return "/usr/bin/" + name, nil
	}
	runInput = func(stdin, name string, args ...string) (string, error) {
		f.cmds = append(f.cmds, name+" "+strings.Join(args, " "))
		f.stdin = append(f.stdin, stdin)
		return "", nil
	}
}

func (f *fakeLinux) rulesAt(pref int) []netlink.Rule {
	var out []netlink.Rule
	for _, r := range f.rules {
		if r.Priority == pref {
			out = append(out, r)
		}
	}
	return out
}

func TestLinuxSetupAndUndo(t *testing.T) {
	f := &fakeLinux{}
	f.install(t, true)
	c := Default
	undo, err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}
	if f.addr != "172.23.0.1/30" || !f.up {
		t.Fatalf("addr %q up %v", f.addr, f.up)
	}
	if len(f.routes) != 1 || f.routes[0].Table != 2023 || f.routes[0].LinkIndex != 7 || f.routes[0].Dst.String() != "0.0.0.0/0" {
		t.Fatalf("routes = %+v", f.routes)
	}
	// Xray's own marked sockets use the main table.
	if r := f.rulesAt(9100); len(r) != 1 || r[0].Mark != 2023 || r[0].Table != mainTable {
		t.Fatalf("mark rule = %+v", r)
	}
	// DNS goes into the tunnel even to LAN resolvers.
	if r := f.rulesAt(9101); len(r) != 1 || r[0].IPProto != unix.IPPROTO_UDP || r[0].Dport.Start != 53 || r[0].Table != 2023 {
		t.Fatalf("dns rule = %+v", r)
	}
	if r := f.rulesAt(9102); len(r) != len(bypass) || r[0].Table != mainTable {
		t.Fatalf("bypass rules = %d", len(r))
	}
	if r := f.rulesAt(9109); len(r) != 1 || r[0].Table != 2023 || r[0].Mark != 0 || r[0].Dst != nil {
		t.Fatalf("catch-all rule = %+v", r)
	}
	if got, _ := os.ReadFile(filepath.Join(procSys, "net/ipv4/conf/tuntop0/rp_filter")); string(got) != "2" {
		t.Fatalf("rp_filter = %q", got)
	}
	want := []string{
		"resolvectl dns tuntop0 172.23.0.2",
		"resolvectl domain tuntop0 ~.",
		"resolvectl default-route tuntop0 yes",
	}
	if strings.Join(f.cmds, "\n") != strings.Join(want, "\n") {
		t.Fatalf("commands:\n%s", strings.Join(f.cmds, "\n"))
	}

	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if len(f.rules) != 1 || f.rules[0].Priority != 32766 {
		t.Fatalf("rules after undo = %+v", f.rules)
	}
}

func TestLinuxSetupWithoutResolved(t *testing.T) {
	f := &fakeLinux{}
	f.install(t, false)
	if _, err := Setup(Default); err != nil {
		t.Fatal(err)
	}
	if len(f.cmds) != 0 {
		t.Fatalf("resolvectl without resolved: %v", f.cmds)
	}
}

func TestLinuxSetupReplacesStaleRules(t *testing.T) {
	f := &fakeLinux{}
	f.install(t, false)
	// A crashed run left its catch-all rule.
	f.rules = append(f.rules, netlink.Rule{Priority: 9109, Table: 2023})
	if _, err := Setup(Default); err != nil {
		t.Fatal(err)
	}
	if n := len(f.rulesAt(9109)); n != 1 {
		t.Fatalf("catch-all rules = %d", n)
	}
}

func TestLinuxKillSwitch(t *testing.T) {
	f := &fakeLinux{}
	f.install(t, false)
	c := Default
	if err := KillSwitchOn(c); err != nil {
		t.Fatal(err)
	}
	if f.cmds[0] != "/usr/bin/nft -f -" || !strings.Contains(f.stdin[0], "meta mark 2023 accept") {
		t.Fatalf("on: %v\n%s", f.cmds, f.stdin[0])
	}
	f.cmds, f.stdin = nil, nil
	// Recover after a crash removes the rules and the table.
	f.rules = append(f.rules, netlink.Rule{Priority: 9100, Table: mainTable})
	if err := Recover(c); err != nil {
		t.Fatal(err)
	}
	if len(f.rulesAt(9100)) != 0 || len(f.stdin) != 1 || !strings.Contains(f.stdin[0], "delete table inet tuntop") {
		t.Fatalf("recover: rules %+v, nft %q", f.rules, f.stdin)
	}
}

func TestLinuxKillSwitchNeedsNft(t *testing.T) {
	f := &fakeLinux{noNft: true}
	f.install(t, false)
	if err := KillSwitchOn(Default); err == nil || !strings.Contains(err.Error(), "nftables") {
		t.Fatalf("err = %v", err)
	}
	if err := KillSwitchOff(Default); err != nil {
		t.Fatalf("off without nft: %v", err)
	}
}

func TestLinuxBypassIsTheMark(t *testing.T) {
	b, err := Bypass(Default)
	if err != nil || b["mark"] != 2023 {
		t.Fatalf("bypass = %v, %v", b, err)
	}
	if Changed(b) {
		t.Fatal("Changed is never true on Linux")
	}
}
