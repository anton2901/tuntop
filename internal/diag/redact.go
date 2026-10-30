// SPDX-License-Identifier: GPL-3.0-or-later

// Package diag holds what a bug report needs: a log file that is safe to
// share (secrets and addresses masked), its rotation, and the system
// snapshot behind "tuntop debug-bundle".
package diag

import (
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"regexp"
	"strings"
)

var (
	shareLink = regexp.MustCompile(`(?i)\b(vless|vmess|trojan|ss|hysteria2|hy2|tuic|happ|wireguard)://[^\s"'<>]+`)
	httpURL   = regexp.MustCompile(`(?i)\b(https?://[^/\s"'<>?#]+)[/?#][^\s"'<>]*`)
	uuid      = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	ipv4      = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	ipv6      = regexp.MustCompile(`(?i)\b(?:[0-9a-f]{1,4}:){3,}[0-9a-f:]*[0-9a-f]\b`)
	token     = regexp.MustCompile(`\b[A-Za-z0-9_\-+/]{32,}={0,2}`)
)

// Redact masks what must not leave the machine in a log or report: share
// links, subscription URL paths and tokens, UUIDs, keys and public IP
// addresses. A masked address keeps its first two parts and a short hash,
// so the same address is recognizable across lines. Loopback and private
// addresses stay: they are what routing problems are about.
func Redact(s string) string {
	s = shareLink.ReplaceAllString(s, "$1://<redacted>")
	s = httpURL.ReplaceAllString(s, "$1/…")
	s = uuid.ReplaceAllString(s, "<uuid>")
	s = ipv4.ReplaceAllStringFunc(s, maskAddr)
	s = ipv6.ReplaceAllStringFunc(s, maskAddr)
	return token.ReplaceAllString(s, "<token>")
}

func maskAddr(text string) string {
	a, err := netip.ParseAddr(text)
	if err != nil {
		return text // not an address (a version number, a time)
	}
	if a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast() {
		return text
	}
	sum := sha256.Sum256([]byte(text))
	h := hex.EncodeToString(sum[:2])
	if a.Is4() {
		p := strings.Split(text, ".")
		return p[0] + "." + p[1] + ".x.x#" + h
	}
	p := strings.Split(text, ":")
	return p[0] + ":" + p[1] + "::x#" + h
}
