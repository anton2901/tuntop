// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/diag"
	"github.com/anton2901/tuntop/internal/paths"
	"github.com/anton2901/tuntop/internal/version"
)

func (c *CLI) logPath() string { return cmp.Or(c.LogPath, paths.LogFile) }

// logs prints the tail of the service log file (secrets are masked when it
// is written) and, with -f, follows it. Without the file it falls back to
// the lines the service keeps in memory.
func (c *CLI) logs(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	n := fs.Int("n", 200, "")
	follow := fs.Bool("f", false, "")
	if err := fs.Parse(args); err != nil || fs.NArg() > 0 || *n < 1 {
		return errors.New(c.T.S("logs: expected -n N (lines) or -f (follow)"))
	}
	path := c.logPath()
	st, err := os.Stat(path)
	if err != nil {
		lines, lerr := c.C.Log(ctx)
		if lerr != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		c.println(c.T.F("(no log file %s; showing what the service keeps in memory)", path))
		for _, l := range lines[max(len(lines)-*n, 0):] {
			c.println(l)
		}
		return nil
	}
	for _, l := range diag.Tail(path, *n) {
		c.println(l)
	}
	if !*follow {
		return nil
	}
	off := st.Size()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
		cur, err := os.Stat(path)
		if err != nil {
			continue
		}
		if cur.Size() < off { // rotated
			off = 0
		}
		if cur.Size() == off {
			continue
		}
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		_, _ = f.Seek(off, io.SeekStart)
		b, _ := io.ReadAll(f)
		_ = f.Close()
		// Only whole lines; the rest is picked up next time.
		if i := bytes.LastIndexByte(b, '\n'); i >= 0 {
			_, _ = c.Out.Write(b[:i+1])
			off += int64(i + 1)
		}
	}
}

// debugBundle writes one file with everything needed to look into a
// problem: versions, settings, the log and the routing and DNS state. All
// of it is masked the same way as the log. It works without the service.
func (c *CLI) debugBundle(ctx context.Context, args []string) error {
	if len(args) > 1 {
		return errors.New(c.T.S("debug-bundle: expected at most one file name"))
	}
	name := "tuntop-debug-" + time.Now().Format("20060102-150405") + ".txt"
	if len(args) == 1 {
		name = args[0]
	}
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "tuntop debug bundle, %s\n", time.Now().Format(time.RFC3339))
	diag.Section(&buf, "client", fmt.Sprintf("tuntop %s\n%s/%s, %s", version.String(), runtime.GOOS, runtime.GOARCH, runtime.Version()))

	if st, err := c.C.Status(ctx); err != nil {
		diag.Section(&buf, "service", "unreachable: "+err.Error())
	} else {
		srv := "-"
		if st.Server != nil {
			srv = st.Server.Name
		}
		diag.Section(&buf, "service", fmt.Sprintf("build: %s\nxray: %s\nconnected: %t\nserver: %s\nsince: %s\nmode: %s\nkill switch: %t\nlast error: %s",
			st.Build, st.Xray, st.Connected, srv, st.Since.Format(time.RFC3339), cmp.Or(st.Mode, "tun"), st.KillSwitch, st.LastError))
	}
	if s, err := c.C.Settings(ctx); err == nil {
		diag.Section(&buf, "settings", fmt.Sprintf("%+v", s))
	}
	if subs, err := c.C.Subscriptions(ctx); err == nil {
		var b strings.Builder
		for _, s := range subs {
			fmt.Fprintf(&b, "%s: %d servers, updated %s, error %q\n", s.ID, s.Servers, s.UpdatedAt.Format(time.RFC3339), s.Error)
		}
		diag.Section(&buf, "subscriptions", b.String())
	}
	if list, err := c.C.Servers(ctx); err == nil {
		var ok, failed, unknown int
		for _, s := range list {
			switch {
			case s.Latency > 0:
				ok++
			case s.Latency < 0:
				failed++
			default:
				unknown++
			}
		}
		diag.Section(&buf, "servers", fmt.Sprintf("total %d: answer %d, no answer %d, not measured %d", len(list), ok, failed, unknown))
	}

	lines := diag.Tail(c.logPath(), 2000)
	if len(lines) == 0 {
		if l, err := c.C.Log(ctx); err == nil {
			lines = l
		}
	}
	diag.Section(&buf, "log (last 2000 lines)", strings.Join(lines, "\n"))
	diag.RunCommands(ctx, &buf, diag.SystemCommands())

	if name == "-" {
		_, err := c.Out.Write(buf.Bytes())
		return err
	}
	if err := os.WriteFile(name, buf.Bytes(), 0o600); err != nil {
		return err
	}
	c.println(c.T.F("Saved %s (%d KB). Secrets and public addresses are masked; check it before sharing.", name, buf.Len()/1024))
	return nil
}
