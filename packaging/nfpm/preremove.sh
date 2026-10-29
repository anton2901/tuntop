#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
# deb/rpm: stop the service before its files go (routing is restored on
# stop). On an upgrade the new package restarts it.
command -v systemctl >/dev/null 2>&1 || exit 0
case "$1" in
    upgrade | 1) exit 0 ;; # deb upgrade / rpm: one version stays installed
esac
systemctl disable --now tuntopd || true
