// SPDX-License-Identifier: GPL-3.0-or-later

// Package cli implements tuntop's command-line interface. Like the TUI it is
// a client of tuntopd and never needs root.
package cli

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/i18n"
	"github.com/anton2901/tuntop/internal/paths"
	"github.com/anton2901/tuntop/internal/version"
)

// Client is the part of api.Client the CLI uses.
type Client interface {
	Status(ctx context.Context) (api.Status, error)
	Servers(ctx context.Context) ([]api.Server, error)
	Connect(ctx context.Context, server string) (api.Status, error)
	Disconnect(ctx context.Context) (api.Status, error)
	Subscriptions(ctx context.Context) ([]api.Subscription, error)
	AddSubscription(ctx context.Context, req api.AddSubscriptionRequest) (api.Subscription, error)
	RemoveSubscription(ctx context.Context, id string) error
	Update(ctx context.Context, id string) ([]api.Subscription, error)
	AddLinks(ctx context.Context, links []string) ([]api.Server, error)
	RemoveServer(ctx context.Context, key string) error
	Test(ctx context.Context, keys []string) ([]api.Server, error)
	Events(ctx context.Context, fn func(api.Event)) error
	Traffic(ctx context.Context) (api.Traffic, error)
	SetFavorite(ctx context.Context, server string, on bool) (api.Server, error)
	MoveSubscription(ctx context.Context, id string, to int) ([]api.Subscription, error)
	Settings(ctx context.Context) (api.Settings, error)
	SetSettings(ctx context.Context, s api.Settings) (api.Settings, error)
}

// CLI runs one command.
type CLI struct {
	C   Client
	T   i18n.T
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

const usage = `usage: tuntop [command]

  (no command)         interactive interface (TUI); status when not a terminal
  status               connection state
  on [server]          connect: to the given server, or the last one used
  off                  disconnect; traffic goes directly
  use <server>         same as "on <server>"
  servers              list servers of all subscriptions
  test [server]        measure latency (all servers by default)
  killswitch [on|off]  block traffic outside the tunnel while the VPN is on
  autoconnect [on|off] connect to the last server when the service starts
  fav <server>         mark a server as favorite: it is listed first
  unfav <server>       unmark a favorite
  sub                  list subscriptions
  sub add <url|->      add a subscription ("-" reads the URL from stdin)
         [-ua UA] [-no-hwid]
  sub rm <n|id>        remove a subscription
  sub mv <n|id> <pos>  move a subscription to a position in the list
  update [n|id]        refresh subscriptions now (all by default)
  add <link…|->        add individual vless:// links
  rm <server>          remove an individually added link
  notify               show desktop notifications about the VPN (runs until stopped)
  version              print version

<server> is a number from "tuntop servers" or part of the server name.
`

// Run executes args (without the program name) and returns the exit code.
func (c *CLI) Run(ctx context.Context, args []string) int {
	cmd := "status"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "status", "st":
		err = c.status(ctx)
	case "on", "connect", "use":
		if cmd == "use" && len(args) == 0 {
			err = errors.New(c.T.S(`use: which server? See "tuntop servers"`))
			break
		}
		err = c.connect(ctx, strings.Join(args, " "))
	case "off", "disconnect":
		if _, err = c.C.Disconnect(ctx); err == nil {
			c.println(c.T.S("VPN off: traffic goes directly"))
		}
	case "servers", "ls", "list":
		err = c.servers(ctx)
	case "test", "ping":
		err = c.test(ctx, strings.Join(args, " "))
	case "killswitch", "ks":
		err = c.killSwitch(ctx, args)
	case "autoconnect":
		err = c.autoConnect(ctx, args)
	case "fav", "unfav":
		err = c.favorite(ctx, strings.Join(args, " "), cmd == "fav")
	case "sub", "subs":
		err = c.sub(ctx, args)
	case "update":
		err = c.update(ctx, args)
	case "add":
		err = c.addLinks(ctx, args)
	case "rm":
		err = c.removeServer(ctx, strings.Join(args, " "))
	case "notify":
		err = c.notify(ctx)
	case "version", "--version":
		c.println("tuntop " + version.String())
	case "help", "-h", "--help":
		_, _ = fmt.Fprint(c.Out, c.T.S(usage))
	default:
		_, _ = fmt.Fprintf(c.Err, "tuntop: %s\n\n%s", c.T.F("unknown command %q", cmd), c.T.S(usage))
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(c.Err, "tuntop:", err)
		if errors.Is(err, api.ErrNoDaemon) {
			_, _ = fmt.Fprintln(c.Err, "      "+c.T.S("start it with:")+" "+paths.StartHint)
		}
		return 1
	}
	return 0
}

