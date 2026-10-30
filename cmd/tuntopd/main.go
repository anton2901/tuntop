// SPDX-License-Identifier: GPL-3.0-or-later

// Command tuntopd is the privileged daemon: it owns the TUN device and
// the proxy core and serves the tuntop client over a unix socket.
//
// "run" is the service; sub, socks and tun are developer commands.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/anton2901/tuntop/internal/daemon"
	"github.com/anton2901/tuntop/internal/device"
	"github.com/anton2901/tuntop/internal/link"
	"github.com/anton2901/tuntop/internal/netcfg"
	"github.com/anton2901/tuntop/internal/sub"
	"github.com/anton2901/tuntop/internal/version"
	"github.com/anton2901/tuntop/internal/xray"
)

const usage = `usage: tuntopd <command> [flags]

commands:
  run [flags]               run the service (root); control it with "tuntop"
  version                   print version

developer commands:
  sub [flags] <url>         download a subscription and list its servers
  socks [flags] <link|url>  run a local SOCKS5 proxy through a share link,
                            or through server N of a subscription (-server N)
  tun [flags] <link|url>    send all system traffic through the server
                            (root, Linux; -timeout for a safe trial run)

<link|url> may be "-" to read it from stdin (keeps it out of shell history).
Run "tuntopd <command> -h" for flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "version", "--version":
		fmt.Println("tuntopd", version.String())
	case "sub":
		err = runSub(os.Args[2:])
	case "socks":
		err = runSOCKS(os.Args[2:])
	case "tun":
		err = runTUN(os.Args[2:])
	case "run":
		err = runDaemon(os.Args[2:])
	case daemon.ChildCommand: // internal: the tunnel's Xray, started by "run"
		err = daemon.RunChildMain()
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "tuntopd: unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tuntopd:", err)
		os.Exit(1)
	}
}

// subFlags are shared by commands that download subscriptions.
type subFlags struct {
	ua     *string
	noHWID *bool
	proxy  *string
}

func addSubFlags(fs *flag.FlagSet) subFlags {
	return subFlags{
		ua:     fs.String("ua", sub.DefaultUserAgent, "User-Agent sent to the subscription server"),
		noHWID: fs.Bool("no-hwid", false, "do not send the device id headers (x-hwid, …)"),
		proxy:  fs.String("proxy", "", "download the subscription through this proxy, e.g. socks5h://127.0.0.1:1080"),
	}
}

func (f subFlags) fetch(url string) (*sub.Subscription, error) {
	opt := sub.Options{UserAgent: *f.ua, Proxy: *f.proxy}
	if !*f.noHWID {
		dev, err := device.Current()
		if err != nil {
			return nil, fmt.Errorf("device id: %w (use -no-hwid to skip)", err)
		}
		opt.Headers = dev.Headers()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return sub.Fetch(ctx, url, opt)
}

func runSub(args []string) error {
	fs := flag.NewFlagSet("sub", flag.ExitOnError)
	sf := addSubFlags(fs)
	save := fs.String("save", "", "save the downloaded subscription to this file (mode 0600) for \"socks -file\"")
	_ = fs.Parse(args)
	url, err := oneArg(fs)
	if err != nil {
		return err
	}
	if !isURL(url) {
		return errors.New("sub: expected an http(s) subscription URL")
	}
	s, err := sf.fetch(url)
	if s == nil {
		return err
	}
	printSubscription(s)
	if err != nil {
		return err
	}
	if *save != "" {
		// The body holds server credentials: owner-only.
		if err := os.WriteFile(*save, s.Body, 0o600); err != nil {
			return fmt.Errorf("save subscription: %w", err)
		}
	}
	return nil
}

func printSubscription(s *sub.Subscription) {
	i := s.Info
	if i.Title != "" {
		fmt.Println(i.Title)
	}
	if i.Download+i.Upload > 0 || i.Total > 0 {
		total := "unlimited"
		if i.Total > 0 {
			total = gb(i.Total)
		}
		fmt.Printf("traffic: %s of %s\n", gb(i.Download+i.Upload), total)
	}
	if !i.Expire.IsZero() {
		fmt.Printf("expires: %s\n", i.Expire.Format("2006-01-02"))
	}
	fmt.Println()
	for n, e := range s.Entries {
		kind := e.Proto
		if e.Kind == sub.KindXrayGroup {
			kind = fmt.Sprintf("group, %d servers", e.Proxies)
		}
		fmt.Printf("%3d. %s  [%s]\n", n+1, strings.TrimSpace(e.Name), kind)
	}
	if len(s.Skipped) > 0 {
		fmt.Printf("\nskipped %d:\n", len(s.Skipped))
		for _, k := range s.Skipped {
			fmt.Printf("     %s — %s\n", strings.TrimSpace(k.Name), k.Reason)
		}
	}
}

func gb(b int64) string {
	return fmt.Sprintf("%.1f GB", float64(b)/(1<<30))
}

// serverFlags select one server: a share link, or entry N of a
// subscription (downloaded, or saved earlier with "tuntopd sub -save").
type serverFlags struct {
	server *int
	file   *string
	sub    subFlags
}

func addServerFlags(fs *flag.FlagSet) serverFlags {
	return serverFlags{
		server: fs.Int("server", 0, "server number from \"tuntopd sub\" (subscriptions only)"),
		file:   fs.String("file", "", "use a subscription saved by \"tuntopd sub -save\" instead of a link"),
		sub:    addSubFlags(fs),
	}
}

// resolve returns the server's display name and profile (an Xray config
// without inbounds).
func (f serverFlags) resolve(fs *flag.FlagSet) (name string, profile []byte, err error) {
	var input string
	if *f.file == "" {
		if input, err = oneArg(fs); err != nil {
			return "", nil, err
		}
	} else if fs.NArg() > 0 {
		return "", nil, fmt.Errorf("%s: -file and a link are mutually exclusive", fs.Name())
	}

	if *f.file == "" && !isURL(input) {
		p, err := link.Parse(input)
		if err != nil {
			return "", nil, err
		}
		profile, err = xray.LinkProfile(p.Outbound)
		return p.Name, profile, err
	}

	if *f.server < 1 {
		return "", nil, fmt.Errorf(`%s: a subscription needs -server N (see "tuntopd sub")`, fs.Name())
	}
	var s *sub.Subscription
	if *f.file != "" {
		body, err := os.ReadFile(*f.file)
		if err != nil {
			return "", nil, fmt.Errorf("read subscription: %w", err)
		}
		if s, err = sub.Parse(body, nil); err != nil {
			return "", nil, err
		}
	} else if s, err = f.sub.fetch(input); err != nil {
		// Providers may reorder servers per request, so -server N can
		// differ from what an earlier "tuntopd sub" showed; -file avoids it.
		return "", nil, err
	}
	if *f.server > len(s.Entries) {
		return "", nil, fmt.Errorf("%s: -server %d out of range 1..%d", fs.Name(), *f.server, len(s.Entries))
	}
	e := s.Entries[*f.server-1]
	profile, err = e.Profile()
	return strings.TrimSpace(e.Name), profile, err
}

func runSOCKS(args []string) error {
	fs := flag.NewFlagSet("socks", flag.ExitOnError)
	listen := fs.String("listen", "127.0.0.1:10808", "SOCKS5 listen address")
	logLevel := fs.String("loglevel", "warning", "xray log level: debug, info, warning, error, none")
	srv := addServerFlags(fs)
	_ = fs.Parse(args)

	host, portStr, err := net.SplitHostPort(*listen)
	if err != nil {
		return fmt.Errorf("invalid -listen: %w", err)
	}
	port, err := strconv.ParseUint(portStr, 10, 16)
	if err != nil {
		return fmt.Errorf("invalid -listen port: %w", err)
	}
	name, profile, err := srv.resolve(fs)
	if err != nil {
		return err
	}
	cfg, err := xray.WithSOCKS(profile, host, uint16(port), *logLevel)
	if err != nil {
		return err
	}

	inst, err := startXray(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = inst.Close() }()

	fmt.Printf("server: %s\nSOCKS5: %s (Ctrl+C to stop)\n", name, *listen)
	fmt.Printf("check:  curl -x socks5h://%s https://api.ipify.org\n", *listen)
	waitStop(0)
	return nil
}

func runTUN(args []string) error {
	fs := flag.NewFlagSet("tun", flag.ExitOnError)
	logLevel := fs.String("loglevel", "warning", "xray log level: debug, info, warning, error, none")
	timeout := fs.Duration("timeout", 0, "stop automatically after this long (safety net for tests), 0 = never")
	srv := addServerFlags(fs)
	_ = fs.Parse(args)

	if os.Geteuid() != 0 {
		return errors.New("tun: must run as root (creates a network interface and routes)")
	}
	name, profile, err := srv.resolve(fs)
	if err != nil {
		return err
	}
	nc := netcfg.Default
	nc.StateDir = os.TempDir()
	bypass, err := netcfg.Bypass(nc)
	if err != nil {
		return err
	}
	cfg, err := xray.WithTUN(profile, xray.TUNOptions{
		Name: nc.Iface, MTU: 1500, Bypass: bypass, LogLevel: *logLevel,
	})
	if err != nil {
		return err
	}

	inst, err := startXray(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = inst.Close() }()
	undo, err := netcfg.Setup(nc)
	if err != nil {
		return err
	}
	defer func() {
		if err := undo(); err != nil {
			fmt.Fprintln(os.Stderr, "tuntopd: restore routing:", err)
		}
	}()

	fmt.Printf("server: %s\nTUN:    %s, all traffic except LAN goes through the server (Ctrl+C to stop)\n", name, nc.Iface)
	if *timeout > 0 {
		fmt.Printf("stops:  automatically in %s\n", *timeout)
	}
	waitStop(*timeout)
	fmt.Println("stopping, routing restored")
	return nil
}

func startXray(cfg []byte) (*xray.Instance, error) {
	if dir := xray.SetupAssets(); dir != "" {
		fmt.Printf("geo:    %s\n", dir)
	} else {
		fmt.Println("geo:    geoip.dat not found; profiles with geoip:/geosite: rules will fail")
	}
	return xray.StartJSON(cfg)
}

// waitStop blocks until SIGINT/SIGTERM or, if timeout > 0, the timeout.
func waitStop(timeout time.Duration) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	var expire <-chan time.Time
	if timeout > 0 {
		expire = time.After(timeout)
	}
	select {
	case <-sig:
	case <-expire:
	}
}

// oneArg returns the single positional argument, reading it from stdin
// when it is "-".
func oneArg(fs *flag.FlagSet) (string, error) {
	if fs.NArg() != 1 {
		return "", fmt.Errorf(`%s: expected exactly one argument (or "-" for stdin)`, fs.Name())
	}
	v := fs.Arg(0)
	if v != "-" {
		return strings.TrimSpace(v), nil
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("read from stdin: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://")
}
