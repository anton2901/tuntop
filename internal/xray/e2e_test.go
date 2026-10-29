// SPDX-License-Identifier: GPL-3.0-or-later

package xray_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/anton2901/tuntop/internal/link"
	"github.com/anton2901/tuntop/internal/xray"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf"
)

const testID = "00000000-0000-0000-0000-000000000001"

// TestSOCKSThroughVLESS runs the whole chain offline, inside one process:
//
//	HTTP client → tuntop SOCKS inbound → vless link → local vless server → HTTP server
func TestSOCKSThroughVLESS(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello through vless")
	}))
	defer web.Close()

	serverPort := freePort(t)
	stopServer := startVLESSServer(t, serverPort)
	defer stopServer()

	p, err := link.Parse(fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=none#local", testID, serverPort))
	if err != nil {
		t.Fatal(err)
	}
	socksPort := freePort(t)
	inst, err := xray.Start(xray.SOCKSConfig("127.0.0.1", socksPort, p.Outbound, "warning"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inst.Close() }()

	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socksPort))
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   10 * time.Second,
	}
	resp, err := client.Get(web.URL)
	if err != nil {
		t.Fatalf("request through SOCKS failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello through vless" {
		t.Fatalf("body = %q", body)
	}
}

func TestTrafficCounters(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 100_000))
	}))
	defer web.Close()
	serverPort := freePort(t)
	defer startVLESSServer(t, serverPort)()

	p, err := link.Parse(fmt.Sprintf("vless://%s@127.0.0.1:%d?type=tcp&security=none", testID, serverPort))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := xray.LinkProfile(p.Outbound)
	if err != nil {
		t.Fatal(err)
	}
	socksPort := freePort(t)
	cfg, err := xray.WithSOCKS(profile, "127.0.0.1", socksPort, "none")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := xray.StartJSON(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inst.Close() }()
	if body := getVia(t, socksPort, web.URL); len(body) != 100_000 {
		t.Fatalf("body = %d bytes", len(body))
	}
	// The ~80-byte request may not be counted yet; the 100 KB answer is.
	if up, down := inst.Traffic(); down < 100_000 || up < 0 {
		t.Fatalf("traffic up=%d down=%d", up, down)
	}
}

// TestProviderProfileThroughVLESS runs a full provider-style Xray JSON
// profile (own inbounds, routing, Happ metadata) with tuntop's inbound.
func TestProviderProfileThroughVLESS(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "hello through profile")
	}))
	defer web.Close()

	serverPort := freePort(t)
	stopServer := startVLESSServer(t, serverPort)
	defer stopServer()

	profile := fmt.Sprintf(`{
		"remarks": "🇳🇱 Test profile",
		"meta": {"serverDescription": "ignored"},
		"inbounds": [
			{"tag": "socks", "port": 10808, "listen": "127.0.0.1", "protocol": "socks"},
			{"tag": "http", "port": 10809, "listen": "127.0.0.1", "protocol": "http"}
		],
		"outbounds": [
			{"tag": "proxy", "protocol": "vless",
			 "settings": {"vnext": [{"address": "127.0.0.1", "port": %d,
			   "users": [{"id": %q, "encryption": "none"}]}]}},
			{"tag": "direct", "protocol": "freedom"},
			{"tag": "block", "protocol": "blackhole"}
		],
		"routing": {"rules": [
			{"type": "field", "inboundTag": ["socks"], "outboundTag": "proxy"}
		]}
	}`, serverPort, testID)

	socksPort := freePort(t)
	raw, err := xray.WithSOCKS([]byte(profile), "127.0.0.1", socksPort, "warning")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := xray.StartJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = inst.Close() }()

	if body := getVia(t, socksPort, web.URL); body != "hello through profile" {
		t.Fatalf("body = %q", body)
	}
}

func getVia(t *testing.T, socksPort uint16, target string) string {
	t.Helper()
	proxyURL, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", socksPort))
	client := &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   10 * time.Second,
	}
	resp, err := client.Get(target)
	if err != nil {
		t.Fatalf("request through SOCKS failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func TestStartRejectsBadConfig(t *testing.T) {
	bad := xray.SOCKSConfig("127.0.0.1", 0, xray.Outbound{Protocol: "no-such-protocol"}, "none")
	if inst, err := xray.Start(bad); err == nil {
		_ = inst.Close()
		t.Fatal("Start succeeded with an unknown protocol")
	}
}

// startVLESSServer runs a plain (no TLS) VLESS server forwarding to freedom.
func startVLESSServer(t *testing.T, port uint16) func() {
	t.Helper()
	raw := fmt.Sprintf(`{
		"log": {"loglevel": "warning"},
		"inbounds": [{
			"listen": "127.0.0.1", "port": %d, "protocol": "vless",
			"settings": {"clients": [{"id": %q}], "decryption": "none"}
		}],
		"outbounds": [{"protocol": "freedom"}]
	}`, port, testID)
	var jc conf.Config
	if err := json.Unmarshal([]byte(raw), &jc); err != nil {
		t.Fatal(err)
	}
	pb, err := jc.Build()
	if err != nil {
		t.Fatal(err)
	}
	srv, err := core.New(pb)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	return func() { _ = srv.Close() }
}

func freePort(t *testing.T) uint16 {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}
