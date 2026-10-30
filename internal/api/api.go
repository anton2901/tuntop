// SPDX-License-Identifier: GPL-3.0-or-later

// Package api defines the tuntopd control protocol: JSON over HTTP on a unix
// socket. The daemon serves it; the tuntop CLI and TUI use Client.
//
//	GET    /v1/status
//	GET    /v1/servers
//	GET    /v1/log                 last lines of the service log
//	GET    /v1/traffic             throughput and history
//	GET    /v1/events              server-sent events stream of api.Event
//	GET    /v1/settings
//	PUT    /v1/settings            api.Settings
//	POST   /v1/connect             {"server": "<key | number | part of name>"}
//	POST   /v1/disconnect
//	GET    /v1/subscriptions
//	POST   /v1/subscriptions       {"url": "...", "user_agent": "...", "no_hwid": false}
//	DELETE /v1/subscriptions/{id}
//	POST   /v1/subscriptions/{id}/move {"to": <1-based position>}
//	POST   /v1/favorites           {"server": "<key | number | part of name>", "favorite": true}
//	POST   /v1/update              {"id": "<subscription id or empty for all>"}
//	POST   /v1/test                {"keys": [...]} (empty: all servers) → servers with latency
//	POST   /v1/links               {"links": ["vless://...", ...]}
//	DELETE /v1/servers/{key}       (manually added links only)
//
// Errors come back as a non-2xx status with {"error": "..."}.
package api

import (
	"time"

	"github.com/anton2901/tuntop/internal/paths"
)

// ManualID is the subscription holding individually added links; it always
// comes last and cannot be moved.
const ManualID = "manual"

// DefaultSocket is where tuntopd listens (per OS, see package paths).
var DefaultSocket = paths.Socket

// Status is the daemon's current state.
type Status struct {
	Version string `json:"version"`
	// Build is the service's full version line (version, commit, date)
	// and Xray the embedded Xray-core module version.
	Build     string    `json:"build,omitempty"`
	Xray      string    `json:"xray,omitempty"`
	Connected bool      `json:"connected"`
	Server    *Server   `json:"server,omitempty"`
	Since     time.Time `json:"since,omitzero"`
	// Selected is the server "tuntop on" connects to, even when disconnected.
	Selected *Server `json:"selected,omitempty"`
	// LastError is the last connection failure, if any.
	LastError string `json:"last_error,omitempty"`
	// KillSwitch is true while traffic outside the tunnel is blocked; it
	// stays so after a failed reconnect until "tuntop off".
	KillSwitch bool `json:"kill_switch,omitempty"`
	// Mode is how the VPN is on (ModeTUN or ModeProxy). ProxyPort is the
	// SOCKS port in proxy mode (HTTP is the next one); 0 in TUN mode.
	Mode      string `json:"mode,omitempty"`
	ProxyPort int    `json:"proxy_port,omitempty"`
}

// Connection modes.
const (
	// ModeTUN routes the whole system through a TUN device (the default).
	ModeTUN = "tun"
	// ModeProxy only opens local SOCKS and HTTP proxy ports; programs
	// use them by choice and nothing else changes in the system.
	ModeProxy = "proxy"
)

// DefaultProxyPort is the SOCKS port of proxy mode; HTTP is one above.
const DefaultProxyPort = 10808

// Server is one selectable server or provider group.
type Server struct {
	// Index is the 1-based position in the current list; it changes when
	// subscriptions update, Key does not.
	Index   int    `json:"index"`
	Key     string `json:"key"`
	Name    string `json:"name"`
	Group   bool   `json:"group"`
	Proxies int    `json:"proxies"`
	Proto   string `json:"proto"`
	SubID   string `json:"sub_id"`
	SubName string `json:"sub_name"`
	// Latency is the last measured delay in ms: 0 not measured, -1 failed.
	Latency int `json:"latency"`
	// LatencyHistory holds recent measurements, oldest first (same units).
	LatencyHistory []int `json:"latency_history,omitempty"`
	// Address is host:port of the (first) proxy server.
	Address string `json:"address,omitempty"`
	// Favorite servers come first in the list.
	Favorite bool `json:"favorite,omitempty"`
}

