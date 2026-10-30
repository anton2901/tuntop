// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Commands are absolute paths: the service runs as root with a minimal PATH.
const (
	cmdRoute       = "/sbin/route"
	cmdNetstat     = "/usr/sbin/netstat"
	cmdNetworksetp = "/usr/sbin/networksetup"
	cmdDscache     = "/usr/bin/dscacheutil"
	cmdKillall     = "/usr/bin/killall"
	cmdPfctl       = "/sbin/pfctl"
)

// Test hooks: run executes a command, ifaceUp reports an interface exists.
var (
	run = func(name string, args ...string) (string, error) {
		out, err := exec.Command(name, args...).CombinedOutput()
		if err != nil {
			return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	ifaceUp = func(name string) bool {
		_, err := net.InterfaceByName(name)
		return err == nil
	}
)

// splitRoutes cover all addresses but lose to more specific routes (the LAN)
// and to the system's default route only in specificity, so removing them
// restores normal routing exactly.
var splitRoutes = []struct{ family, net string }{
	{"-inet", "0.0.0.0/1"},
	{"-inet", "128.0.0.0/1"},
	{"-inet6", "::/1"},
	{"-inet6", "8000::/1"},
}

// dnsBackup is what Setup changed in the system DNS, saved for restore.
type dnsBackup struct {
	Service string   `json:"service"`
	Servers []string `json:"servers"` // empty: DHCP ("Empty")
}

func backupPath(c Config) string { return filepath.Join(c.StateDir, "dns-restore.json") }

// scopedPath records the scoped default route Setup added: the interface.
func scopedPath(c Config) string { return filepath.Join(c.StateDir, "scoped-route") }

// defaultIface is the physical interface carrying the default route.
func defaultIface(c Config) (string, error) {
	iface, _, err := defaultRoute(c)
	return iface, err
}

// defaultRoute is the physical interface and gateway of the default route.
func defaultRoute(c Config) (iface, gateway string, err error) {
	out, err := run(cmdNetstat, "-rn", "-f", "inet")
	if err != nil {
		return "", "", err
	}
	return parseDefaultRoute(out, c.Iface)
}

// Bypass binds Xray's sockets to the current physical interface.
func Bypass(c Config) (map[string]any, error) {
	iface, err := defaultIface(c)
	if err != nil {
		return nil, err
	}
	return map[string]any{"interface": iface}, nil
}

// Changed reports that the physical interface differs from the one the
// tunnel's sockets are bound to (Wi-Fi ↔ cable): the tunnel must restart.
func Changed(bypass map[string]any) bool {
	bound, _ := bypass["interface"].(string)
	now, err := defaultIface(Default)
	return err == nil && bound != "" && now != bound
}

// Setup routes everything through c.Iface and points DNS at the TUN.
func Setup(c Config) (cleanup func() error, err error) {
	deadline := time.Now().Add(5 * time.Second)
	for !ifaceUp(c.Iface) {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("interface %s did not appear", c.Iface)
		}
		time.Sleep(100 * time.Millisecond)
	}
	phys, gw, err := defaultRoute(c)
	if err != nil {
		return nil, err
	}
	_ = deleteRoutes(c) // leftovers of a crashed run
	if err := addScopedDefault(c, phys, gw); err != nil {
		return nil, err
	}

	var added []int
	undoRoutes := func() error {
		var errs []error
		for _, i := range added {
			r := splitRoutes[i]
			// When Xray died first, the routes went with its utun.
			if _, err := run(cmdRoute, "-n", "delete", r.family, "-net", r.net, "-interface", c.Iface); err != nil && !routeGone(err) {
				errs = append(errs, err)
			}
		}
		errs = append(errs, removeScopedDefault(c))
		return errors.Join(errs...)
	}
	for i, r := range splitRoutes {
		if _, err := run(cmdRoute, "-n", "add", r.family, "-net", r.net, "-interface", c.Iface); err != nil {
			if r.family == "-inet6" {
				continue // no IPv6 on this network: IPv4 still works
			}
			_ = undoRoutes()
			return nil, err
		}
		added = append(added, i)
	}

	if err := setDNS(c, phys); err != nil {
		_ = undoRoutes()
		return nil, err
	}
	return func() error {
		return errors.Join(undoRoutes(), restoreDNS(c))
	}, nil
}

// setDNS saves the primary service's DNS and replaces it with the TUN's.
func setDNS(c Config, phys string) error {
	list, err := run(cmdNetworksetp, "-listnetworkserviceorder")
	if err != nil {
		return err
	}
	service, err := parseServiceForDevice(list, phys)
	if err != nil {
		return err
	}
	cur, err := run(cmdNetworksetp, "-getdnsservers", service)
	if err != nil {
		return err
	}
	b := dnsBackup{Service: service, Servers: parseDNSServers(cur)}
	// A previous crash left our own address in place: keep the original.
	if old, ok := readBackup(c); ok && old.Service == service {
		b = old
	}
	raw, _ := json.Marshal(b)
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(backupPath(c), raw, 0o600); err != nil {
		return fmt.Errorf("save DNS backup: %w", err)
	}
	if _, err := run(cmdNetworksetp, "-setdnsservers", service, c.DNS.String()); err != nil {
		return err
	}
	flushDNS()
	return nil
}

func readBackup(c Config) (dnsBackup, bool) {
	raw, err := os.ReadFile(backupPath(c))
	if err != nil {
		return dnsBackup{}, false
	}
	var b dnsBackup
	return b, json.Unmarshal(raw, &b) == nil && b.Service != ""
}

// restoreDNS puts back the saved DNS setting, if any.
func restoreDNS(c Config) error {
	b, ok := readBackup(c)
	if !ok {
		return nil
	}
	args := []string{"-setdnsservers", b.Service}
	if len(b.Servers) == 0 {
		args = append(args, "Empty")
	} else {
		args = append(args, b.Servers...)
	}
	if _, err := run(cmdNetworksetp, args...); err != nil {
		return err
	}
	flushDNS()
	return os.Remove(backupPath(c))
}

func flushDNS() {
	_, _ = run(cmdDscache, "-flushcache")
	_, _ = run(cmdKillall, "-HUP", "mDNSResponder")
}

func deleteRoutes(c Config) error {
	var errs []error
	for _, r := range splitRoutes {
		if _, err := run(cmdRoute, "-n", "delete", r.family, "-net", r.net, "-interface", c.Iface); err != nil && !routeGone(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// addScopedDefault gives the physical interface a scoped default route.
// Xray's sockets are bound to it (IP_BOUND_IF), and a bound socket looks up
// routes scoped to its interface. The primary interface's default route is
// not scoped: such a lookup then finds our 128.0.0.0/1 on the utun, which is
// the wrong interface, and connect fails with "network is unreachable".
// macOS adds scoped defaults only for non-primary interfaces; if one is
// already there it is the system's, and stays.
func addScopedDefault(c Config, iface, gw string) error {
	args := []string{"-n", "add", "-ifscope", iface, "default"}
	if strings.HasPrefix(gw, "link#") {
		args = append(args, "-interface", iface)
	} else {
		args = append(args, gw)
	}
	if _, err := run(cmdRoute, args...); err != nil {
		if strings.Contains(err.Error(), "File exists") {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(scopedPath(c), []byte(iface), 0o600)
}

// removeScopedDefault deletes the route addScopedDefault added, if any.
func removeScopedDefault(c Config) error {
	raw, err := os.ReadFile(scopedPath(c))
	if err != nil {
		return nil
	}
	iface := strings.TrimSpace(string(raw))
	if _, err := run(cmdRoute, "-n", "delete", "-ifscope", iface, "default"); err != nil &&
		!strings.Contains(err.Error(), "not in table") {
		return err
	}
	return os.Remove(scopedPath(c))
}

// routeGone reports a route delete that failed because there is nothing
// to delete: no such route, or no such interface any more (route -inet
// says "bad address", route -inet6 "nodename nor servname").
func routeGone(err error) bool {
	s := err.Error()
	return strings.Contains(s, "not in table") || strings.Contains(s, "bad address") ||
		strings.Contains(s, "nodename nor servname")
}

// Recover undoes what a crashed run left: DNS first, it breaks browsing.
// The split routes went away with the utun; a scoped default would stay
// and could point at an old gateway. So would the kill switch.
func Recover(c Config) error {
	return errors.Join(restoreDNS(c), removeScopedDefault(c), KillSwitchOff(c))
}

// pfAnchor is where the kill switch rules live; the default /etc/pf.conf
// evaluates every "com.apple/*" anchor.
const pfAnchor = "com.apple/tuntop"

// pfTokenPath keeps the reference pf gave us when we enabled it; pf stays
// enabled while any holder (other VPNs, the system) keeps one.
func pfTokenPath(c Config) string { return filepath.Join(c.StateDir, "pf-token") }

// KillSwitchOn loads the kill switch into pf and enables pf.
func KillSwitchOn(c Config) error {
	rules, err := run(cmdPfctl, "-s", "rules")
	if err != nil {
		return err
	}
	if !strings.Contains(rules, `anchor "com.apple/*"`) {
		return errors.New(`kill switch: pf does not evaluate "com.apple/*" anchors (custom /etc/pf.conf?)`)
	}
	if _, err := runInput(pfRules(c), cmdPfctl, "-a", pfAnchor, "-f", "-"); err != nil {
		return err
	}
	if _, err := os.Stat(pfTokenPath(c)); err == nil {
		return nil // enabled earlier, reference still held
	}
	out, err := run(cmdPfctl, "-E")
	if err != nil {
		_, _ = run(cmdPfctl, "-a", pfAnchor, "-F", "all")
		return err
	}
	token := parsePfToken(out)
	if token == "" {
		_, _ = run(cmdPfctl, "-a", pfAnchor, "-F", "all")
		return fmt.Errorf("pfctl -E: no token in %q", strings.TrimSpace(out))
	}
	if err := os.MkdirAll(c.StateDir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(pfTokenPath(c), []byte(token), 0o600)
}

// KillSwitchOff flushes the anchor and releases our pf reference.
// The token file marks pf as enabled by us. Without it (lost, or never
// written after a crash) rules left in the anchor are still removed, or
// the machine would stay cut off until someone runs pfctl by hand.
func KillSwitchOff(c Config) error {
	raw, err := os.ReadFile(pfTokenPath(c))
	if err != nil {
		if out, qerr := run(cmdPfctl, "-a", pfAnchor, "-s", "rules"); qerr == nil && strings.TrimSpace(out) != "" {
			_, ferr := run(cmdPfctl, "-a", pfAnchor, "-F", "all")
			return ferr
		}
		return nil
	}
	var errs []error
	if _, err := run(cmdPfctl, "-a", pfAnchor, "-F", "all"); err != nil {
		errs = append(errs, err)
	}
	if _, err := run(cmdPfctl, "-X", strings.TrimSpace(string(raw))); err != nil {
		errs = append(errs, err)
	}
	_ = os.Remove(pfTokenPath(c))
	return errors.Join(errs...)
}

// RemoveLink waits briefly for the utun to go away after Xray closes it;
// utun devices disappear with their file descriptor.
func RemoveLink(name string) error {
	for i := 0; i < 40 && ifaceUp(name); i++ {
		time.Sleep(50 * time.Millisecond)
	}
	if ifaceUp(name) {
		return fmt.Errorf("interface %s still present", name)
	}
	return nil
}