func (c *CLI) println(s string)               { _, _ = fmt.Fprintln(c.Out, s) }
func (c *CLI) printf(format string, a ...any) { _, _ = fmt.Fprintf(c.Out, c.T.S(format), a...) }

func (c *CLI) status(ctx context.Context) error {
	st, err := c.C.Status(ctx)
	if err != nil {
		return err
	}
	if st.Connected {
		c.printf("VPN:     ON, all traffic goes through the server\n")
		c.printf("server:  %s\n", serverLine(st.Server))
		c.printf("since:   %s (%s)\n", st.Since.Format("15:04"), time.Since(st.Since).Round(time.Second))
		if tr, err := c.C.Traffic(ctx); err == nil && tr.Connected {
			c.printf("traffic: ↓ %s (%s)  ↑ %s (%s)\n",
				c.T.Size(tr.DownTotal), c.T.Rate(tr.DownRate), c.T.Size(tr.UpTotal), c.T.Rate(tr.UpRate))
		}
	} else {
		c.printf("VPN:     off, traffic goes directly\n")
		if st.Selected != nil {
			c.printf("on:      \"tuntop on\" connects to %s\n", serverLine(st.Selected))
		} else {
			c.printf("on:      pick a server first: tuntop servers, then tuntop on <n>\n")
		}
	}
	if st.KillSwitch {
		if st.Connected {
			c.printf("kill:    on, traffic outside the tunnel is blocked\n")
		} else {
			c.printf("BLOCKED: kill switch is on and the VPN is down: tuntop on <server>, or tuntop off for direct traffic\n")
		}
	}
	if st.LastError != "" {
		c.printf("error:   %s\n", st.LastError)
	}
	return nil
}

func (c *CLI) autoConnect(ctx context.Context, args []string) error {
	s, err := c.C.Settings(ctx)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "on":
			s.AutoConnect = true
		case "off":
			s.AutoConnect = false
		default:
			return errors.New(c.T.S("autoconnect: expected on or off"))
		}
		if s, err = c.C.SetSettings(ctx, s); err != nil {
			return err
		}
	}
	if s.AutoConnect {
		c.printf("auto-connect: on; the service connects to the last server when it starts\n")
	} else {
		c.printf("auto-connect: off; the VPN starts off\n")
	}
	return nil
}

func (c *CLI) killSwitch(ctx context.Context, args []string) error {
	s, err := c.C.Settings(ctx)
	if err != nil {
		return err
	}
	if len(args) > 0 {
		switch args[0] {
		case "on":
			s.KillSwitch = true
		case "off":
			s.KillSwitch = false
		default:
			return errors.New(c.T.S("killswitch: expected on or off"))
		}
		if s, err = c.C.SetSettings(ctx, s); err != nil {
			return err
		}
	}
	if s.KillSwitch {
		c.printf("kill switch: on; while the VPN is on, traffic goes only through the tunnel\n")
	} else {
		c.printf("kill switch: off\n")
	}
	return nil
}

func serverLine(s *api.Server) string {
	if s == nil {
		return "?"
	}
	line := s.Name
	if s.SubName != "" {
		line += "  (" + s.SubName + ")"
	}
	return line
}

func (c *CLI) connect(ctx context.Context, server string) error {
	c.println(c.T.S("connecting…"))
	st, err := c.C.Connect(ctx, server)
	if err != nil {
		return err
	}
	c.printf("VPN on: %s\n", serverLine(st.Server))
	return nil
}

func (c *CLI) servers(ctx context.Context) error {
	list, err := c.C.Servers(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		c.println(c.T.S("no servers yet: tuntop sub add <subscription URL>, or tuntop add <vless://…>"))
		return nil
	}
	st, _ := c.C.Status(ctx)
	c.printServers(list, st)
	c.println("\n" + c.T.S("connect: tuntop on <number>"))
	return nil
}

