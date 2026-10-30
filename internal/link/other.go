// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/anton2901/tuntop/internal/xray"
)

// parseVMess handles the v2rayN form: vmess://base64(JSON).
func parseVMess(raw string) (Profile, error) {
	body := strings.TrimPrefix(raw, raw[:strings.Index(raw, "://")+3])
	body, _, _ = strings.Cut(body, "#")
	dec, err := decodeBase64(body)
	if err != nil {
		return Profile{}, errors.New("vmess link: not base64")
	}
	var j struct {
		PS   string `json:"ps"`
		Add  string `json:"add"`
		Port any    `json:"port"`
		ID   string `json:"id"`
		Scy  string `json:"scy"`
		Net  string `json:"net"`
		Type string `json:"type"`
		Host string `json:"host"`
		Path string `json:"path"`
		TLS  string `json:"tls"`
		SNI  string `json:"sni"`
		ALPN string `json:"alpn"`
		FP   string `json:"fp"`
	}
	if err := json.Unmarshal(dec, &j); err != nil {
		return Profile{}, fmt.Errorf("vmess link: %w", err)
	}
	if j.Add == "" || j.ID == "" {
		return Profile{}, errors.New("vmess link: missing address or id")
	}
	port, err := strconv.ParseUint(fmt.Sprint(j.Port), 10, 16)
	if err != nil || port == 0 {
		return Profile{}, fmt.Errorf("vmess link: invalid port %v", j.Port)
	}
	q := url.Values{}
	set := func(k, v string) {
		if v != "" {
			q.Set(k, v)
		}
	}
	set("type", j.Net)
	set("host", j.Host)
	set("path", j.Path)
	set("sni", j.SNI)
	set("alpn", j.ALPN)
	set("fp", j.FP)
	set("headerType", j.Type)
	if j.Net == "grpc" {
		set("serviceName", j.Path)
	}
	if j.TLS == "tls" {
		q.Set("security", "tls")
	}
	stream, err := streamSettings(q, j.Add, "none")
	if err != nil {
		return Profile{}, fmt.Errorf("vmess link: %w", err)
	}
	name := j.PS
	if name == "" {
		name = net.JoinHostPort(j.Add, strconv.FormatUint(port, 10))
	}
	return Profile{
		Name: name,
		Outbound: xray.Outbound{
			Protocol: "vmess",
			Settings: xray.VMessSettings{
				Address: j.Add, Port: uint16(port), ID: j.ID,
				Security: firstNonEmpty(j.Scy, "auto"),
			},
			StreamSettings: stream,
		},
	}, nil
}

func parseTrojan(raw string) (Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Profile{}, fmt.Errorf("parse trojan link: %w", err)
	}
	pass := u.User.Username()
	if pass == "" {
		return Profile{}, errors.New("trojan link: missing password")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Profile{}, fmt.Errorf("trojan link: %w", err)
	}
	stream, err := streamSettings(u.Query(), host, "tls")
	if err != nil {
		return Profile{}, fmt.Errorf("trojan link: %w", err)
	}
	return Profile{
		Name: displayName(u, host, port),
		Outbound: xray.Outbound{
			Protocol:       "trojan",
			Settings:       xray.TrojanSettings{Address: host, Port: port, Password: pass},
			StreamSettings: stream,
		},
	}, nil
}

// parseShadowsocks handles both SIP002 (ss://base64(method:pass)@host:port)
// and the legacy form (ss://base64(method:pass@host:port)).
func parseShadowsocks(raw string) (Profile, error) {
	rest := strings.TrimPrefix(raw, raw[:strings.Index(raw, "://")+3])
	rest, frag, _ := strings.Cut(rest, "#")
	name, _ := url.PathUnescape(frag)
	rest, query, _ := strings.Cut(rest, "?")
	rest = strings.TrimSuffix(rest, "/")

	userinfo, hostport, ok := strings.Cut(rest, "@")
	if !ok { // legacy: everything is base64
		dec, err := decodeBase64(rest)
		if err != nil {
			return Profile{}, errors.New("ss link: not base64")
		}
		userinfo, hostport, ok = strings.Cut(string(dec), "@")
		if !ok {
			return Profile{}, errors.New("ss link: missing host")
		}
	} else if un, err := url.PathUnescape(userinfo); err == nil && strings.Contains(un, ":") {
		userinfo = un // plain method:password
	} else if dec, err := decodeBase64(userinfo); err == nil {
		userinfo = string(dec)
	}
	method, pass, ok := strings.Cut(userinfo, ":")
	if !ok || method == "" {
		return Profile{}, errors.New("ss link: missing method or password")
	}
	u, err := url.Parse("ss://" + hostport)
	if err != nil {
		return Profile{}, fmt.Errorf("ss link: %w", err)
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Profile{}, fmt.Errorf("ss link: %w", err)
	}
	if name == "" {
		name = net.JoinHostPort(host, strconv.Itoa(int(port)))
	}
	if strings.Contains(query, "plugin=") {
		return Profile{}, fmt.Errorf("%w: ss plugin", ErrUnsupported)
	}
	return Profile{
		Name: name,
		Outbound: xray.Outbound{
			Protocol: "shadowsocks",
			Settings: xray.ShadowsocksSettings{Address: host, Port: port, Method: method, Password: pass},
		},
	}, nil
}

func parseHysteria2(raw string) (Profile, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return Profile{}, fmt.Errorf("parse hysteria2 link: %w", err)
	}
	auth := u.User.Username()
	if p, ok := u.User.Password(); ok {
		auth += ":" + p
	}
	if auth == "" {
		return Profile{}, errors.New("hysteria2 link: missing auth")
	}
	host, port, err := hostPort(u)
	if err != nil {
		return Profile{}, fmt.Errorf("hysteria2 link: %w", err)
	}
	q := u.Query()
	// insecure=1 is ignored: Xray dropped allowInsecure, the certificate is
	// verified normally (or pinned with pinSHA256).
	ss := &xray.StreamSettings{
		Network:  "hysteria",
		Security: "tls",
		TLS: &xray.TLSSettings{
			ServerName:           firstNonEmpty(q.Get("sni"), host),
			ALPN:                 []string{"h3"},
			PinnedPeerCertSha256: strings.ReplaceAll(q.Get("pinSHA256"), "-", ":"),
		},
		Hysteria: &xray.HysteriaStream{Version: 2, Auth: auth},
	}
	if t := q.Get("obfs"); t != "" {
		if t != "salamander" {
			return Profile{}, fmt.Errorf("%w: hysteria2 obfs %s", ErrUnsupported, t)
		}
		ss.FinalMask = &xray.FinalMask{UDP: []xray.Mask{{
			Type:     "salamander",
			Settings: map[string]string{"password": q.Get("obfs-password")},
		}}}
	}
	return Profile{
		Name: displayName(u, host, port),
		Outbound: xray.Outbound{
			Protocol:       "hysteria",
			Settings:       xray.HysteriaSettings{Version: 2, Address: host, Port: port},
			StreamSettings: ss,
		},
	}, nil
}

func decodeBase64(s string) ([]byte, error) {
	s = strings.Join(strings.Fields(s), "")
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, errors.New("not base64")
}
