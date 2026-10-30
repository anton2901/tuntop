// SPDX-License-Identifier: GPL-3.0-or-later

package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/mod/semver"

	"github.com/anton2901/tuntop/internal/version"
)

// Where releases and the install script live; tests point them elsewhere.
var (
	releasesAPI = "https://api.github.com/repos/anton2901/tuntop/releases"
	scriptURL   = "https://raw.githubusercontent.com/anton2901/tuntop/main/scripts/get.sh"
)

// upgrade finds the release to install and hands over to scripts/get.sh,
// which verifies the download, installs it and restarts the service.
//
//	tuntop upgrade                 latest stable release
//	tuntop upgrade --pre           newest release, release candidates included
//	tuntop upgrade --version X     exactly X (also a downgrade)
//	tuntop upgrade --check         only say what would be installed
func (c *CLI) upgrade(ctx context.Context, args []string) error {
	var pre, check bool
	var want string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--pre":
			pre = true
		case a == "--check":
			check = true
		case a == "--version" && i+1 < len(args):
			i++
			want = args[i]
		case strings.HasPrefix(a, "--version="):
			want = strings.TrimPrefix(a, "--version=")
		default:
			return errors.New(c.T.S("upgrade: expected --pre, --check or --version X"))
		}
	}
	cur := "v" + strings.TrimPrefix(version.Version, "v")

	target := "v" + strings.TrimPrefix(want, "v")
	if want != "" {
		if !semver.IsValid(target) {
			return errors.New(c.T.F("upgrade: %q is not a version like 0.3.0 or 0.3.0-rc.1", want))
		}
	} else {
		var err error
		if target, err = latestRelease(ctx, pre); err != nil {
			return err
		}
		if semver.Compare(target, cur) <= 0 {
			c.println(c.T.F("tuntop is up to date: %s", version.Version))
			return nil
		}
	}
	if check {
		c.println(c.T.F("available: %s (installed: %s)", strings.TrimPrefix(target, "v"), version.Version))
		return nil
	}
	c.println(c.T.F("Installing %s (installed: %s). The service restarts: the VPN will be off.", strings.TrimPrefix(target, "v"), version.Version))
	run := c.RunInstaller
	if run == nil {
		run = c.runScript
	}
	return run(ctx, strings.TrimPrefix(target, "v"))
}

// latestRelease returns the tag of the newest published release; with pre
// it may be a pre-release, otherwise only stable ones count.
func latestRelease(ctx context.Context, pre bool) (string, error) {
	url := releasesAPI + "/latest"
	if pre {
		url = releasesAPI + "?per_page=20"
	}
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", err
	}
	type release struct {
		Tag   string `json:"tag_name"`
		Draft bool   `json:"draft"`
	}
	var list []release
	if pre {
		err = json.Unmarshal(body, &list)
	} else {
		var one release
		if err = json.Unmarshal(body, &one); err == nil {
			list = []release{one}
		}
	}
	if err != nil {
		return "", fmt.Errorf("read the release list: %w", err)
	}
	best := ""
	for _, r := range list {
		if r.Draft || !semver.IsValid(r.Tag) {
			continue
		}
		if best == "" || semver.Compare(r.Tag, best) > 0 {
			best = r.Tag
		}
	}
	if best == "" {
		return "", errors.New("no release found")
	}
	return best, nil
}

func httpGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach GitHub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub answered HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// runScript downloads scripts/get.sh and runs it for one version, with the
// terminal attached (sudo asks for the password there).
func (c *CLI) runScript(ctx context.Context, ver string) error {
	script, err := httpGet(ctx, scriptURL)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "sh", "-s")
	cmd.Stdin = strings.NewReader(string(script))
	cmd.Stdout, cmd.Stderr = c.Out, c.Err
	cmd.Env = append(os.Environ(), "TUNTOP_VERSION="+ver)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("install script: %w", err)
	}
	return nil
}
