// SPDX-License-Identifier: GPL-3.0-or-later

package i18n

import "fmt"

// Size formats a byte count with binary units.
func (t T) Size(b int64) string {
	units := []string{t.S("B"), t.S("KB"), t.S("MB"), t.S("GB"), t.S("TB")}
	v, u := float64(b), 0
	for v >= 1024 && u < len(units)-1 {
		v /= 1024
		u++
	}
	if u == 0 || v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[u])
	}
	return fmt.Sprintf("%.1f %s", v, units[u])
}

// Rate formats bytes per second.
func (t T) Rate(b int64) string { return t.Size(b) + t.S("/s") }
