# tuntop

A free and open-source terminal (TUI) proxy/VPN client for Linux and macOS,
built on [Xray-core](https://github.com/XTLS/Xray-core). Think Happ or v2rayN,
but in your terminal: subscriptions, a system-wide tunnel, latency tests and a
kill switch, in a btop-style interface that fits 80×24.

> **Status:** pre-1.0, used daily on CachyOS and macOS (Apple Silicon).
> See [CHANGELOG.md](CHANGELOG.md) for what each release changed.

## Features

- **Subscriptions** from providers that serve Happ: Xray JSON profiles
  (including provider groups with balancers, used as they are) and plain or
  base64 link lists. Happ-compatible User-Agent and device headers, per
  subscription; provider info: traffic, expiry, announcements. Automatic
  refresh; blocked subscription domains are fetched through another
  provider's server.
- **Links:** `vless://` with REALITY or TLS over TCP, WebSocket,
  HTTPUpgrade, gRPC and XHTTP; `vmess://`, `trojan://`, `ss://` and
  `hysteria2://` (`hy2://`).
- **System-wide TUN** on Linux (policy routing, systemd-resolved) and macOS
  (utun, split routes); the LAN stays direct, DNS goes through the tunnel.
- **Kill switch:** while the VPN is on, nothing leaves the machine outside
  the tunnel, also during server switches and failed reconnects. nftables
  on Linux, pf on macOS.
- **Proxy mode** as an alternative to TUN (`tuntop mode proxy`): only local
  SOCKS5 (`127.0.0.1:10808`) and HTTP (`:10809`) ports; routing, DNS and the
  rest of the system are left alone. There is no kill switch in this mode.
- **Auto-switch** (off by default): when the server stops answering, moves
  to a working one of the same subscription (the fastest known first) and
  notifies you; if none works it returns to the original.
- **Latency tests** (manual or every 10 min – 6 h) with a short history per
  server; **favorites** listed first; **subscription order** of your choice.
- **Tunnel health:** a check through the tunnel every minute keeps the
  status latency fresh, notifies when the server stops answering, and
  reconnects after sleep. Optional **auto-connect** at start.
- **Live traffic** graph and totals, service log, desktop notifications.
- **TUI and CLI** over the same service: closing the TUI leaves the VPN as
  it is. English and Russian, five themes, mouse support.

Not yet: `tuic://` links, Clash and sing-box formats, WireGuard, server
priorities and a backup subscription for auto-switch, a system-wide proxy
setting for the proxy mode.

## How it works

| Binary | Runs as | Role |
|--------|---------|------|
| `tuntopd` | root (systemd / launchd service) | subscriptions, the tunnel, routing, kill switch |
| `tuntop`  | your user | TUI without arguments, CLI with them |

They talk over a unix socket that only administrators can open: group
`wheel` or `sudo` on Linux (whichever exists), `admin` on macOS. The service starts with the system with the VPN off.

## Installing

Linux with systemd (amd64, arm64) or macOS 13+ (Apple Silicon, Intel).

**One command** (install; see [Updating](#updating) to update):

```bash
curl -fsSL https://raw.githubusercontent.com/anton2901/tuntop/main/scripts/get.sh | sh
```

[The script](scripts/get.sh) downloads the latest release for your system,
checks it against the release's `checksums.txt` and runs the installer from
the archive with `sudo`. Read it first if you like; `TUNTOP_VERSION=0.3.0`
picks a version, `TUNTOP_DOWNLOAD=1` only downloads and verifies. Release
candidates (pre-releases) are skipped unless you pick one by version.

**Packages** (Debian, Ubuntu, Fedora…): take the `.deb` or `.rpm` from
[Releases](https://github.com/anton2901/tuntop/releases):

```bash
sudo apt install ./tuntop_<version>_amd64.deb
sudo dnf install ./tuntop-<version>-1.x86_64.rpm
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

## Updating

```bash
tuntop upgrade              # the latest release
tuntop upgrade --pre        # the newest, release candidates included
tuntop upgrade --version X  # exactly X, also to go back
tuntop upgrade --check      # only show what would be installed
```

`tuntop upgrade` asks GitHub for the release, then runs the same
[install script](scripts/get.sh) as the first install: it verifies the
download against `checksums.txt`, installs with `sudo` and restarts the
service. **The VPN is off afterwards**; turn it on again with `tuntop on`
(or enable auto-connect). Subscriptions and settings stay. It does nothing
if you already have the newest version. With a package manager, install the
new `.deb`/`.rpm` instead.

Release candidates (`v0.3.0-rc.1`) are published as GitHub pre-releases to
try changes before a release; they are never installed unless you ask with
`--pre` or `--version`. tuntop does not check for new versions by itself.

## Using

Everything is done in the terminal interface. Run:

```bash
tuntop
```

**First time:**

1. Press `4` (subscriptions), then `a`, paste your provider's subscription
   link (the one you would give Happ or v2rayN) and press `Enter`. Servers
   appear on the servers tab (`3`).
2. Pick a server with `↑↓` and press `Enter`: the status panel turns
   **● ON** and all traffic goes through it.
3. `o` turns the VPN off and on again (with the last server). Quitting
   the interface (`q`) keeps the VPN as it is.

**Every day:** `tuntop`, then `o`. Or turn on auto-connect in the
settings and the VPN comes up by itself when the computer starts.

**Keys** (`?` shows them all):

| Key | Action |
|-----|--------|
| `↑↓` `Enter` | select a server, connect |
| `o` | VPN on (last server) or off |
| `t` / `T` | measure latency: all servers / the one under the cursor |
| `*` | favorite: listed first |
| `/` | filter servers |
| `i` | server details |
| `3` `4` `5` | tabs: servers, subscriptions, log |
| `a` `x` `Shift+↑↓` | subscriptions tab: add, remove, move |
| `Esc` | menu: settings (language, theme, latency test, mode, kill switch, auto-connect, auto-switch), help, about |

The mouse works too: click to select, double-click to connect. Keys work
in a Russian keyboard layout as well.

**Command line (optional).** The same actions without the interface, for
scripts, status bars or ssh sessions:

```bash
tuntop status                      # state, server, traffic
tuntop on [server]                 # connect: number or part of a name
tuntop off                         # back to direct traffic
tuntop servers                     # list servers
tuntop sub add <link>              # add a subscription
tuntop killswitch on|off           # block traffic outside the tunnel
tuntop autoconnect on|off          # connect when the service starts
tuntop autoswitch on|off           # move to a working server when one dies
tuntop mode tun|proxy [port]       # whole system (default) or local SOCKS/HTTP
tuntop upgrade [--pre]             # update from GitHub, see Updating
```

`tuntop help` lists the rest: `test`, `fav`, `sub rm|mv`, `update`, `add`
(single share links), `notify`, `version`. `TUNTOP_LANG=en|ru` sets
the language.

## Privacy

- To get the full server list, tuntop sends providers the same device
  headers as Happ, including `x-hwid`: a hash of this machine's id, the same
  after reinstalling tuntop. Providers count it as a device: when their
  device limit is reached they send notices instead of servers, and tuntop
  shows their text. Turn it off per
  subscription with `tuntop sub add -no-hwid …`.
- tuntop contacts nothing but your providers and their servers; there is no
  telemetry, no external IP lookup and no version check. Only when you run
  `tuntop upgrade` does it talk to github.com. Latency tests request
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

## Limits and threat model

What tuntop does **not** protect you from, so you can decide for yourself:

- **IPv6 on Linux.** Only IPv4 is routed into the tunnel. If your machine
  has working IPv6, connections to IPv6 addresses (and IPv6 DNS servers)
  bypass the VPN and reveal your real address. With the kill switch on they
  are blocked instead of leaking (the LAN stays reachable). If you do not
  need IPv6, disable it for the connection or system-wide. macOS routes
  IPv6 through the tunnel.
- **Provider profiles are trusted.** An Xray JSON profile is used as the
  provider wrote it, including its routing rules. A rule that sends some
  domains `direct` takes them outside the tunnel **and outside the kill
  switch**; tuntop does not warn about this. The inbounds are always
  replaced, so a profile cannot open a listener of its own.
- **The kill switch has gaps by design.** It starts after the first
  successful connect and does nothing while the VPN is off. On macOS it
  cannot tell root processes from the tunnel, so those pass. See "Kill
  switch details".
- **Proxy mode is open to local users.** The SOCKS5 and HTTP ports listen
  on `127.0.0.1` without a password and relay UDP; any local process or
  user can use your VPN through them. There is no kill switch in this mode.
- **The service log is world-readable** (`/var/log/tuntop/tuntopd.log`,
  mode 0644). Links, subscription URL paths, UUIDs, keys and IP addresses
  are masked, but server and subscription names (usually the provider's
  domain) and connection times are not. On a multi-user machine, restrict
  the file yourself (`chmod 640`, then set a group).
- **Subscription URLs may be plain `http://`.** They are credentials: use
  `https://`. Redirects to other hosts also receive the device headers.
- **Your provider sees** the device id (hash), the subscription URL, and
  everything that leaves the exit server. A subscription fetched through
  another provider's server (when its domain is blocked) is visible to
  that provider.
- **Link limits.** `vmess://` supports `tcp`, `ws`, `httpupgrade`, `grpc`
  and `xhttp` (not `h2`, `http`, `quic`, `kcp`). `ss://` with `plugin=` is
  not supported. `allowInsecure` / `insecure=1` is ignored (Xray removed
  it): servers with self-signed certificates need `pinSHA256`, otherwise
  they do not connect. Such links are rejected with an error.

Anyone in the socket group (`wheel`, `sudo` or `admin`) can control the
service, and the service runs as root. Treat that group as root-equivalent.

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
