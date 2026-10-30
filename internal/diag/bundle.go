// SPDX-License-Identifier: GPL-3.0-or-later

package diag

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Command is one read-only system query whose output goes into the bundle.
type Command struct {
	Name string
	Args []string
}

// SystemCommands are the routing, address and DNS views for this OS.
func SystemCommands() []Command {
	if runtime.GOOS == "darwin" {
		return []Command{
			{"netstat", []string{"-rn"}},
			{"ifconfig", []string{"-a"}},
			{"scutil", []string{"--dns"}},
			{"networksetup", []string{"-listallnetworkservices"}},
			{"pfctl", []string{"-s", "info"}},
		}
	}
	return []Command{
		{"ip", []string{"-br", "addr"}},
		{"ip", []string{"rule"}},
		{"ip", []string{"route"}},
		{"ip", []string{"-6", "route"}},
		{"resolvectl", []string{"status"}},
		{"nft", []string{"list", "tables"}},
	}
}

// Section writes "== title ==" and the redacted body.
func Section(w io.Writer, title, body string) {
	_, _ = fmt.Fprintf(w, "\n== %s ==\n%s\n", title, strings.TrimRight(Redact(body), "\n"))
}

// RunCommands appends the output of each command; failures (a missing tool,
// no root) are noted, not fatal.
func RunCommands(ctx context.Context, w io.Writer, cmds []Command) {
	for _, c := range cmds {
		cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var out bytes.Buffer
		cmd := exec.CommandContext(cctx, c.Name, c.Args...)
		cmd.Stdout, cmd.Stderr = &out, &out
		err := cmd.Run()
		cancel()
		if err != nil {
			_, _ = fmt.Fprintf(&out, "(%v)\n", err)
		}
		Section(w, strings.Join(append([]string{c.Name}, c.Args...), " "), out.String())
	}
}

// maxRotated bounds the numbered predecessors Tail looks for.
const maxRotated = 9

// Tail returns the last n lines of the log at path and its rotated
// predecessors path.1, path.2, …, oldest first.
func Tail(path string, n int) []string {
	var lines []string
	files := []string{path}
	for i := 1; i <= maxRotated; i++ {
		files = append([]string{fmt.Sprintf("%s.%d", path, i)}, files...)
	}
	for _, p := range files {
		if b, err := os.ReadFile(p); err == nil {
			lines = append(lines, strings.Split(strings.TrimRight(string(b), "\n"), "\n")...)
		}
	}
	if len(lines) == 1 && lines[0] == "" {
		return nil
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}
