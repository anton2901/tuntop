// SPDX-License-Identifier: GPL-3.0-or-later

package link

import (
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"github.com/anton2901/tuntop/internal/xray"
)

// All values are fake: zeroed UUIDs, example.com hosts, made-up keys.
const (
	testID  = "00000000-0000-0000-0000-000000000001"
	fakePBK = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA" // 32 zero bytes, base64url
)

func TestParseVLESSReality(t *testing.T) {
	p, err := Parse("vless://" + testID + "@srv.example.com:8443" +
		"?type=tcp&security=reality&sni=www.example.org&fp=firefox" +
		"&pbk=" + fakePBK + "&sid=0a1b&spx=%2Fx" +
		"&flow=xtls-rprx-vision#%F0%9F%87%B3%F0%9F%87%B1%20Netherlands")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "🇳🇱 Netherlands" {
		t.Errorf("Name = %q", p.Name)
	}
	want := xray.Outbound{
		Protocol: "vless",
		Settings: xray.VLESSSettings{
			Address: "srv.example.com", Port: 8443, ID: testID,
			Encryption: "none", Flow: "xtls-rprx-vision",
		},
		StreamSettings: &xray.StreamSettings{
			Network: "tcp", Security: "reality",
			REALITY: &xray.REALITYSettings{
				ServerName: "www.example.org", Fingerprint: "firefox",
				PublicKey: fakePBK,
				ShortID:   "0a1b", SpiderX: "/x",
			},
		},
	}
	if !reflect.DeepEqual(p.Outbound, want) {
		t.Errorf("Outbound =\n%+v\nwant\n%+v", p.Outbound, want)
	}
}

func TestParseVLESSDefaults(t *testing.T) {
	p, err := Parse("  vless://" + testID + "@srv.example.com\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.Name != "srv.example.com:443" {
		t.Errorf("Name = %q", p.Name)
	}
	s := p.Outbound.Settings.(xray.VLESSSettings)
	if s.Port != 443 || s.Encryption != "none" {
		t.Errorf("settings = %+v", s)
	}
	ss := p.Outbound.StreamSettings
	if ss.Network != "tcp" || ss.Security != "none" {
		t.Errorf("stream = %+v", ss)
	}
}

func TestParseVLESSTransports(t *testing.T) {
	base := "vless://" + testID + "@srv.example.com:443?security=tls&sni=cdn.example.com"
	tests := []struct {
		query string
		check func(*xray.StreamSettings) bool
	}{
		{"&type=ws&path=%2Fws&host=h.example.com", func(s *xray.StreamSettings) bool {
			return s.WS != nil && s.WS.Path == "/ws" && s.WS.Host == "h.example.com"
		}},
		{"&type=grpc&serviceName=svc&mode=multi", func(s *xray.StreamSettings) bool {
			return s.GRPC != nil && s.GRPC.ServiceName == "svc" && s.GRPC.MultiMode
		}},
		{"&type=xhttp&path=%2Fx", func(s *xray.StreamSettings) bool {
			return s.XHTTP != nil && s.XHTTP.Path == "/x" && s.XHTTP.Mode == "auto"
		}},
		{"&type=splithttp", func(s *xray.StreamSettings) bool {
			return s.Network == "xhttp" && s.XHTTP != nil
		}},
		{"&type=httpupgrade&path=%2Fu", func(s *xray.StreamSettings) bool {
			return s.HTTPUpgrade != nil && s.HTTPUpgrade.Path == "/u"
		}},
		{"&type=tcp&headerType=http&host=a.example.com", func(s *xray.StreamSettings) bool {
			return s.TCP != nil && s.TCP.Header.Type == "http" &&
				s.TCP.Header.Request.Headers["Host"][0] == "a.example.com"
		}},
		{"&alpn=h2,http%2F1.1&fp=chrome", func(s *xray.StreamSettings) bool {
			return s.TLS.ServerName == "cdn.example.com" && s.TLS.Fingerprint == "chrome" &&
				reflect.DeepEqual(s.TLS.ALPN, []string{"h2", "http/1.1"})
		}},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			p, err := Parse(base + tt.query)
			if err != nil {
				t.Fatal(err)
			}
			if !tt.check(p.Outbound.StreamSettings) {
				t.Errorf("unexpected stream settings: %+v", p.Outbound.StreamSettings)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"no scheme":         "srv.example.com:443",
		"no id":             "vless://srv.example.com:443",
		"no host":           "vless://" + testID + "@:443",
		"bad port":          "vless://" + testID + "@srv.example.com:99999",
		"reality no pbk":    "vless://" + testID + "@srv.example.com?security=reality",
		"unknown security":  "vless://" + testID + "@srv.example.com?security=xtls",
		"unknown transport": "vless://" + testID + "@srv.example.com?type=quic",
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(raw); err == nil {
				t.Errorf("Parse(%q) succeeded, want error", raw)
			}
		})
	}
}

