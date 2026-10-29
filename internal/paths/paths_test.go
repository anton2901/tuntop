// SPDX-License-Identifier: GPL-3.0-or-later

package paths

import "testing"

func TestFirstGroup(t *testing.T) {
	has := func(names ...string) func(string) bool {
		return func(g string) bool {
			for _, n := range names {
				if n == g {
					return true
				}
			}
			return false
		}
	}
	for _, tc := range []struct {
		name   string
		exists func(string) bool
		want   string
	}{
		{"arch", has("wheel", "sudo"), "wheel"},
		{"ubuntu", has("sudo"), "sudo"},
		{"neither", has(), "wheel"},
	} {
		if got := firstGroup(linuxAdminGroups, tc.exists); got != tc.want {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}
}
