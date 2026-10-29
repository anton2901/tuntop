// SPDX-License-Identifier: GPL-3.0-or-later

package netcfg

import (
	"reflect"
	"strings"
	"testing"
)

// Shaped like real macOS output; addresses are documentation ranges.
const netstatOtherVPN = `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
default            link#41            UCSg               utun25
default            192.0.2.1          UGScIg                en0
10                 192.0.2.1          UGSc                  en0
`

const netstatPlain = `Routing tables

Internet:
Destination        Gateway            Flags               Netif Expire
default            192.0.2.1          UGScg                 en5
default            192.0.2.1          UGScIg                en0
0/1                link#50            UCSc              utun227
`

func TestParseDefaultIface(t *testing.T) {
	if _, _, err := parseDefaultRoute(netstatOtherVPN, "utun227"); err == nil || !strings.Contains(err.Error(), "utun25") {
		t.Fatalf("other VPN not detected: %v", err)
	}
	if got, gw, err := parseDefaultRoute(netstatPlain, "utun227"); err != nil || got != "en5" || gw != "192.0.2.1" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, _, err := parseDefaultRoute("Routing tables\n", "utun227"); err == nil {
		t.Fatal("no default route accepted")
	}
}

const serviceOrder = `An asterisk (*) denotes that a network service is disabled.
(1) AX88179A
(Hardware Port: AX88179A, Device: en5)

(2) Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: bridge0)

(3) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)

(*) Old VPN
(Hardware Port: com.example.vpn, Device: )
`

func TestParseServiceForDevice(t *testing.T) {
	for dev, want := range map[string]string{"en0": "Wi-Fi", "en5": "AX88179A"} {
		if got, err := parseServiceForDevice(serviceOrder, dev); err != nil || got != want {
			t.Errorf("%s: got %q, %v", dev, got, err)
		}
	}
	if _, err := parseServiceForDevice(serviceOrder, "en9"); err == nil {
		t.Error("unknown device accepted")
	}
}

func TestParseDNSServers(t *testing.T) {
	if got := parseDNSServers("There aren't any DNS Servers set on Wi-Fi.\n"); got != nil {
		t.Errorf("DHCP: %v", got)
	}
	if got := parseDNSServers("192.0.2.53\n2001:db8::53\n"); !reflect.DeepEqual(got, []string{"192.0.2.53", "2001:db8::53"}) {
		t.Errorf("manual: %v", got)
	}
}
