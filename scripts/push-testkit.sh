#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Build for linux/amd64 and copy the binaries, the installer and the unit
# files (plus the manual test kit in testkit/, if present) to a test
# machine over ssh. There: sudo ~/<remote-dir>/install.sh.
#
#   scripts/push-testkit.sh <ssh-host> [remote-dir]
set -euo pipefail

HOST=${1:?usage: $0 <ssh-host> [remote-dir]}
DIR=${2:-tuntop-test}
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10)

cd "$(dirname "$0")/.."
make build-linux

files=(bin/linux-amd64/tuntop bin/linux-amd64/tuntopd
    scripts/install.sh scripts/uninstall.sh
    packaging/systemd/tuntopd.service packaging/systemd/tuntop-notify.service)
for f in testkit/README.ru.md testkit/lib.sh testkit/test-socks.sh testkit/test-tun.sh testkit/measure.sh testkit/service.sh; do
    [[ -f $f ]] && files+=("$f")
done

# Upload next to the target and rename: a running binary cannot be
# overwritten in place ("text file busy"), but it can be replaced.
"${SSH[@]}" "$HOST" "mkdir -p '$DIR/.incoming'"
scp -q -o BatchMode=yes -o ConnectTimeout=10 "${files[@]}" "$HOST:$DIR/.incoming/"
"${SSH[@]}" "$HOST" "cd '$DIR' && mv -f .incoming/* . && rmdir .incoming && chmod +x tuntop tuntopd *.sh && ./tuntopd version"
