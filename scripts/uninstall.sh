#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Remove tuntop installed by install.sh. Run as root:
#
#   sudo scripts/uninstall.sh [--prefix /usr/local] [--purge]
#
# --purge also deletes /var/lib/tuntop (subscriptions and selection).
# Each user's notification service is stopped at their next login; to stop
# it now: systemctl --user disable --now tuntop-notify
set -euo pipefail

PREFIX=/usr/local
PURGE=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --prefix) PREFIX=$2; shift 2 ;;
        --purge) PURGE=1; shift ;;
        -h | --help) sed -n '3,11p' "$0"; exit 0 ;;
        *) echo "unknown option: $1" >&2; exit 2 ;;
    esac
done
[[ $EUID -eq 0 ]] || { echo "run as root: sudo $0" >&2; exit 1; }

# Stopping the service turns the VPN off and restores routing.
systemctl disable --now tuntopd 2>/dev/null || true
rm -f /etc/systemd/system/tuntopd.service /etc/systemd/user/tuntop-notify.service
rm -f "$PREFIX/bin/tuntop" "$PREFIX/bin/tuntopd"
systemctl daemon-reload
if [[ -n "$PURGE" ]]; then
    rm -rf /var/lib/tuntop
    echo "removed tuntop and its data"
else
    echo "removed tuntop; subscriptions are kept in /var/lib/tuntop (use --purge to delete)"
fi
