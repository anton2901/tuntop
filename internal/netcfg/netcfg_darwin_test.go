// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// fakeMac replaces every system command: these tests never touch the
// network of the machine they run on.
type fakeMac struct {
	cmds      []string
	dns       []string // current DNS of the service; nil = DHCP
	netstat   string
	failInet6 bool
	// scopedExists: the system already has a scoped default (non-primary).
	scopedExists bool
	pfRules      string // `pfctl -s rules`
	stdin        string // last input to runInput
}

func (f *fakeMac) install(t *testing.T) {
	t.Helper()
	oldRun, oldUp, oldIn := run, ifaceUp, runInput
	t.Cleanup(func() { run, ifaceUp, runInput = oldRun, oldUp, oldIn })
	ifaceUp = func(string) bool { return true }
	runInput = func(stdin, name string, args ...string) (string, error) {
		f.cmds = append(f.cmds, name+" "+strings.Join(args, " "))
		f.stdin = stdin
		return "", nil
	}
	run = func(name string, args ...string) (string, error) {
		line := name + " " + strings.Join(args, " ")
		f.cmds = append(f.cmds, line)
		switch {
		case name == cmdNetstat:
			return f.netstat, nil
		case name == cmdPfctl && args[0] == "-s":
			return f.pfRules, nil
		case name == cmdPfctl && args[0] == "-E":
			return "pf enabled\nToken : 42\n", nil
		case name == cmdNetworksetp && args[0] == "-listnetworkserviceorder":
			return serviceOrder, nil
		case name == cmdNetworksetp && args[0] == "-getdnsservers":
			if f.dns == nil {
				return "There aren't any DNS Servers set on " + args[1] + ".\n", nil
			}
			return strings.Join(f.dns, "\n") + "\n", nil
		case name == cmdNetworksetp && args[0] == "-setdnsservers":
			f.dns = args[2:]
			if len(f.dns) == 1 && f.dns[0] == "Empty" {
				f.dns = nil
			}
			return "", nil
		case name == cmdRoute && f.scopedExists && args[1] == "add" && args[2] == "-ifscope":
			return "", errors.New("route: writing to routing socket: File exists")
		case name == cmdRoute && f.failInet6 && strings.Contains(line, "-inet6") && args[1] == "add":
			return "", errors.New("route: network unreachable")
		}
		return "", nil
	}
}

func testConfig(t *testing.T) Config {
	c := Default
	c.Iface = "utun227"
	c.StateDir = t.TempDir()
	return c
}

func countPrefix(cmds []string, prefix string) int {
	n := 0
	for _, c := range cmds {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

func TestSetupAndCleanup(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain, dns: []string{"192.0.2.53"}}
	f.install(t)
	c := testConfig(t)

	b, err := Bypass(c)
	if err != nil || b["interface"] != "en5" {
		t.Fatalf("bypass = %v, %v", b, err)
	}
	undo, err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}
	if n := countPrefix(f.cmds, cmdRoute+" -n add -ifscope en5 default 192.0.2.1"); n != 1 {
		t.Fatalf("no scoped default for the bound interface:\n%s", strings.Join(f.cmds, "\n"))
	}
	if n := countPrefix(f.cmds, cmdRoute+" -n add -"); n != 5 {
		t.Fatalf("routes added: %d\n%s", n, strings.Join(f.cmds, "\n"))
	}
	if len(f.dns) != 1 || f.dns[0] != "172.23.0.2" {
		t.Fatalf("DNS during tunnel = %v", f.dns)
	}
	if _, err := os.Stat(backupPath(c)); err != nil {
		t.Fatal("no DNS backup on disk")
	}

	f.cmds = nil
	if err := undo(); err != nil {
		t.Fatal(err)
	}
	if n := countPrefix(f.cmds, cmdRoute+" -n delete"); n != 5 {
		t.Fatalf("routes deleted: %d", n)
	}
	if _, err := os.Stat(scopedPath(c)); !os.IsNotExist(err) {
		t.Fatal("scoped route marker left behind")
	}
	if len(f.dns) != 1 || f.dns[0] != "192.0.2.53" {
		t.Fatalf("DNS not restored: %v", f.dns)
	}
	if _, err := os.Stat(backupPath(c)); !os.IsNotExist(err) {
		t.Fatal("backup left behind")
	}
}

