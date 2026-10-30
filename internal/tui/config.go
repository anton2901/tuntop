// SPDX-License-Identifier: GPL-3.0-or-later

package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Config holds the TUI's own preferences, per user, in
// $XDG_CONFIG_HOME/tuntop/tui.json. Service settings live in tuntopd.
type Config struct {
	Theme string `json:"theme,omitempty"`
	// Lang is "auto", "en" or "ru".
	Lang string `json:"lang,omitempty"`
	// HideStatus and HideTraffic collapse the top panels (keys 1 and 2).
	HideStatus  bool `json:"hide_status,omitempty"`
	HideTraffic bool `json:"hide_traffic,omitempty"`
	// Sort orders servers inside groups ("", "latency", "name"); Folded
	// lists the collapsed groups (subscription IDs, "\x01" for favorites).
	Sort   string   `json:"sort,omitempty"`
	Folded []string `json:"folded,omitempty"`
}

// ConfigPath returns where the TUI config is stored.
func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "tuntop", "tui.json")
}

// LoadConfig reads the config; a missing file gives defaults.
func LoadConfig(path string) Config {
	c := Config{Lang: "auto"}
	if path == "" {
		return c
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

// Save writes the config, creating the directory.
func (c Config) Save(path string) error {
	if path == "" {
		return errors.New("no config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
