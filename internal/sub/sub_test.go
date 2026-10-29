// SPDX-License-Identifier: GPL-3.0-or-later

package sub

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/anton2901/tuntop/internal/xray"
)

// All values are fake.
const (
	testID  = "00000000-0000-0000-0000-000000000001"
	fakePBK = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
)

func vlessOutbound(tag, host string) string {
	return fmt.Sprintf(`{"tag": %q, "protocol": "vless",
		"settings": {"vnext": [{"address": %q, "port": 443, "users": [{"id": %q, "encryption": "none"}]}]},
		"streamSettings": {"network": "tcp", "security": "reality",
		  "realitySettings": {"serverName": "www.example.org", "publicKey": %q, "fingerprint": "chrome"}}}`,
		tag, host, testID, fakePBK)
}

var xrayJSON = `[
  {"remarks": "🇳🇱 Netherlands", "meta": {},
   "inbounds": [{"tag": "socks", "port": 10808, "protocol": "socks"}],
   "outbounds": [` + vlessOutbound("proxy", "nl.example.com") + `,
     {"tag": "direct", "protocol": "freedom"}, {"tag": "block", "protocol": "blackhole"}]},
  {"remarks": "✨ Smart Best auto",
   "outbounds": [` + vlessOutbound("a", "a.example.com") + `,` + vlessOutbound("b", "b.example.com") + `,
     {"tag": "direct", "protocol": "freedom"}],
   "routing": {"balancers": [{"tag": "bal", "selector": ["a", "b"]}],
     "rules": [{"type": "field", "network": "tcp,udp", "balancerTag": "bal"}]}},
  {"remarks": "⚠️ Неподдерживаемый клиент", "outbounds": [{"protocol": "freedom"}]},
  {"remarks": "only direct", "outbounds": [{"protocol": "freedom"}]}
]`