func TestRecoverAfterCrash(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain} // DHCP DNS
	f.install(t)
	c := testConfig(t)
	if _, err := Setup(c); err != nil {
		t.Fatal(err)
	}
	// The service dies here without cleanup; on the next start:
	if err := Recover(c); err != nil {
		t.Fatal(err)
	}
	if f.dns != nil {
		t.Fatalf("DNS after recover = %v, want DHCP", f.dns)
	}
	if countPrefix(f.cmds, cmdRoute+" -n delete -ifscope en5 default") != 1 {
		t.Fatalf("scoped default not removed on recover:\n%s", strings.Join(f.cmds, "\n"))
	}
	// A second crash before recovery must not save our own address as the
	// "original" DNS.
	_, _ = Setup(c)
	_, _ = Setup(c)
	_ = Recover(c)
	if f.dns != nil {
		t.Fatalf("DNS after double setup = %v", f.dns)
	}
}

func TestRefusesAlongsideAnotherVPN(t *testing.T) {
	f := &fakeMac{netstat: netstatOtherVPN}
	f.install(t)
	c := testConfig(t)
	if _, err := Bypass(c); err == nil {
		t.Fatal("bypass with another VPN active")
	}
	if _, err := Setup(c); err == nil {
		t.Fatal("setup with another VPN active")
	}
	if countPrefix(f.cmds, cmdRoute) != 0 || countPrefix(f.cmds, cmdNetworksetp+" -setdnsservers") != 0 {
		t.Fatalf("changed the system anyway:\n%s", strings.Join(f.cmds, "\n"))
	}
}

func TestWorksWithoutIPv6(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain, failInet6: true}
	f.install(t)
	c := testConfig(t)
	undo, err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}
	f.cmds = nil
	_ = undo()
	if n := countPrefix(f.cmds, cmdRoute+" -n delete"); n != 3 {
		t.Fatalf("deleted %d routes, want the 2 IPv4 ones and the scoped default", n)
	}
}

func TestKeepsSystemScopedDefault(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain, scopedExists: true}
	f.install(t)
	c := testConfig(t)
	undo, err := Setup(c)
	if err != nil {
		t.Fatal(err)
	}
	f.cmds = nil
	_ = undo()
	_ = Recover(c)
	if n := countPrefix(f.cmds, cmdRoute+" -n delete -ifscope"); n != 0 {
		t.Fatalf("deleted the system's scoped default:\n%s", strings.Join(f.cmds, "\n"))
	}
}

func TestKillSwitchPf(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain, pfRules: "scrub-anchor \"com.apple/*\" all fragment reassemble\nanchor \"com.apple/*\" all\n"}
	f.install(t)
	c := testConfig(t)
	if err := KillSwitchOn(c); err != nil {
		t.Fatal(err)
	}
	if countPrefix(f.cmds, cmdPfctl+" -a com.apple/tuntop -f -") != 1 || !strings.Contains(f.stdin, "block return out quick all") {
		t.Fatalf("rules not loaded:\n%s", strings.Join(f.cmds, "\n"))
	}
	if countPrefix(f.cmds, cmdPfctl+" -E") != 1 {
		t.Fatal("pf not enabled")
	}
	// A second on (server switch) keeps one reference.
	_ = KillSwitchOn(c)
	if countPrefix(f.cmds, cmdPfctl+" -E") != 1 {
		t.Fatal("pf enabled twice")
	}
	f.cmds = nil
	// The service crashed; the next start recovers.
	if err := Recover(c); err != nil {
		t.Fatal(err)
	}
	if countPrefix(f.cmds, cmdPfctl+" -a com.apple/tuntop -F all") != 1 || countPrefix(f.cmds, cmdPfctl+" -X 42") != 1 {
		t.Fatalf("not released:\n%s", strings.Join(f.cmds, "\n"))
	}
	f.cmds = nil
	_ = KillSwitchOff(c)
	if len(f.cmds) != 0 {
		t.Fatalf("off twice touched pf: %v", f.cmds)
	}
}

func TestKillSwitchNeedsAppleAnchors(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain, pfRules: "block all\n"}
	f.install(t)
	if err := KillSwitchOn(testConfig(t)); err == nil {
		t.Fatal("kill switch on without the com.apple anchor")
	}
}

func TestRouteGone(t *testing.T) {
	for _, msg := range []string{
		"route: writing to routing socket: not in table",
		"route: bad address: utun227",
		"utun227: nodename nor servname provided, or not known",
	} {
		if !routeGone(errors.New(msg)) {
			t.Errorf("%q not taken as gone", msg)
		}
	}
	if routeGone(errors.New("route: permission denied")) {
		t.Error("a real failure taken as gone")
	}
}

func TestChanged(t *testing.T) {
	f := &fakeMac{netstat: netstatPlain}
	f.install(t)
	if Changed(map[string]any{"interface": "en5"}) {
		t.Fatal("same interface reported as changed")
	}
	if !Changed(map[string]any{"interface": "en0"}) {
		t.Fatal("switch to another interface not detected")
	}
}
