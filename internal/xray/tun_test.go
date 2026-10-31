// SPDX-License-Identifier: GPL-3.0-or-later

package xray

import (
	"encoding/json"
	"testing"
)

const providerProfile = `{
  "remarks": "🇳🇱 NL", "meta": {},
  "inbounds": [{"tag": "socks", "port": 10808, "protocol": "socks"}],
  "outbounds": [
    {"tag": "proxy", "protocol": "vless",
     "settings": {"vnext": [{"address": "nl.example.com", "port": 443,
       "users": [{"id": "00000000-0000-0000-0000-000000000001", "encryption": "none"}]}]},
     "streamSettings": {"network": "tcp", "sockopt": {"tcpFastOpen": true}}},
    {"tag": "direct", "protocol": "freedom"},
    {"tag": "block", "protocol": "blackhole"}
  ],
  "routing": {"rules": [{"type": "field", "ip": ["1.2.3.4"], "outboundTag": "direct"}]}
}`

func TestWithTUN(t *testing.T) {
	raw, err := WithTUN([]byte(providerProfile), TUNOptions{Name: "tuntop0", MTU: 1500, Bypass: map[string]any{"mark": 2023}, LogLevel: "none"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Inbounds []struct {
			Tag, Protocol string
			Settings      map[string]any
		}
		Outbounds []struct {
			Tag, Protocol  string
			StreamSettings struct {
				Sockopt map[string]any
			}
		}
		Routing struct{ Rules []map[string]any }
		DNS     struct{ Servers []string }
		Remarks *string
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Inbounds) != 1 || doc.Inbounds[0].Protocol != "tun" || doc.Inbounds[0].Settings["name"] != "tuntop0" {
		t.Errorf("inbounds = %+v", doc.Inbounds)
	}
	if len(doc.Outbounds) != 4 || doc.Outbounds[3].Protocol != "dns" {
		t.Fatalf("outbounds = %+v", doc.Outbounds)
	}
	for _, o := range doc.Outbounds {
		if o.StreamSettings.Sockopt["mark"] != float64(2023) {
			t.Errorf("%s: sockopt = %v, want mark 2023", o.Tag, o.StreamSettings.Sockopt)
		}
	}
	if doc.Outbounds[0].StreamSettings.Sockopt["tcpFastOpen"] != true {
		t.Error("existing sockopt fields were dropped")
	}
	if r := doc.Routing.Rules; len(r) != 2 || r[0]["outboundTag"] != "dns-out" || r[1]["outboundTag"] != "direct" {
		t.Errorf("rules = %v", r)
	}
	if len(doc.DNS.Servers) == 0 || doc.Remarks != nil {
		t.Errorf("dns = %v, remarks = %v", doc.DNS.Servers, doc.Remarks)
	}
	if _, err := BuildJSON(raw); err != nil {
		t.Fatalf("xray rejected TUN config: %v", err)
	}
}

func TestWithTUNKeepsProviderDNS(t *testing.T) {
	p := `{"outbounds": [{"protocol": "freedom"}, {"tag": "mydns", "protocol": "dns"}],
	       "dns": {"servers": ["192.0.2.53"]}}`
	raw, err := WithTUN([]byte(p), TUNOptions{Name: "tuntop0", Bypass: map[string]any{"mark": 1}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []map[string]any
		Routing   struct{ Rules []map[string]any }
		DNS       struct{ Servers []string }
	}
	_ = json.Unmarshal(raw, &doc)
	if len(doc.Outbounds) != 2 || doc.Routing.Rules[0]["outboundTag"] != "mydns" {
		t.Errorf("existing dns outbound not reused: %+v / %+v", doc.Outbounds, doc.Routing.Rules)
	}
	if len(doc.DNS.Servers) != 1 || doc.DNS.Servers[0] != "192.0.2.53" {
		t.Errorf("provider DNS replaced: %v", doc.DNS.Servers)
	}
}

func TestLinkProfile(t *testing.T) {
	raw, err := LinkProfile(Outbound{Protocol: "vless", Settings: VLESSSettings{
		Address: "srv.example.com", Port: 443, ID: "00000000-0000-0000-0000-000000000001", Encryption: "none",
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := WithTUN(raw, TUNOptions{Name: "tuntop0", Bypass: map[string]any{"mark": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildJSON(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestWithTUNUpgradesPublicDNSToDoH(t *testing.T) {
	p := `{"outbounds": [{"protocol": "freedom"}],
	       "dns": {"servers": [
	         {"address": "77.88.8.8", "port": 53, "domains": ["regexp:.*\\.ru$"]},
	         "1.1.1.1",
	         {"address": "8.8.8.8", "port": 53},
	         "https://dns.google/dns-query"]}}`
	raw, err := WithTUN([]byte(p), TUNOptions{Name: "tuntop0", Bypass: map[string]any{"mark": 1}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ DNS struct{ Servers []any } }
	_ = json.Unmarshal(raw, &doc)
	s := doc.DNS.Servers
	if s[0].(map[string]any)["address"] != "77.88.8.8" {
		t.Errorf("domestic resolver changed: %v", s[0])
	}
	if s[1] != "https://1.1.1.1/dns-query" {
		t.Errorf("plain 1.1.1.1 not upgraded: %v", s[1])
	}
	if m := s[2].(map[string]any); m["address"] != "https://8.8.8.8/dns-query" || m["port"] != nil {
		t.Errorf("8.8.8.8 object not upgraded: %v", m)
	}
	if s[3] != "https://dns.google/dns-query" {
		t.Errorf("DoH entry changed: %v", s[3])
	}
	if _, err := BuildJSON(raw); err != nil {
		t.Fatal(err)
	}
}

func TestWithBypass(t *testing.T) {
	raw, err := WithBypass([]byte(providerProfile), map[string]any{"mark": 7})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Inbounds  []any
		Outbounds []struct {
			StreamSettings struct{ Sockopt map[string]any }
		}
	}
	_ = json.Unmarshal(raw, &doc)
	for _, o := range doc.Outbounds {
		if o.StreamSettings.Sockopt["mark"] != float64(7) {
			t.Fatalf("outbound not marked: %v", o.StreamSettings.Sockopt)
		}
	}
	if len(doc.Inbounds) != 1 {
		t.Fatal("WithBypass must not touch inbounds")
	}
}

func TestWithSOCKSUpgradesPublicDNSToDoH(t *testing.T) {
	p := `{"outbounds": [{"protocol": "freedom"}], "dns": {"servers": ["8.8.8.8", "77.88.8.8"]}}`
	raw, err := WithSOCKS([]byte(p), "127.0.0.1", 1080, "none")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct{ DNS struct{ Servers []string } }
	_ = json.Unmarshal(raw, &doc)
	if doc.DNS.Servers[0] != "https://8.8.8.8/dns-query" || doc.DNS.Servers[1] != "77.88.8.8" {
		t.Fatalf("servers = %v", doc.DNS.Servers)
	}
}

func TestWithTUNInterfaceBypass(t *testing.T) {
	raw, err := WithTUN([]byte(providerProfile), TUNOptions{Name: "utun227", Bypass: map[string]any{"interface": "en0"}})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Outbounds []struct {
			StreamSettings struct{ Sockopt map[string]any }
		}
	}
	_ = json.Unmarshal(raw, &doc)
	for _, o := range doc.Outbounds {
		if o.StreamSettings.Sockopt["interface"] != "en0" || o.StreamSettings.Sockopt["mark"] != nil {
			t.Fatalf("sockopt = %v", o.StreamSettings.Sockopt)
		}
	}
	if _, err := BuildJSON(raw); err != nil {
		t.Fatal(err)
	}
}

func TestServerDomainsAndPinnedHosts(t *testing.T) {
	p := `{"outbounds": [
	  {"tag": "a", "protocol": "vless", "settings": {"vnext": [{"address": "nl.example.com", "port": 443,
	     "users": [{"id": "00000000-0000-0000-0000-000000000001", "encryption": "none"}]}]}},
	  {"tag": "b", "protocol": "trojan", "settings": {"servers": [{"address": "192.0.2.7", "port": 443, "password": "x"}]}},
	  {"tag": "c", "protocol": "vless", "settings": {"address": "de.example.com", "port": 443,
	     "id": "00000000-0000-0000-0000-000000000001", "encryption": "none"}},
	  {"tag": "direct", "protocol": "freedom", "settings": {"address": "ignored.example.com"}}
	]}`
	got := ServerDomains([]byte(p))
	if len(got) != 2 || got[0] != "nl.example.com" || got[1] != "de.example.com" {
		t.Fatalf("domains = %v", got)
	}
	raw, err := WithTUN([]byte(p), TUNOptions{
		Name: "tuntop0", Bypass: map[string]any{"mark": 1},
		Hosts: map[string][]string{"nl.example.com": {"192.0.2.1"}, "de.example.com": {"192.0.2.2", "192.0.2.3"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		DNS       struct{ Hosts map[string][]string }
		Outbounds []struct {
			Tag            string
			StreamSettings struct{ Sockopt map[string]any }
		}
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.DNS.Hosts["de.example.com"]) != 2 {
		t.Fatalf("hosts = %v", doc.DNS.Hosts)
	}
	for _, o := range doc.Outbounds {
		want := o.Tag == "a" || o.Tag == "c"
		if got := o.StreamSettings.Sockopt["domainStrategy"] == "UseIP"; got != want {
			t.Errorf("%s: domainStrategy set = %v, want %v", o.Tag, got, want)
		}
	}
	if _, err := BuildJSON(raw); err != nil {
		t.Fatal(err)
	}
}

func TestWithLocalProxyBuilds(t *testing.T) {
	prof, err := LinkProfile(Outbound{Protocol: "freedom"})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := WithLocalProxy(prof, "127.0.0.1", 17900, 17901, "none")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := BuildJSON(raw); err != nil {
		t.Fatalf("xray rejected the config: %v\n%s", err, raw)
	}
}

func TestAccessLogIsOff(t *testing.T) {
	raw, err := WithTUN([]byte(providerProfile), TUNOptions{Name: "tuntop0", MTU: 1500, Bypass: map[string]any{"mark": 2023}, LogLevel: "warning"})
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Log struct{ Access string }
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Log.Access != "none" {
		t.Fatalf("access log = %q: destinations would land in the service log", doc.Log.Access)
	}
}
