// SPDX-License-Identifier: GPL-3.0-or-later

// Package sub downloads and parses subscriptions.
//
// A subscription URL returns a list of servers in one of several formats;
// which one depends on the User-Agent the client presents. Supported now:
//
//   - Xray JSON: an array of complete Xray configs (or a single one), as
//     panels serve to Happ / INCY. Each entry may be a plain server or a
//     "metaprofile" with balancers and its own routing;
//   - a list of share links, plain or base64-encoded.
//
// Clash YAML and sing-box JSON are not handled yet.
package sub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/link"
	"github.com/anton2901/tuntop/internal/safe"
	"github.com/anton2901/tuntop/internal/xray"
)

// DefaultUserAgent makes panels return the full Xray JSON variant. Many
// providers send a truncated list, or only an "unsupported client" stub,
// to anything else.
const DefaultUserAgent = "Happ/1.0"

// maxBody caps the download; real Xray JSON subscriptions are ~1 MB.
const maxBody = 16 << 20

// Kind tells how an entry runs.
type Kind int

const (
	// KindLink is a single server from a share link.
	KindLink Kind = iota
	// KindXray is a complete Xray config with one proxy outbound.
	KindXray
	// KindXrayGroup is a complete Xray config with several proxies or a
	// balancer: the provider's own auto-select group.
	KindXrayGroup
)

// Entry is one selectable item of a subscription.
type Entry struct {
	Name string
	// Key identifies the server across downloads: it is derived from the
	// connection settings (address, port, credentials), not from the name
	// or position, which providers change (load percentages, re-sorting).
	Key  string
	Kind Kind
	// Proto is a short human description, e.g. "vless reality tcp".
	Proto string
	// Proxies is the number of proxy outbounds (1 for a plain server).
	Proxies int
	// Address is host:port of the (first) proxy server.
	Address string

	outbound *xray.Outbound // KindLink
	raw      json.RawMessage
}

// Profile returns the entry as an Xray JSON config without inbounds; pass
// it to xray.WithSOCKS or xray.WithTUN to make it runnable.
func (e Entry) Profile() ([]byte, error) {
	if e.Kind == KindLink {
		return xray.LinkProfile(*e.outbound)
	}
	return e.raw, nil
}

// Config returns a runnable Xray JSON config for the entry with a SOCKS
// inbound on host:port.
func (e Entry) Config(host string, port uint16, logLevel string) ([]byte, error) {
	p, err := e.Profile()
	if err != nil {
		return nil, err
	}
	return xray.WithSOCKS(p, host, port, logLevel)
}

// Skipped is an entry that could not be used, with the reason.
type Skipped struct {
	Name   string
	Reason string
}

// Subscription is a parsed subscription response.
type Subscription struct {
	Info    Info
	Entries []Entry
	Skipped []Skipped
	// Body is the raw response. Providers reorder servers on every request
	// (e.g. by load), so anything that refers to entries by position must
	// keep and re-parse this exact body rather than download again.
	Body []byte
}

// Options control the request.
type Options struct {
	// UserAgent defaults to DefaultUserAgent.
	UserAgent string
	// Headers are added to the request, e.g. device.Info.Headers().
	Headers map[string]string
	// Proxy, if set, is used for the download (e.g. socks5h://127.0.0.1:1080).
	// Some providers' subscription domains are blocked by SNI on direct
	// connections, so clients fetch them through a working tunnel.
	Proxy  string
	Client *http.Client
}

// Fetch downloads and parses a subscription.
func Fetch(ctx context.Context, rawURL string, opt Options) (*Subscription, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("subscription request: %w", err)
	}
	ua := opt.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	req.Header.Set("User-Agent", ua)
	for k, v := range opt.Headers {
		req.Header.Set(k, v)
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
		if opt.Proxy != "" {
			pu, err := url.Parse(opt.Proxy)
			if err != nil {
				return nil, fmt.Errorf("subscription proxy: %w", err)
			}
			client.Transport = &http.Transport{Proxy: http.ProxyURL(pu)}
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		// url.Error repeats the URL, and its path is the user's token:
		// keep it out of messages and logs.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("download subscription: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download subscription: HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("download subscription: %w", err)
	}
	s, err := Parse(body, resp.Header)
	if s != nil {
		s.Body = body
	}
	return s, err
}

