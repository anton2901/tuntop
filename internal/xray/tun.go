// SPDX-License-Identifier: GPL-3.0-or-later

package xray

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

// TUNTag is the TUN inbound's tag; routing rules and traffic counters
// refer to it.
const TUNTag = "tun"

// LinkProfile wraps a single outbound into a profile (a config without
// inbounds) with a direct outbound next to it.
func LinkProfile(out Outbound) ([]byte, error) {
	if out.Tag == "" {
		out.Tag = "proxy"
	}
	return json.Marshal(Config{Outbounds: []Outbound{
		out,
		{Tag: "direct", Protocol: "freedom"},
	}})
}

// TUNOptions configure WithTUN.
type TUNOptions struct {
	// Name of the TUN interface Xray creates, e.g. "tuntop0".
	Name string
	MTU  uint32
	// Bypass is merged into every outbound's sockopt so the OS routes
	// Xray's own traffic around the TUN: {"mark": N} on Linux,
	// {"interface": "en0"} on macOS (see netcfg.Bypass).
	Bypass   map[string]any
	LogLevel string
	// DNS servers used when the profile does not define its own. They
	// are reached through the profile's default (first) outbound.
	DNS []string
	// Hosts are proxy server domains resolved before the tunnel changed
	// the system DNS. Xray must not ask the system for them: the system
	// DNS points into the tunnel, which needs these very servers (a loop).
	Hosts map[string][]string
}

// DefaultDNS resolves over DoH by IP through the tunnel, so neither the
// ISP resolver nor its DNS spoofing are involved.
var DefaultDNS = []string{"https://1.1.1.1/dns-query", "https://8.8.8.8/dns-query"}

// WithTUN turns a profile into a config that reads traffic from a TUN
// interface:
//
//   - inbounds are replaced with a TUN inbound with sniffing, so the
//     provider's domain rules (geosite:…) still see host names;
//   - every outbound gets SO_MARK, the hook for loop-free OS routing;
//   - DNS packets arriving on the TUN go to Xray's DNS module (dns
//     outbound), which resolves via the profile's DNS servers or DefaultDNS.
func WithTUN(raw []byte, o TUNOptions) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	outs, _ := doc["outbounds"].([]any)
	if len(outs) == 0 {
		return nil, errors.New("profile has no outbounds")
	}

	doc["inbounds"] = []any{map[string]any{
		"tag":      TUNTag,
		"port":     0,
		"protocol": "tun",
		"settings": map[string]any{"name": o.Name, "MTU": o.MTU},
		"sniffing": map[string]any{
			"enabled":      true,
			"destOverride": []string{"http", "tls", "quic"},
			"routeOnly":    true,
		},
	}}

	setSockopt(outs, o.Bypass)
	dnsTag := ""
	for _, ob := range outs {
		if m, ok := ob.(map[string]any); ok && m["protocol"] == "dns" && dnsTag == "" {
			dnsTag, _ = m["tag"].(string)
		}
	}
	if dnsTag == "" {
		dnsTag = "dns-out"
		outs = append(outs, map[string]any{
			"tag": dnsTag, "protocol": "dns",
			"streamSettings": map[string]any{"sockopt": clone(o.Bypass)},
		})
	}
	doc["outbounds"] = outs

	routing, _ := doc["routing"].(map[string]any)
	if routing == nil {
		routing = map[string]any{}
		doc["routing"] = routing
	}
	rules, _ := routing["rules"].([]any)
	dnsRule := map[string]any{
		"type": "field", "inboundTag": []string{TUNTag},
		"network": "udp", "port": "53", "outboundTag": dnsTag,
	}
	routing["rules"] = append([]any{dnsRule}, rules...)

	dns, _ := doc["dns"].(map[string]any)
	if servers, _ := dns["servers"].([]any); len(servers) > 0 {
		dns["servers"] = upgradeToDoH(servers)
	} else {
		if dns == nil {
			dns = map[string]any{}
		}
		list := o.DNS
		if len(list) == 0 {
			list = DefaultDNS
		}
		dns["servers"] = list
		doc["dns"] = dns
	}

	if len(o.Hosts) > 0 {
		pinHosts(doc, outs, o.Hosts)
	}

	doc["log"] = Log{Access: "none", Level: o.LogLevel}
	enableStats(doc)
	delete(doc, "remarks")
	delete(doc, "meta")
	return json.Marshal(doc)
}

