# tuntop

A free and open-source terminal (TUI) proxy/VPN client for Linux and macOS,
built on [Xray-core](https://github.com/XTLS/Xray-core). Think Happ or v2rayN,
but in your terminal: subscriptions, a system-wide tunnel, latency tests and a
kill switch, in a btop-style interface that fits 80×24.

> **Status:** first release, v0.1.0. Used daily on CachyOS and macOS
> (Apple Silicon). See [CHANGELOG.md](CHANGELOG.md).

## Features

- **Subscriptions** from providers that serve Happ: Xray JSON profiles
  (including provider groups with balancers, used as they are) and plain or
  base64 link lists. Happ-compatible User-Agent and device headers, per
  subscription; provider info: traffic, expiry, announcements. Automatic
  refresh; blocked subscription domains are fetched through another
  provider's server.
- **Links:** `vless://` with REALITY or TLS over TCP, WebSocket,
  HTTPUpgrade, gRPC and XHTTP.
- **System-wide TUN** on Linux (policy routing, systemd-resolved) and macOS
  (utun, split routes); the LAN stays direct, DNS goes through the tunnel.
- **Kill switch:** while the VPN is on, nothing leaves the machine outside
  the tunnel, also during server switches and failed reconnects. nftables
  on Linux, pf on macOS.
- **Latency tests** (manual or every 10 min – 6 h) with a short history per
  server; **favorites** listed first; **subscription order** of your choice.
- **Live traffic** graph and totals, service log, desktop notifications.
- **TUI and CLI** over the same service: closing the TUI leaves the VPN as
  it is. English and Russian, five themes, mouse support.

Not yet: other link types (trojan, ss, vmess, hysteria2), Clash and sing-box
formats, a local proxy mode instead of TUN, automatic failover. See
[docs/tasks/mvp.md](docs/tasks/mvp.md) for the plan.

## How it works

| Binary | Runs as | Role |
|--------|---------|------|
| `tuntopd` | root (systemd / launchd service) | subscriptions, the tunnel, routing, kill switch |
| `tuntop`  | your user | TUI without arguments, CLI with them |

They talk over a unix socket that only administrators can open: group
`wheel` or `sudo` on Linux (whichever exists), `admin` on macOS. The service starts with the system with the VPN off.

## Installing

Linux with systemd (amd64, arm64) or macOS 13+ (Apple Silicon, Intel).

**One command** (install or update):

```bash
curl -fsSL https://raw.githubusercontent.com/anton2901/tuntop/main/scripts/get.sh | sh
```

[The script](scripts/get.sh) downloads the latest release for your system,
checks it against the release's `checksums.txt` and runs the installer from
the archive with `sudo`. Read it first if you like; `TUNTOP_VERSION=0.1.0`
picks a version, `TUNTOP_DOWNLOAD=1` only downloads and verifies.

**Packages** (Debian, Ubuntu, Fedora…): take the `.deb` or `.rpm` from
[Releases](https://github.com/anton2901/tuntop/releases):

```bash
sudo apt install ./tuntop_0.1.0_amd64.deb
sudo dnf install ./tuntop-0.1.0-1.x86_64.rpm
```

**Archive:** download `tuntop_<version>_<os>_<arch>.tar.gz` from
[Releases](https://github.com/anton2901/tuntop/releases), unpack, then
`sudo ./install.sh` (Linux) or `sudo ./install-macos.sh` (macOS).

**From source:** build (see below), then `sudo scripts/install.sh` or
`sudo scripts/install-macos.sh`.

What the installers do: put `tuntop` and `tuntopd` into `/usr/local/bin`,
install the service (it starts with the system, the VPN starts off) and
desktop notifications, and download the geoip/geosite databases that
provider rules need unless the system has them (on Arch: `v2ray-geoip`,
`v2ray-domain-list-community`). Enable notifications on Linux with
`systemctl --user enable --now tuntop-notify`. The kill switch needs
`nftables` on Linux.

Turn other VPNs off before `tuntop on`: two at once do not work (on macOS
tuntop refuses to connect while another one owns the default route).

Remove with `sudo ./uninstall.sh [--purge]` or
`sudo ./uninstall-macos.sh [--purge]` from the archive (or `scripts/`), or
with your package manager.

## Using

```bash
tuntop sub add https://provider.example/sub/…   # add a subscription
tuntop servers                                  # list servers
tuntop on 3                                     # connect (number or part of a name)
tuntop status                                   # state, server, traffic
tuntop off                                      # back to direct traffic
tuntop killswitch on                            # block traffic outside the tunnel
tuntop                                          # the TUI
```

Other commands: `test`, `fav`/`unfav`, `sub rm`, `sub mv`, `update`, `add`
(single links), `rm`, `notify`, `version`; `tuntop help` lists them.

In the TUI, `?` shows all keys. The main ones:

| Key | Action |
|-----|--------|
| `↑↓` `Enter` | select, connect |
| `o` | turn the VPN on (last server) or off |
| `t` / `T` | latency of all servers / the one under the cursor |
| `*` | favorite |
| `/` | filter |
| `3` `4` `5` | servers, subscriptions, log |
| `a` `x` `Shift+↑↓` | add, remove, move a subscription |
| `Esc` | menu: settings (language, theme, latency test, kill switch) |

`TUNTOP_LANG=en|ru` overrides the language, `TUNTOP_SOCKET` the socket path.

## Privacy

- To get the full server list, tuntop sends providers the same device
  headers as Happ, including `x-hwid`: a hash of this machine's id, the same
  after reinstalling tuntop. Providers count it as a device: when their
  device limit is reached they send notices instead of servers, and tuntop
  shows their text. Turn it off per
  subscription with `tuntop sub add -no-hwid …`.
- tuntop contacts nothing but your providers and their servers; there is no
  telemetry and no external IP lookup. Latency tests request
  `https://www.gstatic.com/generate_204` through each server. The macOS
  installer downloads the geo databases from GitHub
  ([Loyalsoldier/v2ray-rules-dat](https://github.com/Loyalsoldier/v2ray-rules-dat)).
- Subscription URLs are credentials: they are stored root-only (0600) and
  masked in the API and the log.

## Kill switch details

- On with `tuntop killswitch on` (or in the TUI settings); it takes effect on
  the next connect. `tuntop off` turns the VPN off **and** lifts the block.
- If the tunnel drops or a reconnect fails, traffic stays blocked
  ("BLOCKED" in the status) until you connect again or run `tuntop off`.
- If the service crashes, the block stays until it restarts (a few
  seconds); it then starts with the VPN off and removes the block.
- Always allowed: loopback, the LAN and link-local ranges, DHCP.
- macOS: sockets cannot be marked there, so traffic of root processes
  passes (the tunnel itself runs as root). The switch protects your
  applications, not system daemons.

## Building

Requires Go 1.27+ and [golangci-lint](https://golangci-lint.run/) v2.

```bash
make                # lint (for this OS and Linux), test, build into bin/
make build-linux    # linux/amd64 into bin/linux-amd64/
make build-darwin   # macOS arm64 and amd64 into bin/darwin-*/
make test-remote HOST=<ssh-host>   # unit tests on a Linux machine without Go
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Security issues: [SECURITY.md](SECURITY.md).

## License

Copyright (C) 2026 AV

tuntop is free software: you can redistribute it and/or modify it under the
terms of the GNU General Public License as published by the Free Software
Foundation, either version 3 of the License, or (at your option) any later
version. See [LICENSE](LICENSE).
