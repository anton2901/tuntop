// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// Lock takes the exclusive instance lock in dir. A second service would
// undo the first one's routes and kill switch on start and steal its
// socket, so it must be refused before it touches anything. The lock goes
// away with the process, however it ends; call the returned func to release.
func Lock(dir string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, "tuntopd.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if err == syscall.EWOULDBLOCK {
			return nil, fmt.Errorf("another tuntopd is already running (lock %s)", f.Name())
		}
		return nil, err
	}
	return func() { _ = f.Close() }, nil
}