// Parse parses a subscription body; header may be nil.
func Parse(body []byte, header http.Header) (*Subscription, error) {
	s := &Subscription{Info: ParseInfo(header)}
	body = bytes.TrimSpace(body)
	var err error
	switch {
	case len(body) == 0:
		return nil, errors.New("subscription is empty")
	case body[0] == '[' || body[0] == '{':
		err = s.parseXrayJSON(body)
	default:
		err = s.parseLinks(body)
	}
	if err != nil {
		return nil, err
	}
	kept := s.Entries[:0]
	for _, e := range s.Entries {
		if localAddress(e.Address) {
			s.Skipped = append(s.Skipped, Skipped{e.Name, "points at this machine, not a server"})
			continue
		}
		kept = append(kept, e)
	}
	s.Entries = kept
	if len(s.Entries) == 0 {
		return s, noServers(s.Skipped)
	}
	// Identical entries share a key; number the repeats to keep keys unique.
	seen := map[string]int{}
	for i := range s.Entries {
		k := s.Entries[i].Key
		seen[k]++
		if n := seen[k]; n > 1 {
			s.Entries[i].Key = fmt.Sprintf("%s-%d", k, n)
		}
	}
	return s, nil
}

// settingsAddress extracts host:port from outbound settings in any of
// Xray's shapes: vnext (vless/vmess), servers (trojan/ss) or flat.
func settingsAddress(raw []byte) string {
	type hp struct {
		Address string          `json:"address"`
		Port    json.RawMessage `json:"port"`
	}
	var v struct {
		hp
		Vnext   []hp `json:"vnext"`
		Servers []hp `json:"servers"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	pick := v.hp
	switch {
	case len(v.Vnext) > 0:
		pick = v.Vnext[0]
	case len(v.Servers) > 0:
		pick = v.Servers[0]
	}
	if pick.Address == "" {
		return ""
	}
	return net.JoinHostPort(pick.Address, strings.Trim(string(pick.Port), `"`))
}

// canonical re-encodes JSON with sorted keys so the same settings hash the
// same regardless of how the provider ordered them.
func canonical(raw []byte) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return string(raw)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func hashKey(prefix, s string) string {
	sum := sha256.Sum256([]byte(s))
	return prefix + hex.EncodeToString(sum[:6])
}

func (s *Subscription) parseXrayJSON(body []byte) error {
	var items []json.RawMessage
	if body[0] == '{' {
		items = []json.RawMessage{body}
	} else if err := json.Unmarshal(body, &items); err != nil {
		return fmt.Errorf("parse Xray JSON subscription: %w", err)
	}
	for i, raw := range items {
		var p struct {
			Remarks   string `json:"remarks"`
			Outbounds []struct {
				Protocol       string          `json:"protocol"`
				Settings       json.RawMessage `json:"settings"`
				StreamSettings struct {
					Network  string `json:"network"`
					Security string `json:"security"`
				} `json:"streamSettings"`
			} `json:"outbounds"`
			Routing struct {
				Balancers []json.RawMessage `json:"balancers"`
			} `json:"routing"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			s.Skipped = append(s.Skipped, Skipped{fmt.Sprintf("#%d", i+1), "invalid JSON: " + err.Error()})
			continue
		}
		name := safe.Text(strings.TrimSpace(p.Remarks))
		if name == "" {
			name = fmt.Sprintf("Profile %d", i+1)
		}
		if isJunk(name) {
			s.Skipped = append(s.Skipped, Skipped{name, noticeReason})
			continue
		}
		e := Entry{Name: name, Kind: KindXray, raw: raw}
		var parts []string
		for _, o := range p.Outbounds {
			if !isProxyProtocol(o.Protocol) {
				continue
			}
			parts = append(parts, o.Protocol+" "+canonical(o.Settings))
			if e.Address == "" {
				e.Address = settingsAddress(o.Settings)
			}
			e.Proxies++
			if e.Proto == "" {
				e.Proto = describe(o.Protocol, o.StreamSettings.Security, o.StreamSettings.Network)
			}
		}
		if e.Proxies == 0 {
			s.Skipped = append(s.Skipped, Skipped{name, "no proxy outbounds"})
			continue
		}
		if e.Proxies > 1 || len(p.Routing.Balancers) > 0 {
			e.Kind = KindXrayGroup
		}
		sort.Strings(parts)
		e.Key = hashKey("x", strings.Join(parts, "\n"))
		s.Entries = append(s.Entries, e)
	}
	return nil
}

func (s *Subscription) parseLinks(body []byte) error {
	text := string(body)
	if !strings.Contains(text, "://") {
		dec, err := decodeBase64(text)
		if err != nil {
			return errors.New("unknown subscription format (not JSON, links or base64)")
		}
		text = string(dec)
	}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
			continue
		}
		p, err := link.Parse(line)
		if err != nil {
			s.Skipped = append(s.Skipped, Skipped{linkName(line), err.Error()})
			continue
		}
		if isJunk(p.Name) || isPlaceholder(p.Outbound) {
			s.Skipped = append(s.Skipped, Skipped{p.Name, noticeReason})
			continue
		}
		out := p.Outbound
		ss := out.StreamSettings
		settings, _ := json.Marshal(out.Settings)
		addr := settingsAddress(settings)
		s.Entries = append(s.Entries, Entry{
			Name:     p.Name,
			Key:      hashKey("l", out.Protocol+" "+canonical(settings)),
			Kind:     KindLink,
			Address:  addr,
			Proto:    describe(out.Protocol, ss.Security, ss.Network),
			Proxies:  1,
			outbound: &out,
		})
	}
	return nil
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

// junkMarkers are substrings of fake "servers" providers insert to tell
// unsupported clients to install Happ.
var junkMarkers = []string{
	"не поддерживается", "неподдерживаемый", "скачайте", "поддержкой xray json",
	"поддержкой json", "unsupported", "not supported",
}

func isJunk(name string) bool {
	n := strings.ToLower(name)
	for _, m := range junkMarkers {
		if strings.Contains(n, m) {
			return true
		}
	}
	return false
}

// noticeReason marks skipped entries that are messages from the provider.
const noticeReason = "provider notice, not a server"

// ErrNoServers means the subscription downloaded but holds no usable servers.
// Downloading it again another way does not help.
var ErrNoServers = errors.New("subscription has no usable servers")

// noServers explains an empty subscription. Providers answer clients they
// do not accept with fake entries whose names say why ("enable sending
// HWID"); show them.
func noServers(skipped []Skipped) error {
	var notes []string
	for _, sk := range skipped {
		if sk.Reason == noticeReason && len(notes) < 3 {
			notes = append(notes, "«"+strings.TrimSpace(sk.Name)+"»")
		}
	}
	if len(notes) == 0 {
		return ErrNoServers
	}
	// The provider's words first: a narrow screen cuts the end.
	return fmt.Errorf("provider says %s: %w", strings.Join(notes, ", "), ErrNoServers)
}

// localAddress reports a loopback or unspecified host: providers use such
// entries as separators or stubs ("ㅤ" at 127.0.0.1); a real server is
// never on the user's machine.
func localAddress(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

// isPlaceholder catches stub entries pointing at port 0 or 1.
func isPlaceholder(o xray.Outbound) bool {
	if v, ok := o.Settings.(xray.VLESSSettings); ok {
		return v.Port <= 1
	}
	return false
}

func isProxyProtocol(p string) bool {
	switch p {
	case "freedom", "blackhole", "dns", "loopback", "":
		return false
	}
	return true
}

func describe(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" && p != "none" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func linkName(line string) string {
	if _, frag, ok := strings.Cut(line, "#"); ok && frag != "" {
		return frag
	}
	scheme, _, _ := strings.Cut(line, "://")
	return scheme + " link"
}
