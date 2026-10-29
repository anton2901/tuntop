// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/api"
)

// Notifier shows one desktop notification. The default uses notify-send
// (libnotify) or KDE's kdialog on Linux and osascript (Notification Center)
// on macOS; tests replace it.
type Notifier func(title, body string, urgent bool) error

// Notify is the notifier used by "tuntop notify"; nil means the default.
var Notify Notifier

// reconnectEvery is the pause before re-subscribing when tuntopd restarts.
const reconnectEvery = 5 * time.Second

// notify runs until ctx ends: it follows tuntopd's events and turns them into
// desktop notifications. The service runs as root outside the user's
// graphical session, so this small user-side process does the showing
// (systemd user unit tuntop-notify.service).
func (c *CLI) notify(ctx context.Context) error {
	show := Notify
	if show == nil {
		show = desktopNotify
	}
	for {
		err := c.C.Events(ctx, func(e api.Event) {
			title, body, urgent := c.describe(e)
			if title == "" {
				return
			}
			if err := show(title, body, urgent); err != nil {
				c.println(title + ": " + body)
			}
		})
		if ctx.Err() != nil {
			return nil
		}
		_ = err // tuntopd restarting or not started yet: try again
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(reconnectEvery):
		}
	}
}

// describe turns an event into notification text.
func (c *CLI) describe(e api.Event) (title, body string, urgent bool) {
	t := c.T
	switch e.Kind {
	case api.EventConnected:
		body = e.Server
		if e.Sub != "" {
			body += " · " + e.Sub
		}
		return t.S("VPN on"), body, false
	case api.EventDisconnected:
		return t.S("VPN off"), t.S("Traffic goes directly"), false
	case api.EventConnectFailed:
		return t.S("VPN did not connect"), e.Server + ": " + e.Error, true
	case api.EventUpdateFailed:
		return t.S("Subscription not updated"), e.Sub + ": " + e.Error, false
	case api.EventExpiring:
		return t.S("Subscription expires soon"), t.F("%s: %d days left", e.Sub, e.Days), true
	}
	return "", "", false
}

func desktopNotify(title, body string, urgent bool) error {
	if runtime.GOOS == "darwin" {
		script := fmt.Sprintf("display notification %s with title %s", appleString(body), appleString("tuntop: "+title))
		return exec.Command("/usr/bin/osascript", "-e", script).Run()
	}
	if p, err := exec.LookPath("notify-send"); err == nil {
		args := []string{"--app-name=tuntop", "--icon=network-vpn", "--expire-time=5000"}
		if urgent {
			args = append(args, "--urgency=critical")
		}
		return exec.Command(p, append(args, title, body)...).Run()
	}
	if p, err := exec.LookPath("kdialog"); err == nil {
		return exec.Command(p, "--title", title, "--passivepopup", body, "5").Run()
	}
	return exec.ErrNotFound
}

// appleString quotes s as an AppleScript string literal.
func appleString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}
