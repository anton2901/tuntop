// SPDX-License-Identifier: GPL-3.0-or-later

//go:build unix

package daemon

import (
	"strings"
	"testing"
)

func TestLockRefusesSecondInstance(t *testing.T) {
	dir := t.TempDir()
	unlock, err := Lock(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Lock(dir); err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second Lock: %v", err)
	}
	unlock()
	again, err := Lock(dir)
	if err != nil {
		t.Fatalf("Lock after release: %v", err)
	}
	again()
}
