// SPDX-License-Identifier: GPL-3.0-or-later

// Package paths holds per-OS locations and names: Linux follows systemd
// conventions, macOS has no /run or /var/lib and uses launchd.
package paths

import (
	"os/user"
	"runtime"
)

// Defaults for this OS.
var (
	// Socket is where tuntopd listens for clients.
	Socket = pick("/run/tuntop/tuntopd.sock", "/var/run/tuntop/tuntopd.sock")
	// StateDir keeps subscriptions and the selection (root only).
	StateDir = pick("/var/lib/tuntop", "/Library/Application Support/tuntop")
	// Group may control the VPN through the socket: the usual admin group
	// (Arch, Fedora: wheel; Debian, Ubuntu: sudo; macOS: admin).
	Group = adminGroup()
	// StartHint is the command that starts the service.
	StartHint = pick("sudo systemctl start tuntopd", "sudo launchctl kickstart -k system/org.tuntop.tuntopd")
)

// linuxAdminGroups are tried in order; the first that exists wins.
var linuxAdminGroups = []string{"wheel", "sudo"}

func adminGroup() string {
	if runtime.GOOS == "darwin" {
		return "admin"
	}
	return firstGroup(linuxAdminGroups, func(g string) bool {
		_, err := user.LookupGroup(g)
		return err == nil
	})
}

// firstGroup returns the first existing group, or the first candidate
// (the service then reports the missing group) if none exists.
func firstGroup(candidates []string, exists func(string) bool) string {
	for _, g := range candidates {
		if exists(g) {
			return g
		}
	}
	return candidates[0]
}

func pick(linux, darwin string) string {
	if runtime.GOOS == "darwin" {
		return darwin
	}
	return linux
}

// Where the service keeps its log (rotated, secrets masked).
const (
	LogDir  = "/var/log/tuntop"
	LogFile = LogDir + "/tuntopd.log"
)
