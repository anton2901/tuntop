// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !linux && !darwin

package netcfg

import (
	"fmt"
	"runtime"
)

// Setup is not implemented on this OS yet.
func Setup(Config) (func() error, error) {
	return nil, fmt.Errorf("TUN routing is not supported on %s yet", runtime.GOOS)
}

// RemoveLink is a no-op on this OS.
func RemoveLink(string) error { return nil }

// Bypass is not implemented on this OS yet.
func Bypass(Config) (map[string]any, error) {
	return nil, fmt.Errorf("TUN routing is not supported on %s yet", runtime.GOOS)
}

// KillSwitchOn is not implemented on this OS yet.
func KillSwitchOn(Config) error {
	return fmt.Errorf("kill switch is not supported on %s yet", runtime.GOOS)
}

// KillSwitchOff is a no-op on this OS.
func KillSwitchOff(Config) error { return nil }

// Recover is a no-op on this OS.
func Recover(Config) error { return nil }

// Changed is never true on this OS.
func Changed(map[string]any) bool { return false }
