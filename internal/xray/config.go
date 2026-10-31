// SPDX-License-Identifier: GPL-3.0-or-later

// Package xray builds Xray-core configurations and runs them in-process.
//
// The types below model the subset of the Xray JSON config that tuntop
// generates. They marshal to the same JSON users know from Xray docs, which
// is then fed through Xray's own config loader, so every field keeps its
// upstream semantics.
package xray

// Config is a top-level Xray configuration.
type Config struct {
	Log       *Log       `json:"log,omitempty"`
	Inbounds  []Inbound  `json:"inbounds,omitempty"`
	Outbounds []Outbound `json:"outbounds,omitempty"`
}

// Log controls Xray logging. Level is one of debug, info, warning, error, none.
//
// Access is always "none": by default Xray writes every destination of every
// connection to the console, i.e. the user's browsing history into the log.
type Log struct {
	Access string `json:"access,omitempty"`
	Level  string `json:"loglevel,omitempty"`
}

// Inbound is a listener accepting local connections.
type Inbound struct {
	Tag      string `json:"tag,omitempty"`
	Listen   string `json:"listen,omitempty"`
	Port     uint16 `json:"port"`
	Protocol string `json:"protocol"`
	Settings any    `json:"settings,omitempty"`
}

// SOCKSInboundSettings configures a "socks" inbound.
type SOCKSInboundSettings struct {
	Auth string `json:"auth"`
	UDP  bool   `json:"udp"`
}

// Outbound is a way out of the process: a proxy server, direct or block.
type Outbound struct {
	Tag            string          `json:"tag,omitempty"`
	Protocol       string          `json:"protocol"`
	Settings       any             `json:"settings,omitempty"`
	StreamSettings *StreamSettings `json:"streamSettings,omitempty"`
}

// VLESSSettings configures a "vless" outbound (flat form, one server).
type VLESSSettings struct {
	Address    string `json:"address"`
	Port       uint16 `json:"port"`
	ID         string `json:"id"`
	Encryption string `json:"encryption"`
	Flow       string `json:"flow,omitempty"`
}

// VMessSettings configures a "vmess" outbound (flat form, one server).
type VMessSettings struct {
	Address  string `json:"address"`
	Port     uint16 `json:"port"`
	ID       string `json:"id"`
	Security string `json:"security,omitempty"`
}

// TrojanSettings configures a "trojan" outbound (flat form, one server).
type TrojanSettings struct {
	Address  string `json:"address"`
	Port     uint16 `json:"port"`
	Password string `json:"password"`
}

// ShadowsocksSettings configures a "shadowsocks" outbound (flat form).
type ShadowsocksSettings struct {
	Address  string `json:"address"`
	Port     uint16 `json:"port"`
	Method   string `json:"method"`
	Password string `json:"password"`
}

// HysteriaSettings configures a "hysteria" outbound (version 2 only).
type HysteriaSettings struct {
	Version int32  `json:"version"`
	Address string `json:"address"`
	Port    uint16 `json:"port"`
}

// StreamSettings selects the transport and its security layer.
type StreamSettings struct {
	Network  string `json:"network"`
	Security string `json:"security,omitempty"`

	TLS     *TLSSettings     `json:"tlsSettings,omitempty"`
	REALITY *REALITYSettings `json:"realitySettings,omitempty"`

	TCP         *TCPSettings   `json:"tcpSettings,omitempty"`
	WS          *HTTPPathHost  `json:"wsSettings,omitempty"`
	HTTPUpgrade *HTTPPathHost  `json:"httpupgradeSettings,omitempty"`
	GRPC        *GRPCSettings  `json:"grpcSettings,omitempty"`
	XHTTP       *XHTTPSettings `json:"xhttpSettings,omitempty"`

	Hysteria  *HysteriaStream `json:"hysteriaSettings,omitempty"`
	FinalMask *FinalMask      `json:"finalmask,omitempty"`
}

// HysteriaStream is the hysteria transport (auth password, version 2).
type HysteriaStream struct {
	Version int32  `json:"version"`
	Auth    string `json:"auth"`
}

// FinalMask wraps the transport, e.g. Salamander obfuscation for hysteria.
type FinalMask struct {
	UDP []Mask `json:"udp,omitempty"`
}

// Mask is one finalmask layer.
type Mask struct {
	Type     string `json:"type"`
	Settings any    `json:"settings,omitempty"`
}

// TLSSettings configures plain TLS.
type TLSSettings struct {
	ServerName    string   `json:"serverName,omitempty"`
	Fingerprint   string   `json:"fingerprint,omitempty"`
	ALPN          []string `json:"alpn,omitempty"`
	AllowInsecure bool     `json:"allowInsecure,omitempty"`
	// PinnedPeerCertSha256 pins the server certificate (hysteria2 pinSHA256).
	PinnedPeerCertSha256 string `json:"pinnedPeerCertSha256,omitempty"`
}

// REALITYSettings configures the client side of REALITY.
type REALITYSettings struct {
	ServerName    string `json:"serverName,omitempty"`
	Fingerprint   string `json:"fingerprint,omitempty"`
	PublicKey     string `json:"publicKey"`
	ShortID       string `json:"shortId,omitempty"`
	SpiderX       string `json:"spiderX,omitempty"`
	Mldsa65Verify string `json:"mldsa65Verify,omitempty"`
}

// TCPSettings carries optional HTTP header obfuscation for raw TCP.
type TCPSettings struct {
	Header *TCPHeader `json:"header,omitempty"`
}

// TCPHeader is the "http" header camouflage for raw TCP.
type TCPHeader struct {
	Type    string          `json:"type"`
	Request *TCPHTTPRequest `json:"request,omitempty"`
}

// TCPHTTPRequest is the fake HTTP request sent by the "http" header type.
type TCPHTTPRequest struct {
	Path    []string            `json:"path,omitempty"`
	Headers map[string][]string `json:"headers,omitempty"`
}

// HTTPPathHost is shared by the ws and httpupgrade transports.
type HTTPPathHost struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

// GRPCSettings configures the gRPC transport.
type GRPCSettings struct {
	ServiceName string `json:"serviceName,omitempty"`
	Authority   string `json:"authority,omitempty"`
	MultiMode   bool   `json:"multiMode,omitempty"`
}

// XHTTPSettings configures the XHTTP (splithttp) transport.
type XHTTPSettings struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
	Mode string `json:"mode,omitempty"`
}

// SOCKSConfig returns a config with a single SOCKS5 inbound on listen
// (host:port) that sends everything to out.
func SOCKSConfig(host string, port uint16, out Outbound, logLevel string) Config {
	if out.Tag == "" {
		out.Tag = "proxy"
	}
	return Config{
		Log:       &Log{Access: "none", Level: logLevel},
		Inbounds:  []Inbound{socksInbound(host, port)},
		Outbounds: []Outbound{out},
	}
}
