// SPDX-License-Identifier: GPL-3.0-or-later

// Package version holds build metadata injected at link time.
//
// The single source of truth for the release version is the git tag
// (vX.Y.Z). The Makefile and GoReleaser pass it via -ldflags:
//
//	-X tuntop/internal/version.Version=0.1.0
package version

import "fmt"

// Set by -ldflags at build time. Defaults identify a local dev build.
var (
	Version = "0.0.0-dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return fmt.Sprintf("%s (commit %s, built %s)", Version, Commit, Date)
}
