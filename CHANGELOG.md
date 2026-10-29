# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- One-command install and update from GitHub releases:
  `curl -fsSL …/scripts/get.sh | sh` (detects the system, verifies the
  archive against `checksums.txt`, runs its installer).

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

[Unreleased]: https://github.com/anton2901/tuntop/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/anton2901/tuntop/releases/tag/v0.1.0
