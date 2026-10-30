// SPDX-License-Identifier: GPL-3.0-or-later

//go:build !unix

package daemon

// Lock is a no-op where flock is unavailable.
func Lock(string) (func(), error) { return func() {}, nil }
