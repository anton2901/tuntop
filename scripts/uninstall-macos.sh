#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Remove tuntop installed by install-macos.sh. Run as root:
#
#   sudo scripts/uninstall-macos.sh [--purge]
#
# Stopping the service turns the VPN off and restores routes and DNS.
# --purge also deletes subscriptions (/Library/Application Support/tuntop)
# and the geo databases.
set -euo pipefail

PURGE=""
[[ "${1:-}" == --purge ]] && PURGE=1
[[ $EUID -eq 0 ]] || { echo "run as root: sudo $0" >&2; exit 1; }

launchctl bootout system/org.tuntop.tuntopd 2>/dev/null || true
rm -f /Library/LaunchDaemons/org.tuntop.tuntopd.plist
if [[ -n "${SUDO_USER:-}" && "$SUDO_USER" != root ]]; then
    uid=$(id -u "$SUDO_USER")
    home=$(dscl . -read "/Users/$SUDO_USER" NFSHomeDirectory | awk '{print $2}')
    launchctl bootout "gui/$uid/org.tuntop.notify" 2>/dev/null || true
    rm -f "$home/Library/LaunchAgents/org.tuntop.notify.plist"
fi
rm -f /usr/local/bin/tuntop /usr/local/bin/tuntopd
if [[ -n "$PURGE" ]]; then
    rm -rf "/Library/Application Support/tuntop" /usr/local/share/tuntop /var/log/tuntopd.log
    echo "removed tuntop and its data"
else
    echo "removed tuntop; subscriptions kept in /Library/Application Support/tuntop (--purge deletes them)"
fi