func (c *CLI) printServers(list []api.Server, st api.Status) {
	cur := ""
	switch {
	case st.Server != nil:
		cur = st.Server.Key
	case st.Selected != nil:
		cur = st.Selected.Key
	}
	group := "\x00"
	for _, s := range list {
		g, title := s.SubID, s.SubName
		if s.Favorite {
			g, title = "\x01", c.T.S("★ Favorites")
		}
		if g != group {
			group = g
			c.println("\n" + title)
		}
		mark := " "
		if s.Key == cur {
			mark = "→"
			if !st.Connected {
				mark = "·"
			}
		}
		kind := s.Proto
		if s.Group {
			kind = c.T.F("group of %d", s.Proxies)
		}
		lat := ""
		switch {
		case s.Latency < 0:
			lat = "  ✗"
		case s.Latency > 0:
			lat = "  " + c.T.F("%d ms", s.Latency)
		}
		name := s.Name
		if s.Favorite && s.SubName != "" {
			name += "  (" + s.SubName + ")"
		}
		_, _ = fmt.Fprintf(c.Out, " %s %3d. %s  [%s]%s\n", mark, s.Index, name, kind, lat)
	}
}

func (c *CLI) favorite(ctx context.Context, q string, on bool) error {
	if q == "" {
		return errors.New(c.T.S(`which server? See "tuntop servers"`))
	}
	s, err := c.C.SetFavorite(ctx, q, on)
	if err != nil {
		return err
	}
	if on {
		c.printf("★ %s: in favorites, listed first\n", s.Name)
	} else {
		c.printf("%s: removed from favorites\n", s.Name)
	}
	return nil
}

func (c *CLI) test(ctx context.Context, q string) error {
	var keys []string
	if q != "" {
		list, err := c.C.Servers(ctx)
		if err != nil {
			return err
		}
		k, err := pick(c.T, list, q)
		if err != nil {
			return err
		}
		keys = []string{k}
	}
	c.println(c.T.S("measuring latency…"))
	list, err := c.C.Test(ctx, keys)
	if err != nil {
		return err
	}
	st, _ := c.C.Status(ctx)
	c.printServers(list, st)
	return nil
}

// pick finds a server key by list number or unique part of the name.
func pick(t i18n.T, list []api.Server, q string) (string, error) {
	var found []api.Server
	for _, s := range list {
		if strconv.Itoa(s.Index) == q || s.Key == q {
			return s.Key, nil
		}
		if strings.Contains(strings.ToLower(s.Name), strings.ToLower(q)) {
			found = append(found, s)
		}
	}
	switch len(found) {
	case 0:
		return "", errors.New(t.F("no server matches %q", q))
	case 1:
		return found[0].Key, nil
	}
	return "", errors.New(t.F("%q matches %d servers, be more specific or use the number", q, len(found)))
}

func (c *CLI) sub(ctx context.Context, args []string) error {
	op := "ls"
	if len(args) > 0 {
		op, args = args[0], args[1:]
	}
	switch op {
	case "ls", "list":
		return c.listSubs(ctx)
	case "add":
		fs := flag.NewFlagSet("sub add", flag.ContinueOnError)
		fs.SetOutput(c.Err)
		ua := fs.String("ua", "", c.T.S("User-Agent for this subscription (default: Happ-compatible)"))
		noHWID := fs.Bool("no-hwid", false, c.T.S("do not send the device id"))
		if err := fs.Parse(args); err != nil {
			return err
		}
		if fs.NArg() != 1 {
			return errors.New(c.T.S(`sub add: expected one URL (or "-" to read it from stdin)`))
		}
		u := fs.Arg(0)
		if u == "-" {
			line, _ := bufio.NewReader(c.In).ReadString('\n')
			u = strings.TrimSpace(line)
		}
		c.println(c.T.S("downloading…"))
		s, err := c.C.AddSubscription(ctx, api.AddSubscriptionRequest{URL: u, UserAgent: *ua, NoHWID: *noHWID})
		if err != nil {
			return fmt.Errorf("%s: %w", c.T.S("subscription not added"), err)
		}
		c.printf("added %s: %d servers. See them with: tuntop servers\n", s.Name, s.Servers)
		return nil
	case "rm", "remove":
		if len(args) != 1 {
			return errors.New(c.T.S(`sub rm: which one? Give its number from "tuntop sub"`))
		}
		id, err := c.subID(ctx, args[0])
		if err != nil {
			return err
		}
		return c.C.RemoveSubscription(ctx, id)
	case "mv", "move":
		if len(args) != 2 {
			return errors.New(c.T.S(`sub mv: expected a subscription and a position, e.g. "tuntop sub mv 2 1"`))
		}
		id, err := c.subID(ctx, args[0])
		if err != nil {
			return err
		}
		to, err := strconv.Atoi(args[1])
		if err != nil || to < 1 {
			return errors.New(c.T.F("sub mv: bad position %q", args[1]))
		}
		if _, err := c.C.MoveSubscription(ctx, id, to); err != nil {
			return err
		}
		return c.listSubs(ctx)
	default:
		return errors.New(c.T.F("sub: unknown operation %q (ls, add, rm, mv)", op))
	}
}