func TestParseUnsupportedScheme(t *testing.T) {
	_, err := Parse("tuic://pass@srv.example.com:443")
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("err = %v, want ErrUnsupported", err)
	}
}

// Every parsed link must be accepted by Xray's own config builder.
func TestParsedOutboundsBuildInXray(t *testing.T) {
	links := []string{
		"vless://" + testID + "@srv.example.com:443?security=none",
		"vless://" + testID + "@srv.example.com:443?security=reality&sni=www.example.org" +
			"&pbk=" + fakePBK + "&sid=0a1b&flow=xtls-rprx-vision",
		"vless://" + testID + "@srv.example.com:443?security=tls&type=ws&path=%2Fws",
		"vless://" + testID + "@srv.example.com:443?security=tls&type=grpc&serviceName=svc",
		"vless://" + testID + "@srv.example.com:443?security=tls&type=xhttp&mode=packet-up",
		"vless://" + testID + "@srv.example.com:443?type=tcp&headerType=http&host=a.example.com",
		vmessLink(`{"v":"2","ps":"x","add":"srv.example.com","port":"443","id":"` + testID + `","net":"ws","tls":"tls","path":"/w","host":"h.example.com"}`),
		"trojan://secret@srv.example.com:443?sni=a.example.com#t",
		"trojan://secret@srv.example.com:443?security=none&type=ws",
		"ss://" + b64("aes-256-gcm:pw") + "@srv.example.com:8388#s",
		"ss://" + b64("chacha20-ietf-poly1305:pw@srv.example.com:8388"),
		"hysteria2://auth@srv.example.com:443?sni=a.example.com&insecure=1#h",
		"hysteria2://auth@srv.example.com:443?obfs=salamander&obfs-password=x&pinSHA256=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	for _, l := range links {
		p, err := Parse(l)
		if err != nil {
			t.Fatalf("Parse(%q): %v", l, err)
		}
		if _, err := xray.Build(xray.SOCKSConfig("127.0.0.1", 10808, p.Outbound, "none")); err != nil {
			t.Errorf("xray rejected %q: %v", l, err)
		}
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

func vmessLink(j string) string { return "vmess://" + base64.StdEncoding.EncodeToString([]byte(j)) }

func TestParseVMess(t *testing.T) {
	p, err := Parse(vmessLink(`{"ps":"Name","add":"srv.example.com","port":8443,"id":"` + testID + `","net":"tcp"}`))
	if err != nil {
		t.Fatal(err)
	}
	v := p.Outbound.Settings.(xray.VMessSettings)
	if p.Name != "Name" || v.Port != 8443 || v.ID != testID || v.Security != "auto" {
		t.Errorf("got %q %+v", p.Name, v)
	}
}

func TestParseShadowsocksForms(t *testing.T) {
	want := xray.ShadowsocksSettings{Address: "srv.example.com", Port: 8388, Method: "aes-256-gcm", Password: "p:w"}
	for _, l := range []string{
		"ss://" + b64("aes-256-gcm:p:w") + "@srv.example.com:8388#n",
		"ss://aes-256-gcm:p%3Aw@srv.example.com:8388/#n",
		"ss://" + b64("aes-256-gcm:p:w@srv.example.com:8388") + "#n",
	} {
		p, err := Parse(l)
		if err != nil {
			t.Fatalf("Parse(%q): %v", l, err)
		}
		if got := p.Outbound.Settings.(xray.ShadowsocksSettings); got != want {
			t.Errorf("Parse(%q) = %+v", l, got)
		}
	}
	if _, err := Parse("ss://" + b64("aes-256-gcm:p") + "@h.example.com:1?plugin=obfs-local"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("plugin: err = %v, want ErrUnsupported", err)
	}
}

func TestParseHysteria2(t *testing.T) {
	p, err := Parse("hy2://a:b@srv.example.com:8443?sni=s.example.com&pinSHA256=00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff&obfs=salamander&obfs-password=o#N")
	if err != nil {
		t.Fatal(err)
	}
	ss := p.Outbound.StreamSettings
	if p.Outbound.Protocol != "hysteria" || ss.Hysteria.Auth != "a:b" || ss.TLS.PinnedPeerCertSha256 != "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff" ||
		ss.TLS.ServerName != "s.example.com" || ss.FinalMask.UDP[0].Type != "salamander" {
		t.Errorf("got %+v %+v", p.Outbound, ss)
	}
	if _, err := Parse("hysteria2://a@h.example.com:1?obfs=other"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("obfs: err = %v", err)
	}
}