// Traffic is the tunnel's throughput. Histories are bytes per second,
// one sample per second, oldest first; zeros while disconnected.
type Traffic struct {
	Connected bool    `json:"connected"`
	UpRate    int64   `json:"up_rate"`
	DownRate  int64   `json:"down_rate"`
	UpTotal   int64   `json:"up_total"`   // since connect
	DownTotal int64   `json:"down_total"` // since connect
	UpHist    []int64 `json:"up_hist"`
	DownHist  []int64 `json:"down_hist"`
}

// Event kinds sent on GET /v1/events.
const (
	EventConnected     = "connected"
	EventDisconnected  = "disconnected"
	EventConnectFailed = "connect_failed"
	EventUpdateFailed  = "update_failed"
	EventExpiring      = "subscription_expiring"
	// The running tunnel stopped answering, or answers again.
	EventUnreachable = "server_unreachable"
	EventRecovered   = "server_recovered"
	EventSwitched    = "server_switched"
)

// Event is something worth telling the user about (desktop notifications).
type Event struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Server string    `json:"server,omitempty"` // server name
	From   string    `json:"from,omitempty"`   // EventSwitched: the server left
	Sub    string    `json:"sub,omitempty"`    // subscription name
	Error  string    `json:"error,omitempty"`
	Days   int       `json:"days,omitempty"` // for EventExpiring
}

// Settings are the service's own settings.
type Settings struct {
	// AutoTestMinutes measures all servers' latency every N minutes;
	// 0 means only on request.
	AutoTestMinutes int `json:"auto_test_minutes"`
	// KillSwitch: while the VPN is on, block traffic outside the tunnel.
	KillSwitch bool `json:"kill_switch"`
	// AutoConnect: connect to the selected server when the service starts.
	AutoConnect bool `json:"auto_connect"`
	// AutoSwitch: when the server stops answering, move to a working one
	// of the same subscription.
	AutoSwitch bool `json:"auto_switch"`
	// Mode is ModeTUN (default when empty) or ModeProxy; ProxyPort is the
	// SOCKS port of ModeProxy (0: DefaultProxyPort).
	Mode      string `json:"mode"`
	ProxyPort int    `json:"proxy_port"`
}

// Subscription is a source of servers.
type Subscription struct {
	ID string `json:"id"`
	// URL is masked: the token is a credential.
	URL        string    `json:"url,omitempty"`
	Name       string    `json:"name"`
	Servers    int       `json:"servers"`
	UpdatedAt  time.Time `json:"updated_at,omitzero"`
	Error      string    `json:"error,omitempty"`
	Upload     int64     `json:"upload"`
	Download   int64     `json:"download"`
	Total      int64     `json:"total"`
	Expire     time.Time `json:"expire,omitzero"`
	Announce   string    `json:"announce,omitempty"`
	SupportURL string    `json:"support_url,omitempty"`
}

// ConnectRequest selects the server to connect to; empty means the
// previously selected one.
type ConnectRequest struct {
	Server string `json:"server"`
}

// AddSubscriptionRequest adds a subscription and downloads it.
type AddSubscriptionRequest struct {
	URL       string `json:"url"`
	UserAgent string `json:"user_agent,omitempty"`
	NoHWID    bool   `json:"no_hwid,omitempty"`
}

// FavoriteRequest marks or unmarks a server as favorite.
type FavoriteRequest struct {
	Server   string `json:"server"`
	Favorite bool   `json:"favorite"`
}

// MoveRequest puts a subscription at a 1-based position in the list.
type MoveRequest struct {
	To int `json:"to"`
}

// UpdateRequest refreshes one subscription, or all when ID is empty.
type UpdateRequest struct {
	ID string `json:"id,omitempty"`
}

// TestRequest measures latency of the given servers, or all when empty.
type TestRequest struct {
	Keys []string `json:"keys,omitempty"`
}

// AddLinksRequest adds individual share links.
type AddLinksRequest struct {
	Links []string `json:"links"`
}

// Error is the body of a failed request.
type Error struct {
	Error string `json:"error"`
}
