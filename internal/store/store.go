// SPDX-License-Identifier: GPL-3.0-or-later

// Package store persists tuntop's state: subscriptions, manual links and the
// selected server. Everything lives in one directory owned by root
// (/var/lib/tuntop), files are 0600 because subscription URLs and bodies are
// credentials.
//
//	state.json        State
//	subs/<id>.body    last downloaded body of each subscription
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/anton2901/tuntop/internal/sub"
)

// ManualID is the pseudo-subscription holding links added one by one; its
// body is the list of links. Same as api.ManualID.
const ManualID = "manual"

// State is what tuntop remembers between runs.
type State struct {
	Subscriptions []Subscription `json:"subscriptions"`
	// Selected is the server key ("<subscription id>:<entry key>") used by
	// "tuntop on" without arguments.
	Selected string `json:"selected,omitempty"`
	// Favorites are server keys shown at the top of the list.
	Favorites []string `json:"favorites,omitempty"`
	// KillSwitch blocks traffic outside the tunnel while the VPN is on.
	KillSwitch bool `json:"kill_switch,omitempty"`
	// AutoConnect: connect to the selected server when the service starts.
	AutoConnect bool `json:"auto_connect,omitempty"`
	// AutoSwitch: move to a working server when the current one stops answering.
	AutoSwitch bool `json:"auto_switch,omitempty"`
	// Mode is "tun" (default when empty) or "proxy"; ProxyPort is the SOCKS
	// port of the proxy mode.
	Mode      string `json:"mode,omitempty"`
	ProxyPort int    `json:"proxy_port,omitempty"`
	// AutoTestMinutes: measure all servers' latency every N minutes; 0 off.
	AutoTestMinutes int `json:"auto_test_minutes,omitempty"`
}

// Subscription is one source of servers.
type Subscription struct {
	ID  string `json:"id"`
	URL string `json:"url,omitempty"` // empty for ManualID
	// UserAgent overrides sub.DefaultUserAgent.
	UserAgent string    `json:"user_agent,omitempty"`
	NoHWID    bool      `json:"no_hwid,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	Info      sub.Info  `json:"info"`
	// LastError is the last failed update, cleared on success.
	LastError string `json:"last_error,omitempty"`
}

// Name returns the provider's title, or a fallback.
func (s Subscription) Name() string {
	switch {
	case s.Info.Title != "":
		return s.Info.Title
	case s.ID == ManualID:
		return "Manual"
	default:
		return s.ID
	}
}

// Store reads and writes state in Dir.
type Store struct {
	Dir string
}

// Load returns the saved state, or an empty one on first run.
func (s Store) Load() (*State, error) {
	b, err := os.ReadFile(filepath.Join(s.Dir, "state.json"))
	if errors.Is(err, os.ErrNotExist) {
		return &State{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("parse state: %w", err)
	}
	return &st, nil
}

// Save writes state atomically.
func (s Store) Save(st *State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return s.write("state.json", b)
}

// Body returns the cached body of a subscription (nil if none).
func (s Store) Body(id string) ([]byte, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid subscription id %q", id)
	}
	b, err := os.ReadFile(filepath.Join(s.Dir, "subs", id+".body"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// SaveBody caches a subscription body.
func (s Store) SaveBody(id string, body []byte) error {
	if !validID(id) {
		return fmt.Errorf("invalid subscription id %q", id)
	}
	return s.write(filepath.Join("subs", id+".body"), body)
}

// DeleteBody removes a cached body.
func (s Store) DeleteBody(id string) error {
	if !validID(id) {
		return fmt.Errorf("invalid subscription id %q", id)
	}
	err := os.Remove(filepath.Join(s.Dir, "subs", id+".body"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s Store) write(rel string, b []byte) error {
	path := filepath.Join(s.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

var idRe = regexp.MustCompile(`^[a-z0-9]{1,32}$`)

func validID(id string) bool { return idRe.MatchString(id) }

// NewID returns a random subscription id.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
