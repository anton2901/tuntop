// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"slices"
	"sort"
	"strings"

	"github.com/anton2901/tuntop/internal/api"
)

// Sort orders of the servers inside each group.
const (
	sortOrder   = ""        // as the subscription lists them
	sortLatency = "latency" // fastest first, failed last
	sortName    = "name"
)

const favGroup = "\x01"

// groupKey is the list group a server belongs to: favorites or its subscription.
func groupKey(s api.Server) string {
	if s.Favorite {
		return favGroup
	}
	return s.SubID
}

func (m Model) isFolded(group string) bool {
	return slices.Contains(m.cfg.Folded, group)
}

// foldedAtCursor reports whether the cursor stands on a collapsed group's row.
func (m Model) foldedAtCursor() bool {
	vis := m.visibleServers()
	return m.cursor < len(vis) && m.folding() && m.isFolded(groupKey(vis[m.cursor]))
}

// folding is false while a filter is typed: matches are never hidden.
func (m Model) folding() bool {
	return strings.TrimSpace(m.filter.Value()) == ""
}

// arrange sorts the servers inside their groups and keeps the groups in
// the order they come; a collapsed group is cut down to its first server,
// which stands for the whole group.
func (m Model) arrange(in []api.Server) []api.Server {
	if m.cfg.Sort == sortOrder && len(m.cfg.Folded) == 0 {
		return in
	}
	var order []string
	groups := map[string][]api.Server{}
	for _, s := range in {
		g := groupKey(s)
		if _, ok := groups[g]; !ok {
			order = append(order, g)
		}
		groups[g] = append(groups[g], s)
	}
	out := make([]api.Server, 0, len(in))
	for _, g := range order {
		list := groups[g]
		switch m.cfg.Sort {
		case sortLatency:
			sort.SliceStable(list, func(i, j int) bool { return latencyRank(list[i].Latency) < latencyRank(list[j].Latency) })
		case sortName:
			sort.SliceStable(list, func(i, j int) bool {
				return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
			})
		}
		if m.folding() && m.isFolded(g) {
			list = list[:1]
		}
		out = append(out, list...)
	}
	return out
}

// latencyRank puts measured servers by speed, then unmeasured, then failed.
func latencyRank(ms int) int {
	switch {
	case ms > 0:
		return ms
	case ms == 0:
		return 1 << 30
	}
	return 1<<30 + 1
}

// groupSize counts the servers of a group, hidden ones included.
func (m Model) groupSize(group string) int {
	n := 0
	for _, s := range m.servers {
		if groupKey(s) == group {
			n++
		}
	}
	return n
}

// cursorTo puts the cursor on the first visible server of group, or on the
// server with key when it is still visible.
func (m *Model) cursorTo(key, group string) {
	vis := m.visibleServers()
	for i, s := range vis {
		if s.Key == key {
			m.cursor = i
			return
		}
	}
	for i, s := range vis {
		if groupKey(s) == group {
			m.cursor = i
			return
		}
	}
}

// toggleFold collapses or expands the group under the cursor (all=false)
// or every group (all=true: collapse all unless all are already collapsed).
func (m *Model) toggleFold(all bool, want *bool) {
	vis := m.visibleServers()
	if m.cursor >= len(vis) {
		return
	}
	cur := vis[m.cursor]
	g := groupKey(cur)
	if all {
		var groups []string
		for _, s := range m.servers {
			if !slices.Contains(groups, groupKey(s)) {
				groups = append(groups, groupKey(s))
			}
		}
		if len(m.cfg.Folded) >= len(groups) {
			m.cfg.Folded = nil
		} else {
			m.cfg.Folded = groups
		}
	} else {
		folded := m.isFolded(g)
		if want != nil && *want == folded {
			return
		}
		if folded {
			m.cfg.Folded = slices.DeleteFunc(slices.Clone(m.cfg.Folded), func(x string) bool { return x == g })
		} else {
			m.cfg.Folded = append(slices.Clone(m.cfg.Folded), g)
		}
	}
	m.cursorTo(cur.Key, g)
	m.saveConfig()
}

// cycleSort switches the order inside groups, keeping the cursor on its server.
func (m *Model) cycleSort() {
	vis := m.visibleServers()
	key, g := "", ""
	if m.cursor < len(vis) {
		key, g = vis[m.cursor].Key, groupKey(vis[m.cursor])
	}
	switch m.cfg.Sort {
	case sortOrder:
		m.cfg.Sort = sortLatency
		m.setMsg(m.t.S("sort: by latency"), false)
	case sortLatency:
		m.cfg.Sort = sortName
		m.setMsg(m.t.S("sort: by name"), false)
	default:
		m.cfg.Sort = sortOrder
		m.setMsg(m.t.S("sort: subscription order"), false)
	}
	m.cursorTo(key, g)
	m.saveConfig()
}
