// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"bufio"
	"fmt"
	"regexp"
	"strings"
)

// Parsers for macOS command output. They have no build tag so their tests
// run everywhere.

// tunnelIface reports interface names used by VPNs (utun, ipsec, ppp).
func tunnelIface(name string) bool {
	for _, p := range []string{"utun", "ipsec", "ppp", "tun", "tap"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// parseDefaultRoute reads `netstat -rn -f inet` and returns the interface
// and gateway of the first default route. If that route belongs to another VPN, it says
// so: two VPNs at once loop or leak, the other one must be turned off.
func parseDefaultRoute(netstat, ownIface string) (iface, gateway string, err error) {
	sc := bufio.NewScanner(strings.NewReader(netstat))
	netifCol, gwCol := -1, -1
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		if f[0] == "Destination" {
			for i, h := range f {
				switch h {
				case "Netif":
					netifCol = i
				case "Gateway":
					gwCol = i
				}
			}
			continue
		}
		if f[0] != "default" || netifCol < 0 || gwCol < 0 || len(f) <= netifCol {
			continue
		}
		iface = f[netifCol]
		if iface == ownIface {
			continue
		}
		if tunnelIface(iface) {
			return "", "", fmt.Errorf("another VPN owns the default route (%s): turn it off first", iface)
		}
		return iface, f[gwCol], nil
	}
	return "", "", fmt.Errorf("no default route: is the network connected?")
}

var (
	serviceRe = regexp.MustCompile(`^\(\d+\)\s+(.+)$`)
	deviceRe  = regexp.MustCompile(`Device:\s*([^)\s]+)\)`)
)

// parseServiceForDevice reads `networksetup -listnetworkserviceorder` and
// returns the network service name ("Wi-Fi") using device ("en0").
func parseServiceForDevice(out, device string) (string, error) {
	var service string
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if m := serviceRe.FindStringSubmatch(line); m != nil {
			service = strings.TrimPrefix(m[1], "*") // "*" marks disabled services
			continue
		}
		if m := deviceRe.FindStringSubmatch(line); m != nil && m[1] == device && service != "" {
			return service, nil
		}
	}
	return "", fmt.Errorf("no network service for %s", device)
}

// parsePfToken reads the reference token from `pfctl -E` output
// ("Token : 1234567890").
func parsePfToken(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.TrimSpace(k) == "Token" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// parseDNSServers reads `networksetup -getdnsservers <service>`; none set
// (DHCP-provided) gives an empty list.
func parseDNSServers(out string) []string {
	if strings.Contains(out, "aren't any DNS Servers") {
		return nil
	}
	return strings.Fields(out)
}
