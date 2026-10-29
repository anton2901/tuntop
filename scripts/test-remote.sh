#!/usr/bin/env bash
# SPDX-License-Identifier: GPL-3.0-or-later
#
# Run the unit tests on a Linux machine that has no Go: cross-compile one
# test binary per package, copy them over ssh and run them there as the
# login user. Linux-only code (netcfg_linux.go) is tested only this way.
# The tests replace every system command; they do not touch the network.
#
#   scripts/test-remote.sh <ssh-host> [remote-dir]
set -euo pipefail

HOST=${1:?usage: $0 <ssh-host> [remote-dir]}
DIR=${2:-tuntop-test/unit}
SSH=(ssh -o BatchMode=yes -o ConnectTimeout=10)

cd "$(dirname "$0")/.."
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

pkgs=$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
for p in $pkgs; do
    name=$(echo "$p" | tr / _)
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go test -c -o "$out/$name.test" "$p"
done

"${SSH[@]}" "$HOST" "rm -rf '$DIR' && mkdir -p '$DIR'"
scp -q -o BatchMode=yes -o ConnectTimeout=10 "$out"/*.test "$HOST:$DIR/"
# Each binary runs in its own empty directory, like "go test" in a package
# directory without the sources.
"${SSH[@]}" "$HOST" "cd '$DIR' && fail=0 && for t in *.test; do
    d=\${t%.test}; mkdir -p \"\$d\" && cd \"\$d\"
    if ../\$t -test.count=1 >../\$d.log 2>&1; then echo \"ok   \$d\"; else echo \"FAIL \$d\"; tail -20 ../\$d.log; fail=1; fi
    cd ..
done; exit \$fail"
