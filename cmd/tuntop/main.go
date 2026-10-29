// SPDX-License-Identifier: GPL-3.0-or-later

// Command tuntop is the user-facing client: a TUI when run without
// arguments in a terminal, a scriptable CLI otherwise. It talks to tuntopd
// over a unix socket and never needs root.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/cli"
	"github.com/anton2901/tuntop/internal/i18n"
	"github.com/anton2901/tuntop/internal/tui"

	"golang.org/x/term"
)

func main() {
	c := api.NewClient("")
	lang := i18n.Detect()
	args := os.Args[1:]
	if len(args) == 0 && term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
		if err := tui.Run(c, lang); err != nil {
			fmt.Fprintln(os.Stderr, "tuntop:", err)
			os.Exit(1)
		}
		return
	}
	os.Exit(runCLI(c, lang, args))
}

func runCLI(c *api.Client, lang i18n.Lang, args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cl := &cli.CLI{C: c, T: i18n.T{Lang: lang}, In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
	return cl.Run(ctx, args)
}
