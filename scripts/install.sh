#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install tuntop on a Linux machine with systemd. Run as root:
#
#   sudo scripts/install.sh [--prefix /usr/local] [--no-geo]
#
# Installs tuntop and tuntopd into PREFIX/bin, the tuntopd system service
# (enabled and started; the VPN itself starts off) and the tuntop-notify
# user service for desktop notifications. Binaries and unit files are
# taken from next to this script or from the source tree. The geoip and
# geosite databases provider rules need are downloaded into
# /usr/local/share/tuntop unless the system already has them (Arch:
# v2ray-geoip, v2ray-domain-list-community) or --no-geo is given.
set -euo pipefail

PREFIX=/usr/local
GEO=1
while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix) PREFIX=$2; shift 2 ;;
        --no-geo) GEO=""; shift ;;
        -h | --help) sed -n '3,15p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
done

[[ $EUID -eq 0 ]] || { echo "run as root: sudo $0" >&2; exit 1; }
command -v systemctl >/dev/null || { echo "systemd is required" >&2; exit 1; }

here=$(cd "$(dirname "$0")" && pwd)
find_file() { # name, candidate dirs…
    local name=$1; shift
    for d in "$@"; do
        [[ -f "$d/$name" ]] && { echo "$d/$name"; return; }
    done
    echo "cannot find $name (looked in: $*)" >&2
    exit 1
}
bins=("$here" "$here/../bin/linux-amd64" "$here/../bin")
units=("$here" "$here/../packaging/systemd")

tuntop=$(find_file tuntop "${bins[@]}")
tuntopd=$(find_file tuntopd "${bins[@]}")
svc=$(find_file tuntopd.service "${units[@]}")
notify=$(find_file tuntop-notify.service "${units[@]}")

echo "installing into $PREFIX/bin"
# Replace via rename: running binaries cannot be overwritten in place.
for b in "$tuntop" "$tuntopd"; do
    install -m 0755 "$b" "$PREFIX/bin/.$(basename "$b").new"
    mv -f "$PREFIX/bin/.$(basename "$b").new" "$PREFIX/bin/$(basename "$b")"
done

# Geo databases: same search order as tuntopd (internal/xray/profile.go).
geo_found=""
for d in /usr/local/share/tuntop /usr/share/tuntop /usr/local/share/xray /usr/share/xray \
    /opt/share/xray /usr/share/v2ray /usr/local/share/v2ray; do
    if [[ -s "$d/geoip.dat" && -s "$d/geosite.dat" ]]; then geo_found=$d; break; fi
done
if [[ -n "$geo_found" ]]; then
    echo "geo databases: $geo_found"
elif [[ -n "$GEO" ]]; then
    geo=/usr/local/share/tuntop
    install -d -m 0755 "$geo"
    for f in geoip.dat geosite.dat; do
        echo "downloading $f"
        curl -fsSL --retry 3 -o "$geo/$f.tmp" \
            "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/$f"
        mv -f "$geo/$f.tmp" "$geo/$f"
    done
else
    echo "no geo databases: provider rules with geoip:/geosite: will fail" >&2
fi

sed "s|/usr/bin/|$PREFIX/bin/|g" "$svc" >/etc/systemd/system/tuntopd.service
install -d /etc/systemd/user
sed "s|/usr/bin/|$PREFIX/bin/|g" "$notify" >/etc/systemd/user/tuntop-notify.service

systemctl daemon-reload
if systemctl is-active --quiet tuntopd; then
    echo "restarting tuntopd (the VPN will be off until you turn it on)"
    systemctl restart tuntopd
else
    systemctl enable --now tuntopd
fi
systemctl enable tuntopd >/dev/null 2>&1

"$PREFIX/bin/tuntopd" version
group=wheel
getent group wheel >/dev/null || group=sudo
cat <<EOF

tuntop is installed. The service starts with the system; the VPN starts off.

  tuntop                   interface
  tuntop on / tuntop off   turn the VPN on / off

Desktop notifications are off; to get them (as your user, not root):
  systemctl --user enable --now tuntop-notify

Only members of the "$group" group can control the VPN.
EOF
