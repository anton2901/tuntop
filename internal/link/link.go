// SPDX-License-Identifier: GPL-3.0-or-later

// Package link parses share links (vless://, vmess://, trojan://, ss://, hysteria2://) into Xray outbounds.
package link

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/anton2901/tuntop/internal/safe"
	"github.com/anton2901/tuntop/internal/xray"
)

// Profile is one server parsed from a share link.
type Profile struct {
	// Name is the display name from the link fragment, or host:port.
	Name     string
	Outbound xray.Outbound
}

// ErrUnsupported is returned for schemes tuntop cannot handle yet.
var ErrUnsupported = errors.New("unsupported link scheme")

// Parse parses a single share link.
func Parse(raw string) (Profile, error) {
	raw = strings.TrimSpace(raw)
	scheme, _, ok := strings.Cut(raw, "://")
	if !ok {
		return Profile{}, errors.New("not a link: missing scheme")
	}
	p, err := parseScheme(strings.ToLower(scheme), raw)
	p.Name = safe.Text(p.Name)
	return p, err
}

func parseScheme(scheme, raw string) (Profile, error) {
	switch scheme {
	case "vless":
		return parseVLESS(raw)
	case "vmess":
		return parseVMess(raw)
	case "trojan":
		return parseTrojan(raw)
	case "ss":
		return parseShadowsocks(raw)
	case "hysteria2", "hy2":
		return parseHysteria2(raw)
	default:
		return Profile{}, fmt.Errorf("%w: %s", ErrUnsupported, scheme)
	}
}

func parseVLESS(raw string) (Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Profile{}, fmt.Errorf("parse vless link: %w", err)
	}
	id := u.User.Username()
	if id == "" {
		return Profile{}, errors.New("vless link: missing user id")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Profile{}, fmt.Errorf("vless link: %w", err)
	}
	q := u.Query()

	stream, err := streamSettings(q, host, "none")
	if err != nil {
		return Profile{}, fmt.Errorf("vless link: %w", err)
	}
	enc := q.Get("encryption")
	if enc == "" {
		enc = "none"
	}
	return Profile{
		Name: displayName(u, host, port),
		Outbound: xray.Outbound{
			Protocol: "vless",
			Settings: xray.VLESSSettings{
				Address:    host,
				Port:       port,
				ID:         id,
				Encryption: enc,
				Flow:       q.Get("flow"),
			},
			StreamSettings: stream,
		},
	}, nil
}

func hostPort(u *url.URL) (string, uint16, error) {
	host := u.Hostname()
	if host == "" {
		return "", 0, errors.New("missing host")
	}
	ps := u.Port()
	if ps == "" {
		return host, 443, nil
	}
	p, err := strconv.ParseUint(ps, 10, 16)
	if err != nil || p == 0 {
		return "", 0, fmt.Errorf("invalid port %q", ps)
	}
	return host, uint16(p), nil
}

func displayName(u *url.URL, host string, port uint16) string {
	if u.Fragment != "" {
		return u.Fragment
	}
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}

// streamSettings maps the de facto standard share-link query parameters
// (as used by v2rayN, Happ, Xray docs) to Xray streamSettings.
func streamSettings(q url.Values, host, defaultSecurity string) (*xray.StreamSettings, error) {
	network := q.Get("type")
	if network == "" {
		network = "tcp"
	}
	security := q.Get("security")
	if security == "" {
		security = defaultSecurity
	}
	hh := q.Get("host")
	path := q.Get("path")
	if path == "" {
		path = "/"
	}

	ss := &xray.StreamSettings{Network: network, Security: security}

	switch security {
	case "none":
	case "tls":
		sni := firstNonEmpty(q.Get("sni"), q.Get("peer"), hh, host)
		ss.TLS = &xray.TLSSettings{
			ServerName:  sni,
			Fingerprint: q.Get("fp"),
			// allowInsecure=1 is ignored: Xray rejects the option, the
			// certificate is checked normally.
		}
		if a := q.Get("alpn"); a != "" {
			ss.TLS.ALPN = strings.Split(a, ",")
		}
	case "reality":
		pbk := q.Get("pbk")
		if pbk == "" {
			return nil, errors.New("reality: missing public key (pbk)")
		}
		ss.REALITY = &xray.REALITYSettings{
			ServerName:    q.Get("sni"),
			Fingerprint:   firstNonEmpty(q.Get("fp"), "chrome"),
			PublicKey:     pbk,
			ShortID:       q.Get("sid"),
			SpiderX:       q.Get("spx"),
			Mldsa65Verify: q.Get("pqv"),
		}
	default:
		return nil, fmt.Errorf("unsupported security %q", security)
	}

	switch network {
	case "tcp", "raw":
		if q.Get("headerType") == "http" {
			req := &xray.TCPHTTPRequest{Path: []string{path}}
			if hh != "" {
				req.Headers = map[string][]string{"Host": strings.Split(hh, ",")}
			}
			ss.TCP = &xray.TCPSettings{Header: &xray.TCPHeader{Type: "http", Request: req}}
		}
	case "ws":
		ss.WS = &xray.HTTPPathHost{Path: path, Host: hh}
	case "httpupgrade":
		ss.HTTPUpgrade = &xray.HTTPPathHost{Path: path, Host: hh}
	case "grpc":
		ss.GRPC = &xray.GRPCSettings{
			ServiceName: q.Get("serviceName"),
			Authority:   q.Get("authority"),
			MultiMode:   q.Get("mode") == "multi",
		}
	case "xhttp", "splithttp":
		ss.Network = "xhttp"
		ss.XHTTP = &xray.XHTTPSettings{Path: path, Host: hh, Mode: firstNonEmpty(q.Get("mode"), "auto")}
	default:
		return nil, fmt.Errorf("unsupported transport %q", network)
	}
	return ss, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