// publicDoH maps well-known public resolvers to their DoH endpoints.
var publicDoH = map[string]string{
	"1.1.1.1": "https://1.1.1.1/dns-query",
	"1.0.0.1": "https://1.0.0.1/dns-query",
	"8.8.8.8": "https://8.8.8.8/dns-query",
	"8.8.4.4": "https://8.8.4.4/dns-query",
	"9.9.9.9": "https://9.9.9.9/dns-query",
}

// upgradeToDoH rewrites plain-UDP public resolvers in a provider's DNS
// list to DoH. Profiles often send UDP DNS through a balancer, and UDP
// over many proxy servers silently fails, which stalls every lookup under
// TUN; DoH runs over TCP like ordinary traffic. Other entries (e.g. a
// domestic resolver for local domains) are left alone.
func upgradeToDoH(servers []any) []any {
	out := make([]any, 0, len(servers))
	for _, s := range servers {
		switch v := s.(type) {
		case string:
			if doh, ok := publicDoH[v]; ok {
				s = doh
			}
		case map[string]any:
			addr, _ := v["address"].(string)
			if doh, ok := publicDoH[addr]; ok {
				v["address"] = doh
				delete(v, "port")
			}
		}
		out = append(out, s)
	}
	return out
}

// setSockopt merges so into every outbound's sockopt.
func setSockopt(outs []any, so map[string]any) {
	for _, ob := range outs {
		m, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		ss, _ := m["streamSettings"].(map[string]any)
		if ss == nil {
			ss = map[string]any{}
			m["streamSettings"] = ss
		}
		sock, _ := ss["sockopt"].(map[string]any)
		if sock == nil {
			sock = map[string]any{}
			ss["sockopt"] = sock
		}
		for k, v := range so {
			sock[k] = v
		}
	}
}

func clone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// WithBypass returns the profile with bypass merged into all outbounds'
// sockopt, so its traffic goes around tuntop's TUN (used for latency
// probes and fallback downloads while connected).
func WithBypass(raw []byte, bypass map[string]any) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	outs, _ := doc["outbounds"].([]any)
	setSockopt(outs, bypass)
	return json.Marshal(doc)
}

// ServerDomains returns the domain names (not IPs) of the proxy servers in
// a profile, to resolve them before the tunnel starts.
func ServerDomains(raw []byte) []string {
	var doc struct {
		Outbounds []struct {
			Protocol string          `json:"protocol"`
			Settings json.RawMessage `json:"settings"`
		} `json:"outbounds"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, o := range doc.Outbounds {
		if !isProxy(o.Protocol) {
			continue
		}
		for _, a := range serverAddresses(o.Settings) {
			if net.ParseIP(a) == nil && a != "" && !seen[a] {
				seen[a] = true
				out = append(out, a)
			}
		}
	}
	return out
}

// serverAddresses lists server hosts in any of Xray's settings shapes:
// vnext (vless/vmess), servers (trojan/ss/socks) or flat.
func serverAddresses(raw json.RawMessage) []string {
	type host struct {
		Address string `json:"address"`
	}
	var s struct {
		host
		Vnext   []host `json:"vnext"`
		Servers []host `json:"servers"`
	}
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	list := []string{s.Address}
	for _, h := range append(s.Vnext, s.Servers...) {
		list = append(list, h.Address)
	}
	return list
}

func isProxy(protocol string) bool {
	switch protocol {
	case "freedom", "blackhole", "dns", "loopback", "":
		return false
	}
	return true
}

// pinHosts adds hosts to Xray's DNS hosts table and makes the proxy
// outbounds that use them resolve through Xray's DNS (which consults the
// table first) instead of the system resolver.
func pinHosts(doc map[string]any, outs []any, hosts map[string][]string) {
	dns, _ := doc["dns"].(map[string]any)
	if dns == nil {
		dns = map[string]any{}
		doc["dns"] = dns
	}
	table, _ := dns["hosts"].(map[string]any)
	if table == nil {
		table = map[string]any{}
		dns["hosts"] = table
	}
	for name, ips := range hosts {
		if len(ips) > 0 {
			table[name] = ips
		}
	}
	for _, ob := range outs {
		m, ok := ob.(map[string]any)
		if !ok {
			continue
		}
		proto, _ := m["protocol"].(string)
		if !isProxy(proto) || proto == "dns" {
			continue
		}
		raw, _ := json.Marshal(m["settings"])
		pinned := false
		for _, a := range serverAddresses(raw) {
			if len(hosts[a]) > 0 {
				pinned = true
			}
		}
		if !pinned {
			continue
		}
		setSockopt([]any{m}, map[string]any{"domainStrategy": "UseIP"})
	}
}
