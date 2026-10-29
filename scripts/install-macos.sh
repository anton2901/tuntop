#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install tuntop on macOS (Apple Silicon or Intel). Run as root from a
# source tree after `make build-darwin`, or from a folder holding the
# binaries and plists:
#
#   sudo scripts/install-macos.sh [--no-geo]
#
# Installs tuntop and tuntopd into /usr/local/bin, the service as a
# LaunchDaemon (started now and at boot; the VPN itself starts off), the
# notifications LaunchAgent for the user who ran sudo, and the geoip/geosite
# databases provider profiles need (downloaded unless --no-geo or present).
# Nothing here changes network settings: that happens only on "tuntop on".
set -euo pipefail

GEO=1
for a in "$@"; do
    case "$a" in
        --no-geo) GEO="" ;;
        -h | --help) sed -n '3,14p' "$0"; exit 0 ;;
        *) echo "unknown option: $a" >&2; exit 2 ;;
    esac
done
[[ $EUID -eq 0 ]] || { echo "run as root: sudo $0" >&2; exit 1; }
[[ $(uname -s) == Darwin ]] || { echo "this is the macOS installer; on Linux use install.sh" >&2; exit 1; }

arch=$(uname -m) # arm64 or x86_64
[[ $arch == x86_64 ]] && arch=amd64
here=$(cd "$(dirname "$0")" && pwd)
find_file() { # name, candidate dirs…
    local name=$1; shift
    for d in "$@"; do
        [[ -f "$d/$name" ]] && { echo "$d/$name"; return; }
    done
    echo "cannot find $name (looked in: $*); build with: make build-darwin" >&2
    exit 1
}
# Next to this script, the archive layout (scripts/dist-macos.sh), or the
# source tree.
bins=("$here" "$here/bin/darwin-$arch" "$here/../bin/darwin-$arch")
plists=("$here" "$here/../packaging/launchd")

tuntop=$(find_file tuntop "${bins[@]}")
tuntopd=$(find_file tuntopd "${bins[@]}")
daemon_plist=$(find_file org.tuntop.tuntopd.plist "${plists[@]}")
agent_plist=$(find_file org.tuntop.notify.plist "${plists[@]}")

echo "installing into /usr/local/bin"
install -d -m 0755 /usr/local/bin
for b in "$tuntop" "$tuntopd"; do
    install -m 0755 "$b" "/usr/local/bin/.$(basename "$b").new"
    mv -f "/usr/local/bin/.$(basename "$b").new" "/usr/local/bin/$(basename "$b")"
    # Built locally or downloaded: drop the quarantine flag if any.
    xattr -d com.apple.quarantine "/usr/local/bin/$(basename "$b")" 2>/dev/null || true
done

geo=/usr/local/share/tuntop
install -d -m 0755 "$geo"
if [[ -n "$GEO" ]]; then
    for f in geoip.dat geosite.dat; do
        if [[ -s "$geo/$f" ]]; then
            echo "$f: already present"
            continue
        fi
        echo "downloading $f"
        curl -fsSL --retry 3 -o "$geo/$f.tmp" \
            "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/$f"
        mv -f "$geo/$f.tmp" "$geo/$f"
    done
fi

# reload DOMAIN LABEL PLIST: replace a loaded launchd job. bootout returns
# before the job is gone; bootstrapping too early fails with
# "Input/output error", so wait for it to disappear and retry.
reload() {
    local domain=$1 label=$2 plist=$3
    launchctl bootout "$domain/$label" 2>/dev/null || true
    for _ in $(seq 1 50); do
        launchctl print "$domain/$label" >/dev/null 2>&1 || break
        sleep 0.2
    done
    for _ in 1 2 3 4 5; do
        launchctl bootstrap "$domain" "$plist" 2>/dev/null && return 0
        sleep 1
    done
    launchctl bootstrap "$domain" "$plist" # last try, with its error shown
}

# Service: replace a running one cleanly (stopping it turns the VPN off).
dst=/Library/LaunchDaemons/org.tuntop.tuntopd.plist
launchctl bootout system/org.tuntop.tuntopd 2>/dev/null || true
install -m 0644 -o root -g wheel "$daemon_plist" "$dst"
reload system org.tuntop.tuntopd "$dst"
launchctl enable system/org.tuntop.tuntopd

# Notifications for the user who ran sudo.
if [[ -n "${SUDO_USER:-}" && "$SUDO_USER" != root ]]; then
    uid=$(id -u "$SUDO_USER")
    home=$(dscl . -read "/Users/$SUDO_USER" NFSHomeDirectory | awk '{print $2}')
    agent="$home/Library/LaunchAgents/org.tuntop.notify.plist"
    install -d -o "$SUDO_USER" "$home/Library/LaunchAgents"
    install -m 0644 -o "$SUDO_USER" "$agent_plist" "$agent"
    reload "gui/$uid" org.tuntop.notify "$agent" || echo "notifications: log out and in to start them"
fi

sleep 1
/usr/local/bin/tuntopd version
cat <<EOF

tuntop is installed. The service starts with the system; the VPN starts off.

  tuntop                     interface
  tuntop on / tuntop off     turn the VPN on / off

Turn other VPNs (Happ etc.) off before "tuntop on": two at once do not work.
Only administrators (group "admin") can control the VPN.
Log: /var/log/tuntopd.log
EOF
