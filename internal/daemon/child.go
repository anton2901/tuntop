// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/anton2901/tuntop/internal/xray"

	"golang.org/x/sys/unix"
)

// The tunnel's Xray runs in a child process. Xray's TUN inbound has no
// shutdown path: closing the instance leaves the TUN device, its gVisor
// stack and goroutines alive (on macOS the utun stays busy and the next
// connect fails). A process exit releases all of it, and an Xray crash no
// longer takes the service down.
//
// Protocol: the parent writes the Xray JSON config to the child's stdin and
// closes it (credentials never touch the disk or the process list). The
// child prints "ready" once Xray runs, then "traffic <up> <down>" every
// second; on SIGTERM it stops Xray and exits. If the parent dies, the next
// write to the closed pipe ends the child.

// ChildCommand is the hidden tuntopd command that runs one Xray config.
const ChildCommand = "xray-child"

// childReadyTimeout covers loading large geosite databases.
const childReadyTimeout = 20 * time.Second

// RunChild is the child side; it returns when stop is closed.
func RunChild(in io.Reader, out io.Writer, stop <-chan os.Signal) error {
	cfg, err := io.ReadAll(in)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	xray.SetupAssets()
	inst, err := xray.StartJSON(cfg)
	if err != nil {
		return err
	}
	defer func() { _ = inst.Close() }()
	if _, err := fmt.Fprintln(out, "ready"); err != nil {
		return err
	}
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return nil
		case <-t.C:
			up, down := inst.Traffic()
			if _, err := fmt.Fprintf(out, "traffic %d %d\n", up, down); err != nil {
				return nil // parent gone
			}
		}
	}
}

// RunChildMain runs the child on the process's stdio until SIGTERM.
func RunChildMain() error {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, os.Interrupt)
	proto, err := protocolOut()
	if err != nil {
		return err
	}
	return RunChild(os.Stdin, proto, sig)
}

// protocolOut moves the protocol off fd 1 and points fd 1 at stderr. Xray
// writes its console log to stdout, where the parent would read it as
// protocol lines and drop it; this way the log reaches the service log.
func protocolOut() (*os.File, error) {
	fd, err := unix.Dup(1)
	if err != nil {
		return nil, err
	}
	if err := unix.Dup2(2, 1); err != nil {
		return nil, err
	}
	os.Stdout = os.Stderr
	return os.NewFile(uintptr(fd), "protocol"), nil
}

// child is the parent's handle on a running child.
type child struct {
	cmd      *exec.Cmd
	up, down atomic.Int64
	exited   chan struct{} // closed when the process is gone
	stderr   *tail
	stopping atomic.Bool
}

// startChild runs exe with args as an Xray child for cfg and waits until
// it is ready.
func startChild(exe string, args []string, cfg []byte) (*child, error) {
	c := &child{exited: make(chan struct{}), stderr: &tail{max: 20}}
	c.cmd = exec.Command(exe, args...)
	c.cmd.Stdin = bytes.NewReader(cfg)
	// Xray's own log goes to the service log and is kept for errors.
	c.cmd.Stderr = io.MultiWriter(os.Stderr, c.stderr)
	out, err := c.cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.cmd.Start(); err != nil {
		return nil, fmt.Errorf("start xray child: %w", err)
	}

	ready := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(out)
		once := sync.Once{}
		for sc.Scan() {
			line := sc.Text()
			if line == "ready" {
				once.Do(func() { close(ready) })
				continue
			}
			var up, down int64
			if _, err := fmt.Sscanf(line, "traffic %d %d", &up, &down); err == nil {
				c.up.Store(up)
				c.down.Store(down)
			}
		}
		_ = c.cmd.Wait()
		close(c.exited)
	}()

	select {
	case <-ready:
		return c, nil
	case <-c.exited:
		return nil, fmt.Errorf("xray: %s", c.stderr.lastError())
	case <-time.After(childReadyTimeout):
		_ = c.stop()
		return nil, errors.New("xray did not start in time")
	}
}

// stop asks the child to exit and waits; it kills it if it hangs.
func (c *child) stop() error {
	c.stopping.Store(true)
	select {
	case <-c.exited:
		return nil
	default:
	}
	_ = c.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-c.exited:
		return nil
	case <-time.After(3 * time.Second):
		_ = c.cmd.Process.Kill()
		<-c.exited
		return errors.New("xray child did not stop, killed")
	}
}

// tail keeps the last lines written to it.
type tail struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lines = append(t.lines, strings.Split(strings.TrimRight(string(p), "\n"), "\n")...)
	if over := len(t.lines) - t.max; over > 0 {
		t.lines = t.lines[over:]
	}
	return len(p), nil
}

// lastError is the most useful line for a failed start: the last one
// mentioning a failure, else the last line.
func (t *tail) lastError() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	for i := len(t.lines) - 1; i >= 0; i-- {
		if l := strings.ToLower(t.lines[i]); strings.Contains(l, "fail") || strings.Contains(l, "error") {
			return strings.TrimSpace(t.lines[i])
		}
	}
	if len(t.lines) > 0 {
		return strings.TrimSpace(t.lines[len(t.lines)-1])
	}
	return "exited before start"
}
