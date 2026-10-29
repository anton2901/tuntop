// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"strings"
	"sync"
	"time"
)

// LogRing keeps the last lines written to it, for the TUI's log view. Use
// it as an extra log output: log.SetOutput(io.MultiWriter(os.Stderr, ring)).
type LogRing struct {
	mu    sync.Mutex
	lines []string
	max   int
}

// NewLogRing keeps up to n lines.
func NewLogRing(n int) *LogRing { return &LogRing{max: n} }

// Write implements io.Writer; each line gets a local time prefix.
func (r *LogRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ts := time.Now().Format("15:04:05")
	for _, l := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		r.lines = append(r.lines, ts+" "+l)
	}
	if over := len(r.lines) - r.max; over > 0 {
		r.lines = append([]string(nil), r.lines[over:]...)
	}
	return len(p), nil
}

// Lines returns a copy of the kept lines, oldest first.
func (r *LogRing) Lines() []string {
	if r == nil {
		return []string{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.lines...)
}
