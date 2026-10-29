// SPDX-License-Identifier: GPL-3.0-or-later

// Package device describes this machine to subscription providers.
//
// Panels with a device limit (Remnawave and others) only return servers
// when the client sends a hardware id and a few device headers, the way
// Happ does. The id is derived from the OS machine id with HMAC, so it is
// stable across reinstalls of tuntop but does not reveal the machine id.
package device

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

// Model is sent as the device model so the provider's device list shows
// which client registered the slot.
const Model = "tuntop"

// hwidKey keys the HMAC; changing it changes every user's HWID and makes
// providers count a new device, so it must stay fixed.
const hwidKey = "avpn-hwid"

// Info is what providers see about this device.
type Info struct {
	HWID      string
	OS        string
	OSVersion string
	Model     string
}

// Current returns the Info for this machine.
func Current() (Info, error) {
	id, err := machineID()
	if err != nil {
		return Info{}, err
	}
	return Info{
		HWID:      DeriveHWID(id),
		OS:        osName(),
		OSVersion: osVersion(),
		Model:     Model,
	}, nil
}

// DeriveHWID turns a machine id into the HWID sent to providers.
func DeriveHWID(machineID string) string {
	m := hmac.New(sha256.New, []byte(strings.TrimSpace(machineID)))
	m.Write([]byte(hwidKey))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// Headers returns the HTTP headers Happ-compatible panels expect.
func (i Info) Headers() map[string]string {
	return map[string]string{
		"x-hwid":         i.HWID,
		"x-device-os":    i.OS,
		"x-ver-os":       i.OSVersion,
		"x-device-model": i.Model,
	}
}

func machineID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
			if b, err := os.ReadFile(p); err == nil && len(strings.TrimSpace(string(b))) > 0 {
				return strings.TrimSpace(string(b)), nil
			}
		}
		return "", errors.New("machine id not found in /etc/machine-id")
	case "darwin":
		out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
		if err != nil {
			return "", fmt.Errorf("read IOPlatformUUID: %w", err)
		}
		m := regexp.MustCompile(`"IOPlatformUUID" = "([^"]+)"`).FindSubmatch(out)
		if m == nil {
			return "", errors.New("IOPlatformUUID not found")
		}
		return string(m[1]), nil
	default:
		return "", fmt.Errorf("machine id: unsupported OS %s", runtime.GOOS)
	}
}

func osName() string {
	switch runtime.GOOS {
	case "linux":
		return "Linux"
	case "darwin":
		return "macOS"
	default:
		return runtime.GOOS
	}
}

func osVersion() string {
	switch runtime.GOOS {
	case "linux":
		if b, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			return strings.TrimSpace(string(b))
		}
	case "darwin":
		if out, err := exec.Command("sw_vers", "-productVersion").Output(); err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return ""
}
