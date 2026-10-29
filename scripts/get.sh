#!/bin/sh
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Install or update tuntop from GitHub releases:
#
#   curl -fsSL https://raw.githubusercontent.com/anton2901/tuntop/main/scripts/get.sh | sh
#
# Finds the latest release for this OS and CPU, downloads its archive and
# checksums.txt, verifies the archive, unpacks it and runs its installer
# with sudo (Linux: install.sh, macOS: install-macos.sh). Running it again
# updates tuntop; subscriptions and settings stay. The installer restarts
# the service, so the VPN is off afterwards.
#
# Environment:
#   TUNTOP_VERSION   install this version (e.g. 0.1.0) instead of the latest
#   TUNTOP_DOWNLOAD  only download and verify; print where the files are
#   TUNTOP_REPO      owner/name on GitHub (default anton2901/tuntop)
#   TUNTOP_BASE_URL  take the files from here instead of GitHub (testing)
set -eu

repo=${TUNTOP_REPO:-anton2901/tuntop}

say() { printf 'tuntop: %s\n' "$*"; }
die() { printf 'tuntop: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

have curl || die "curl is required"
have tar || die "tar is required"
have bash || die "bash is required"

case $(uname -s) in
    Linux) os=linux; installer=install.sh ;;
    Darwin)
        os=darwin; installer=install-macos.sh
        major=$(sw_vers -productVersion | cut -d. -f1)
        [ "$major" -ge 13 ] || die "macOS 13 or newer is required (this is $(sw_vers -productVersion))"
        ;;
    *) die "unsupported system: $(uname -s) (Linux and macOS only)" ;;
esac
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) die "unsupported CPU: $(uname -m) (amd64 and arm64 only)" ;;
esac
[ "$os" = linux ] && ! have systemctl && die "systemd is required on Linux"

version=${TUNTOP_VERSION:-}
if [ -z "$version" ]; then
    [ -z "${TUNTOP_BASE_URL:-}" ] || die "set TUNTOP_VERSION with TUNTOP_BASE_URL"
    # /releases/latest redirects to /releases/tag/vX.Y.Z.
    url=$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
        die "cannot reach github.com"
    version=${url##*/v}
    case $version in
        [0-9]*) ;;
        *) die "no release found at https://github.com/$repo/releases" ;;
    esac
fi
version=${version#v}

base=${TUNTOP_BASE_URL:-https://github.com/$repo/releases/download/v$version}
name="tuntop_${version}_${os}_${arch}"

tmp=$(mktemp -d)
if [ -z "${TUNTOP_DOWNLOAD:-}" ]; then
    trap 'rm -rf "$tmp"' EXIT
fi

say "downloading tuntop $version for $os/$arch"
curl -fsSL -o "$tmp/$name.tar.gz" "$base/$name.tar.gz" || die "download failed: $base/$name.tar.gz"
curl -fsSL -o "$tmp/checksums.txt" "$base/checksums.txt" || die "download failed: $base/checksums.txt"

want=$(awk -v f="$name.tar.gz" '$2 == f { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "$name.tar.gz is not in checksums.txt"
if have sha256sum; then
    got=$(sha256sum "$tmp/$name.tar.gz" | cut -d' ' -f1)
else
    got=$(shasum -a 256 "$tmp/$name.tar.gz" | cut -d' ' -f1)
fi
[ "$got" = "$want" ] || die "checksum mismatch for $name.tar.gz: refusing to install"
say "checksum ok"

tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
[ -f "$tmp/$name/$installer" ] || die "$installer not found in the archive"

if [ -n "${TUNTOP_DOWNLOAD:-}" ]; then
    say "downloaded to $tmp/$name; to install: sudo bash $tmp/$name/$installer"
    exit 0
fi

say "installing (sudo asks for your password)"
if [ "$(id -u)" -eq 0 ]; then
    bash "$tmp/$name/$installer"
else
    # The installer runs from a pipe: sudo reads the password from the terminal.
    sudo bash "$tmp/$name/$installer" </dev/tty
fi
