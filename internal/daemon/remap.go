// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"regexp"
	"strings"

	"github.com/anton2901/tuntop/internal/sub"
)

// Server keys are hashes of connection settings, so they survive renames
// and reordering. When a provider changes a server's settings, its key
// changes; remapLocked carries the user's choices (selected server,
// favorites, the running connection, latency history) over to the entry
// with the same name, if that is unambiguous.
func (d *Daemon) remapLocked(subID string, old, cur *sub.Subscription) {
	if old == nil || cur == nil {
		return
	}
	moved := remapKeys(old, cur)
	if len(moved) == 0 {
		return
	}
	full := func(k string) (string, bool) {
		id, entry, ok := strings.Cut(k, ":")
		if !ok || id != subID {
			return "", false
		}
		n, ok := moved[entry]
		return subID + ":" + n, ok
	}
	if n, ok := full(d.st.Selected); ok {
		d.st.Selected = n
	}
	for i, k := range d.st.Favorites {
		if n, ok := full(k); ok {
			d.st.Favorites[i] = n
		}
	}
	if d.conn != nil {
		if n, ok := full(d.conn.key); ok {
			d.conn.key = n
		}
	}
	for k, h := range d.latency {
		if n, ok := full(k); ok {
			delete(d.latency, k)
			d.latency[n] = h
		}
	}
}

// remapKeys maps entry keys gone from old to the new key of the only entry
// in cur with the same name, when that entry is new too.
func remapKeys(old, cur *sub.Subscription) map[string]string {
	oldKeys := map[string]bool{}
	for _, e := range old.Entries {
		oldKeys[e.Key] = true
	}
	curKeys := map[string]bool{}
	byName := map[string][]string{}
	for _, e := range cur.Entries {
		curKeys[e.Key] = true
		if !oldKeys[e.Key] {
			byName[baseName(e.Name)] = append(byName[baseName(e.Name)], e.Key)
		}
	}
	oldNames := map[string]int{}
	for _, e := range old.Entries {
		oldNames[baseName(e.Name)]++
	}
	moved := map[string]string{}
	for _, e := range old.Entries {
		n := baseName(e.Name)
		if curKeys[e.Key] || oldNames[n] != 1 || len(byName[n]) != 1 {
			continue
		}
		moved[e.Key] = byName[n][0]
	}
	return moved
}

// loadSuffix is the server load some providers put in names: "· 🟢15%".
var loadSuffix = regexp.MustCompile(`\s*·\s*\S*?\d+(?:[.,]\d+)?%\s*$`)

// baseName is a server name without the changing load figure.
func baseName(name string) string {
	return strings.TrimSpace(loadSuffix.ReplaceAllString(strings.TrimSpace(name), ""))
}
