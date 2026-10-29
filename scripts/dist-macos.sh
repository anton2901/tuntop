#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Build a macOS archive to install tuntop on another Mac without the
# source tree: binaries for arm64 and amd64, the installer, the
# uninstaller, launchd plists and the install guide.
#
#   scripts/dist-macos.sh        → dist/tuntop-<version>-macos.zip
set -euo pipefail

cd "$(dirname "$0")/.."
make build-darwin
version=$(bin/darwin-arm64/tuntop version | awk '{print $2}')
name="tuntop-$version-macos"
stage="dist/$name"

rm -rf "$stage" "dist/$name.zip"
mkdir -p "$stage/bin"
cp -R bin/darwin-arm64 bin/darwin-amd64 "$stage/bin/"
cp scripts/install-macos.sh scripts/uninstall-macos.sh \
    packaging/launchd/org.tuntop.tuntopd.plist packaging/launchd/org.tuntop.notify.plist \
    "$stage/"
cp packaging/macos/INSTALL.ru.md "$stage/УСТАНОВКА.md"
cp LICENSE README.md "$stage/"
chmod +x "$stage"/*.sh "$stage"/bin/*/*

# ditto keeps permissions and makes a zip Finder opens with a double click.
(cd dist && ditto -c -k --norsrc --noextattr --noqtn --keepParent "$name" "$name.zip")
rm -rf "$stage"
echo "dist/$name.zip"
