// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The test binary doubles as the Xray child when this variable is set.
const childEnv = "TUNTOP_TEST_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		if err := RunChildMain(); err != nil {
			fmt.Fprintln(os.Stderr, "child:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// startTestChild runs this test binary as an Xray child.
func startTestChild(t *testing.T, cfg string) (*child, error) {
	t.Helper()
	t.Setenv(childEnv, "1")
	return startChild(os.Args[0], []string{"-test.run=^$"}, []byte(cfg))
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port
}

// A SOCKS inbound with a direct outbound: loopback only, no real network.
func socksConfig(port int) string {
	return fmt.Sprintf(`{
	  "log": {"loglevel": "none"},
	  "stats": {},
	  "policy": {"system": {"statsOutboundUplink": true, "statsOutboundDownlink": true}},
	  "inbounds": [{"tag": "socks", "listen": "127.0.0.1", "port": %d, "protocol": "socks",
	                "settings": {"auth": "noauth", "udp": false}}],
	  "outbounds": [{"tag": "direct", "protocol": "freedom"}]
	}`, port)
}

func TestChildRunsAndStops(t *testing.T) {
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(make([]byte, 50_000))
	}))
	defer web.Close()

	port := freePort(t)
	c, err := startTestChild(t, socksConfig(port))
	if err != nil {
		t.Fatal(err)
	}
	proxy, _ := url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", port))
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxy)}, Timeout: 10 * time.Second}
	resp, err := client.Get(web.URL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if len(body) != 50_000 {
		t.Fatalf("body = %d bytes", len(body))
	}
	// Counters arrive once a second. On Linux the direct outbound copies
	// the response with splice(2), past Xray's counters: only the request
	// is counted there. Proxy outbounds count both ways on every OS.
	deadline := time.Now().Add(5 * time.Second)
	counted := func() bool {
		if runtime.GOOS == "linux" {
			return c.up.Load() > 0
		}
		return c.down.Load() >= 50_000
	}
	for !counted() {
		if time.Now().After(deadline) {
			t.Fatalf("traffic not reported: up %d down %d", c.up.Load(), c.down.Load())
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := c.stop(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.exited:
	default:
		t.Fatal("child still running after stop")
	}
	// Its listener is gone with the process.
	if conn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port)); err == nil {
		_ = conn.Close()
		t.Fatal("port still open after stop")
	}
}

func TestChildBadConfig(t *testing.T) {
	_, err := startTestChild(t, `{"outbounds": [{"protocol": "no-such-protocol"}]}`)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "no-such-protocol") {
		t.Fatalf("err = %v", err)
	}
}

func TestChildDeathDetected(t *testing.T) {
	c, err := startTestChild(t, socksConfig(freePort(t)))
	if err != nil {
		t.Fatal(err)
	}
	_ = c.cmd.Process.Signal(syscall.SIGKILL)
	select {
	case <-c.exited:
	case <-time.After(5 * time.Second):
		t.Fatal("death not noticed")
	}
	if c.stopping.Load() {
		t.Fatal("unexpected exit looks like a requested stop")
	}
}
