# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Log file `/var/log/tuntop/tuntopd.log` (5 MB x 4, rotated) with
  timestamps; share links, subscription URL paths, UUIDs, keys and public
  IP addresses are masked (an address keeps two parts and a short hash).
  More events: connect timing and failures, subscription updates, latency
  test summary, settings changes. `tuntop logs [-n N] [-f]` shows it.
- `tuntop debug-bundle [file]`: one masked report with versions,
  settings, the log tail and the routing and DNS state, for bug reports.
- Share links `vmess://`, `trojan://`, `ss://` (SIP002 and legacy) and
  `hysteria2://` (`hy2://`; password auth, `sni`, `pinSHA256`, Salamander
  obfs). `insecure=1` is ignored: Xray no longer accepts `allowInsecure`.
- Auto-switch (`tuntop autoswitch on`, TUI settings, off by default): when
  the server stops answering, the service moves to a working server of the
  same subscription (known-fast first, up to five tries) and notifies;
  if none works it returns to the original one.
- `tuntop upgrade` updates from GitHub through `scripts/get.sh`: the latest
  release, `--pre` for the newest incl. release candidates, `--version X`
  for an exact one, `--check` to only look.
- Release candidates: a tag like `v0.3.0-rc.1` is published as a
  pre-release, which `get.sh` and `tuntop upgrade` skip unless asked.
- Proxy mode (`tuntop mode proxy [port]`, TUI settings) as an alternative
  to TUN: SOCKS5 on `127.0.0.1:10808` and HTTP on `:10809`, the system's
  routing and DNS are not touched, no kill switch. TUN stays the default.

### Fixed

- TUI: no panic on terminals 31-33 columns wide; overlays (menu,
  settings) are clipped to the screen instead of tearing it.
- A second `tuntopd` is refused instead of undoing the running one's
  routes and kill switch.
- "Off" is no longer undone by auto-switch, wake-from-sleep or
  network-change reconnects, nor by a connect that was still starting.
- macOS: the kill switch is released even if the pf token file is lost.
- A damaged `state.json` is set aside and the service starts empty.
- Connecting no longer makes the TUI show "tuntopd is not reachable";
  the status says "connecting".
- Auto-switch retries while no server answers; removing the active
  subscription turns the VPN off; leaked watcher goroutine per connection.
- Subscription URLs are shown as `scheme://host/...` only; escape and bidi
  characters in provider texts (names, titles, announcements) are removed.

## [0.2.0] - 2026-10-29

### Added

- The running tunnel is checked once a minute with a small request
  through it: the status shows a fresh latency, a server that stops
  answering (three failures in a row) and its recovery are notified, and
  the tunnel restarts after the machine wakes from sleep.
- Auto-connect (`tuntop autoconnect on`, TUI settings): the service
  connects to the last server when it starts, retrying while the network
  comes up. Off by default: the VPN still starts off.
- "About" in the TUI menu: the interface's and the service's versions
  (with a warning when they differ), the embedded Xray-core version,
  license and home page. The status API reports the service build and
  Xray-core version.

## [0.1.0] - 2026-10-29

First release. Linux (systemd) and macOS 13+ (Apple Silicon and Intel).

### Added

- `tuntopd`, a root service with the VPN off at start, and `tuntop`, a TUI
  without arguments and a CLI with them, talking over a unix socket open to
  administrators (`wheel` or `sudo` on Linux, `admin` on macOS).
- Subscriptions: Happ-compatible User-Agent and device headers (`x-hwid`
  from the machine id, optional per subscription), Xray JSON profiles
  including provider groups with balancers, plain and base64 link lists,
  provider info (traffic, expiry, announcements), automatic refresh,
  download through another provider's server when the domain is blocked,
  provider notices shown when a provider refuses the device. Stable server
  keys: the selection survives re-sorting and setting changes.
- `vless://` links with REALITY or TLS over TCP, WebSocket, HTTPUpgrade,
  gRPC and XHTTP.
- System-wide TUN through Xray, run in a child process: policy routing
  and systemd-resolved on Linux; utun, split routes, a scoped default
  route and service DNS on macOS. The LAN stays direct; DNS goes through
  the tunnel (public UDP resolvers upgraded to DoH); routing is restored
  on exit and after a crash; reconnect when the network changes or Xray
  dies.
- Kill switch (nftables on Linux, pf on macOS), off by default.
- Latency tests through each server, manual or periodic, with history;
  favorites; subscription order.
- Live traffic graph, totals and rates (also in `tuntop status`); service
  log; desktop notifications (notify-send, kdialog, Notification Center).
- btop-style TUI for 80×24 and up: panels, Esc menu, five themes, mouse,
  filter, server details; English and Russian, keys work in a Russian
  keyboard layout.
- Installers and uninstallers for Linux (systemd units, geo databases
  downloaded when missing) and macOS (LaunchDaemon and LaunchAgent).

[Unreleased]: https://github.com/anton2901/tuntop/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/anton2901/tuntop/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/anton2901/tuntop/releases/tag/v0.1.0