func (c *CLI) listSubs(ctx context.Context) error {
	subs, err := c.C.Subscriptions(ctx)
	if err != nil {
		return err
	}
	if len(subs) == 0 {
		c.println(c.T.S("no subscriptions: tuntop sub add <URL>"))
		return nil
	}
	for i, s := range subs {
		_, _ = fmt.Fprintf(c.Out, "%2d. %s — %s\n", i+1, s.Name, c.T.F("%d servers", s.Servers))
		if s.URL != "" {
			c.println("    " + s.URL)
		}
		var facts []string
		switch {
		case s.Total > 0:
			facts = append(facts, c.T.F("traffic %s of %s", gb(s.Download+s.Upload), gb(s.Total)))
		case s.Download+s.Upload > 0:
			facts = append(facts, c.T.F("traffic %s (unlimited)", gb(s.Download+s.Upload)))
		}
		if !s.Expire.IsZero() {
			days := int(time.Until(s.Expire).Hours() / 24)
			facts = append(facts, c.T.F("expires %s (%d days)", s.Expire.Format("2006-01-02"), days))
		}
		if !s.UpdatedAt.IsZero() {
			facts = append(facts, c.T.F("updated %s", s.UpdatedAt.Format("01-02 15:04")))
		}
		if len(facts) > 0 {
			c.println("    " + strings.Join(facts, " · "))
		}
		if s.Error != "" {
			c.println("    " + c.T.F("last update failed: %s", s.Error))
		}
		if s.Announce != "" {
			c.println("    " + c.T.F("provider: %s", strings.ReplaceAll(s.Announce, "\n", " ")))
		}
	}
	return nil
}

func gb(b int64) string { return fmt.Sprintf("%.1f GB", float64(b)/(1<<30)) }

// subID accepts a list number or an id.
func (c *CLI) subID(ctx context.Context, s string) (string, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return s, nil
	}
	subs, err := c.C.Subscriptions(ctx)
	if err != nil {
		return "", err
	}
	if n < 1 || n > len(subs) {
		return "", errors.New(c.T.F("subscription number %d out of range 1..%d", n, len(subs)))
	}
	return subs[n-1].ID, nil
}

func (c *CLI) update(ctx context.Context, args []string) error {
	id := ""
	if len(args) > 0 {
		var err error
		if id, err = c.subID(ctx, args[0]); err != nil {
			return err
		}
	}
	c.println(c.T.S("updating…"))
	if _, err := c.C.Update(ctx, id); err != nil {
		return err
	}
	return c.listSubs(ctx)
}

func (c *CLI) addLinks(ctx context.Context, args []string) error {
	if len(args) == 1 && args[0] == "-" {
		args = nil
		sc := bufio.NewScanner(c.In)
		for sc.Scan() {
			args = append(args, sc.Text())
		}
	}
	if len(args) == 0 {
		return errors.New(c.T.S(`add: give one or more vless:// links (or "-" for stdin)`))
	}
	if _, err := c.C.AddLinks(ctx, args); err != nil {
		return err
	}
	c.println(c.T.S("added. See them with: tuntop servers"))
	return nil
}

func (c *CLI) removeServer(ctx context.Context, q string) error {
	list, err := c.C.Servers(ctx)
	if err != nil {
		return err
	}
	for _, s := range list {
		if strconv.Itoa(s.Index) == q || s.Key == q {
			return c.C.RemoveServer(ctx, s.Key)
		}
	}
	return errors.New(c.T.F(`rm: no server %q (use its number from "tuntop servers")`, q))
}
