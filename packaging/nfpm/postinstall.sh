#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
# deb/rpm: start the service (the VPN itself starts off), or restart it on
# an upgrade.
set -e
command -v systemctl >/dev/null 2>&1 || exit 0
systemctl daemon-reload || true
if systemctl is-active --quiet tuntopd; then
    systemctl restart tuntopd || true
else
    systemctl enable --now tuntopd || true
fi
cat <<'EOF'
tuntop is installed. The service starts with the system; the VPN starts off.
  tuntop                                        interface
  systemctl --user enable --now tuntop-notify   desktop notifications
Provider rules with geoip:/geosite: need geoip.dat and geosite.dat in
/usr/share/v2ray or /usr/local/share/tuntop (see the README).
EOF