func TestParseXrayJSON(t *testing.T) {
	s, err := Parse([]byte(xrayJSON), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (%+v)", len(s.Entries), s.Entries)
	}
	nl, group := s.Entries[0], s.Entries[1]
	if nl.Name != "🇳🇱 Netherlands" || nl.Kind != KindXray || nl.Proxies != 1 || nl.Proto != "vless reality tcp" {
		t.Errorf("server entry = %+v", nl)
	}
	if nl.Address != "nl.example.com:443" {
		t.Errorf("address = %q", nl.Address)
	}
	if group.Kind != KindXrayGroup || group.Proxies != 2 {
		t.Errorf("group entry = %+v", group)
	}
	if len(s.Skipped) != 2 {
		t.Errorf("skipped = %+v, want junk + no-proxy", s.Skipped)
	}
	for _, e := range s.Entries {
		raw, err := e.Config("127.0.0.1", 10808, "none")
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"remarks"`) || strings.Contains(string(raw), "10809") {
			t.Errorf("%s: provider metadata or inbounds leaked: %s", e.Name, raw)
		}
		if _, err := xray.BuildJSON(raw); err != nil {
			t.Errorf("%s: xray rejected config: %v", e.Name, err)
		}
	}
}

func TestParseSingleXrayConfig(t *testing.T) {
	one := strings.TrimSpace(xrayJSON[strings.Index(xrayJSON, "{"):strings.Index(xrayJSON, `,
  {"remarks": "✨`)])
	s, err := Parse([]byte(one), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 1 || s.Entries[0].Name != "🇳🇱 Netherlands" {
		t.Fatalf("entries = %+v", s.Entries)
	}
}

func TestParseBase64Links(t *testing.T) {
	list := strings.Join([]string{
		"vless://" + testID + "@nl.example.com:443?type=tcp&security=reality&pbk=" + fakePBK + "&sni=www.example.org#NL",
		"vless://" + testID + "@de.example.com:8443?type=ws&security=tls&path=%2Fws#DE",
		"hysteria2://secret@hy.example.com:443#HY2",
		"vless://" + testID + "@0.0.0.0:1?type=tcp#Приложение не поддерживается",
		"vless://" + testID + "@x.example.com:443?security=none#👉 Скачайте Happ/INCY для 100% доступа",
		"vless://" + testID + "@127.0.0.1:443?security=none#ㅤ",
		"",
	}, "\n")
	for name, body := range map[string]string{
		"base64": base64.StdEncoding.EncodeToString([]byte(list)),
		"plain":  list,
	} {
		t.Run(name, func(t *testing.T) {
			s, err := Parse([]byte(body), nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(s.Entries) != 2 || s.Entries[0].Name != "NL" || s.Entries[1].Proto != "vless tls ws" ||
				s.Entries[1].Address != "de.example.com:8443" {
				t.Fatalf("entries = %+v", s.Entries)
			}
			if len(s.Skipped) != 4 {
				t.Fatalf("skipped = %+v", s.Skipped)
			}
			raw, err := s.Entries[0].Config("127.0.0.1", 10808, "none")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := xray.BuildJSON(raw); err != nil {
				t.Fatalf("xray rejected config: %v", err)
			}
		})
	}
}

func TestNoServersShowsProviderNotices(t *testing.T) {
	body := strings.Join([]string{
		"vless://" + testID + "@stub.example.com:1?security=none#Приложение не поддерживается",
		"vless://" + testID + "@stub.example.com:1?security=none#Или включите «отправить HWID»",
	}, "\n")
	_, err := Parse([]byte(body), nil)
	if !errors.Is(err, ErrNoServers) || !strings.Contains(err.Error(), "«Или включите «отправить HWID»»") {
		t.Fatalf("err = %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	stub := "vless://" + testID + "@0.0.0.0:1?type=tcp#Приложение не поддерживается"
	for name, body := range map[string]string{
		"empty":     "   ",
		"garbage":   "<html>nope</html>",
		"only stub": base64.StdEncoding.EncodeToString([]byte(stub)),
		"bad json":  "[{",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(body), nil); err == nil {
				t.Fatal("Parse succeeded, want error")
			}
		})
	}
}

func TestParseInfo(t *testing.T) {
	h := http.Header{}
	h.Set("Profile-Title", "base64:"+base64.StdEncoding.EncodeToString([]byte("✨ Test VPN")))
	h.Set("Subscription-Userinfo", "upload=10; download=3658825318; total=0; expire=1795360781")
	h.Set("Profile-Update-Interval", "12")
	h.Set("Support-Url", "https://t.me/example")
	h.Set("Announce", "plain text")
	i := ParseInfo(h)
	if i.Title != "✨ Test VPN" || i.Upload != 10 || i.Download != 3658825318 || i.Total != 0 {
		t.Errorf("info = %+v", i)
	}
	if !i.Expire.Equal(time.Unix(1795360781, 0)) || i.UpdateInterval != 12*time.Hour {
		t.Errorf("expire/interval = %v / %v", i.Expire, i.UpdateInterval)
	}
	if i.SupportURL != "https://t.me/example" || i.Announce != "plain text" {
		t.Errorf("info = %+v", i)
	}
}

func TestFetchSendsHeaders(t *testing.T) {
	var gotUA, gotHWID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA, gotHWID = r.UserAgent(), r.Header.Get("x-hwid")
		w.Header().Set("Profile-Title", "Fake")
		_, _ = w.Write([]byte(xrayJSON))
	}))
	defer srv.Close()

	s, err := Fetch(context.Background(), srv.URL, Options{Headers: map[string]string{"x-hwid": "abc"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotUA != DefaultUserAgent || gotHWID != "abc" {
		t.Errorf("UA = %q, x-hwid = %q", gotUA, gotHWID)
	}
	if s.Info.Title != "Fake" || len(s.Entries) != 2 || string(s.Body) != xrayJSON {
		t.Errorf("subscription = %+v", s)
	}
}

func TestFetchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL, Options{}); err == nil {
		t.Fatal("Fetch succeeded on 403")
	}
}

func TestFetchErrorHidesToken(t *testing.T) {
	// Nothing listens on port 1.
	_, err := Fetch(context.Background(), "http://127.0.0.1:1/api/sub/SECRETTOKEN", Options{})
	if err == nil || strings.Contains(err.Error(), "SECRETTOKEN") {
		t.Fatalf("err = %v", err)
	}
}

func TestFetchThroughProxy(t *testing.T) {
	var proxied bool
	// An HTTP proxy for a plain-http target receives the absolute URL.
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied = r.URL.IsAbs()
		_, _ = w.Write([]byte(xrayJSON))
	}))
	defer proxy.Close()

	s, err := Fetch(context.Background(), "http://sub.example.com/x", Options{Proxy: proxy.URL})
	if err != nil {
		t.Fatal(err)
	}
	if !proxied || len(s.Entries) != 2 {
		t.Fatalf("proxied = %v, entries = %d", proxied, len(s.Entries))
	}
}

func TestKeysStableAcrossReorderAndRename(t *testing.T) {
	a, err := Parse([]byte(xrayJSON), nil)
	if err != nil {
		t.Fatal(err)
	}
	// Same servers, reversed order, different names (load % changed) and
	// different key order inside settings.
	swapped := strings.Replace(xrayJSON, "🇳🇱 Netherlands", "🇳🇱 Netherlands · 🟢12%", 1)
	b, err := Parse([]byte(swapped), nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Entries[0].Key == "" || a.Entries[0].Key != b.Entries[0].Key {
		t.Errorf("key changed with the name: %q vs %q", a.Entries[0].Key, b.Entries[0].Key)
	}
	if a.Entries[0].Key == a.Entries[1].Key {
		t.Error("different servers share a key")
	}
}

func TestDuplicateEntriesGetUniqueKeys(t *testing.T) {
	l := "vless://" + testID + "@nl.example.com:443?security=none#NL"
	s, err := Parse([]byte(l+"\n"+l+"\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Entries) != 2 || s.Entries[0].Key == s.Entries[1].Key {
		t.Fatalf("keys = %q, %q", s.Entries[0].Key, s.Entries[1].Key)
	}
}
