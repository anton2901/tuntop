// SPDX-License-Identifier: GPL-3.0-or-later

package xray

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// InboundTag is the tag of tuntop's SOCKS inbound. Happ uses the same tag, so
// provider routing rules that match on inboundTag keep working.
const InboundTag = "socks"

// WithSOCKS takes a complete Xray JSON config from a provider (a
// "metaprofile" with its own balancers, observatory and routing) and
// replaces its inbounds with tuntop's single SOCKS inbound. Everything else
// is kept as the provider wrote it, except plain public DNS resolvers,
// which are upgraded to DoH (see upgradeToDoH).
func WithSOCKS(raw []byte, host string, port uint16, logLevel string) ([]byte, error) {
	return withInbounds(raw, []Inbound{socksInbound(host, port)}, logLevel)
}

// HTTPInboundTag is the tag of the HTTP inbound added by WithLocalProxy.
const HTTPInboundTag = "http"

// WithLocalProxy is WithSOCKS with an HTTP proxy inbound next to the SOCKS
// one, for the local proxy mode.
func WithLocalProxy(raw []byte, host string, socksPort, httpPort uint16, logLevel string) ([]byte, error) {
	return withInbounds(raw, []Inbound{
		socksInbound(host, socksPort),
		{Tag: HTTPInboundTag, Listen: host, Port: httpPort, Protocol: "http", Settings: map[string]any{}},
	}, logLevel)
}

func withInbounds(raw []byte, inbounds []Inbound, logLevel string) ([]byte, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse profile: %w", err)
	}
	if _, ok := doc["outbounds"]; !ok {
		return nil, errors.New("profile has no outbounds")
	}
	doc["inbounds"] = inbounds
	doc["log"] = Log{Access: "none", Level: logLevel}
	enableStats(doc)
	// With domainStrategy IPIfNonMatch Xray resolves destinations itself,
	// through the profile's DNS: same UDP-over-balancer stall as under TUN.
	if dns, _ := doc["dns"].(map[string]any); dns != nil {
		if servers, _ := dns["servers"].([]any); len(servers) > 0 {
			dns["servers"] = upgradeToDoH(servers)
		}
	}
	// Client-specific metadata (Happ) that Xray does not know.
	delete(doc, "remarks")
	delete(doc, "meta")
	return json.Marshal(doc)
}

func socksInbound(host string, port uint16) Inbound {
	return Inbound{
		Tag:      InboundTag,
		Listen:   host,
		Port:     port,
		Protocol: "socks",
		Settings: SOCKSInboundSettings{Auth: "noauth", UDP: true},
	}
}

// assetDirs are searched for geoip.dat / geosite.dat in order. Xray itself
// only looks in .../xray; distro packages (Arch v2ray-geoip, Homebrew) use
// .../v2ray.
var assetDirs = []string{
	"/usr/local/share/tuntop", // macOS installer puts them here
	"/usr/share/tuntop",
	"/usr/local/share/xray",
	"/usr/share/xray",
	"/opt/share/xray",
	"/usr/share/v2ray",
	"/usr/local/share/v2ray",
	"/opt/homebrew/share/v2ray",
}

// SetupAssets points Xray at a directory with geoip.dat and geosite.dat,
// which provider routing rules (geoip:, geosite:) require. An explicit
// XRAY_LOCATION_ASSET wins. Returns the directory used, or "" if none.
func SetupAssets() string {
	for _, env := range []string{"xray.location.asset", "XRAY_LOCATION_ASSET"} {
		if v, ok := os.LookupEnv(env); ok {
			return v
		}
	}
	dirs := assetDirs
	if exe, err := os.Executable(); err == nil {
		dirs = append([]string{filepath.Dir(exe)}, dirs...)
	}
	for _, d := range dirs {
		if hasGeoData(d) {
			_ = os.Setenv("XRAY_LOCATION_ASSET", d)
			return d
		}
	}
	return ""
}

// hasGeoData reports whether dir holds both geo databases.
func hasGeoData(dir string) bool {
	for _, f := range []string{"geoip.dat", "geosite.dat"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// enableStats turns on Xray's traffic counters, read with
// Instance.Traffic. The provider's own policy levels are kept.
func enableStats(doc map[string]any) {
	doc["stats"] = map[string]any{}
	policy, _ := doc["policy"].(map[string]any)
	if policy == nil {
		policy = map[string]any{}
		doc["policy"] = policy
	}
	sys, _ := policy["system"].(map[string]any)
	if sys == nil {
		sys = map[string]any{}
		policy["system"] = sys
	}
	sys["statsOutboundUplink"] = true
	sys["statsOutboundDownlink"] = true
}
