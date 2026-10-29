// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"
)

// ErrNoDaemon means nothing listens on the socket.
var ErrNoDaemon = errors.New("tuntopd is not running")

// Client talks to tuntopd.
type Client struct {
	Socket string
	http   *http.Client
}

// NewClient returns a client for socket ("" means $TUNTOP_SOCKET or
// DefaultSocket).
func NewClient(socket string) *Client {
	if socket == "" {
		socket = os.Getenv("TUNTOP_SOCKET")
	}
	if socket == "" {
		socket = DefaultSocket
	}
	c := &Client{Socket: socket}
	c.http = &http.Client{
		// Updates download subscriptions, possibly through fallbacks.
		Timeout: 5 * time.Minute,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socket)
			},
		},
	}
	return c
}

// Status returns the daemon state.
func (c *Client) Status(ctx context.Context) (Status, error) {
	var s Status
	return s, c.do(ctx, http.MethodGet, "/v1/status", nil, &s)
}

// Log returns the last lines of the service log.
func (c *Client) Log(ctx context.Context) ([]string, error) {
	var l []string
	return l, c.do(ctx, http.MethodGet, "/v1/log", nil, &l)
}

// Traffic returns throughput and its recent history.
func (c *Client) Traffic(ctx context.Context) (Traffic, error) {
	var t Traffic
	return t, c.do(ctx, http.MethodGet, "/v1/traffic", nil, &t)
}

// Settings returns the service settings.
func (c *Client) Settings(ctx context.Context) (Settings, error) {
	var s Settings
	return s, c.do(ctx, http.MethodGet, "/v1/settings", nil, &s)
}

// SetSettings changes the service settings.
func (c *Client) SetSettings(ctx context.Context, s Settings) (Settings, error) {
	var out Settings
	return out, c.do(ctx, http.MethodPut, "/v1/settings", s, &out)
}

// Events streams events to fn until ctx ends or the connection drops.
func (c *Client) Events(ctx context.Context, fn func(Event)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://tuntopd/v1/events", http.NoBody)
	if err != nil {
		return err
	}
	// The stream is long-lived: no overall timeout, unlike c.http.
	hc := &http.Client{Transport: c.http.Transport}
	resp, err := hc.Do(req)
	if err != nil {
		return c.wrapErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tuntopd: HTTP %s", resp.Status)
	}
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		data, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue // comments (keep-alives) and blank separators
		}
		var e Event
		if json.Unmarshal([]byte(data), &e) == nil {
			fn(e)
		}
	}
	if err := sc.Err(); err != nil && ctx.Err() == nil {
		return err
	}
	return errors.New("tuntopd closed the event stream")
}

// Servers lists all servers.
func (c *Client) Servers(ctx context.Context) ([]Server, error) {
	var s []Server
	return s, c.do(ctx, http.MethodGet, "/v1/servers", nil, &s)
}

// Connect connects to a server (key, number or part of the name; empty for
// the selected one).
func (c *Client) Connect(ctx context.Context, server string) (Status, error) {
	var s Status
	return s, c.do(ctx, http.MethodPost, "/v1/connect", ConnectRequest{Server: server}, &s)
}

// Disconnect stops the tunnel.
func (c *Client) Disconnect(ctx context.Context) (Status, error) {
	var s Status
	return s, c.do(ctx, http.MethodPost, "/v1/disconnect", nil, &s)
}

// Subscriptions lists subscriptions.
func (c *Client) Subscriptions(ctx context.Context) ([]Subscription, error) {
	var s []Subscription
	return s, c.do(ctx, http.MethodGet, "/v1/subscriptions", nil, &s)
}

// AddSubscription adds and downloads a subscription.
func (c *Client) AddSubscription(ctx context.Context, req AddSubscriptionRequest) (Subscription, error) {
	var s Subscription
	return s, c.do(ctx, http.MethodPost, "/v1/subscriptions", req, &s)
}

// RemoveSubscription deletes a subscription.
func (c *Client) RemoveSubscription(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/v1/subscriptions/"+url.PathEscape(id), nil, nil)
}

// MoveSubscription puts a subscription at a 1-based position.
func (c *Client) MoveSubscription(ctx context.Context, id string, to int) ([]Subscription, error) {
	var s []Subscription
	return s, c.do(ctx, http.MethodPost, "/v1/subscriptions/"+url.PathEscape(id)+"/move", MoveRequest{To: to}, &s)
}

// SetFavorite marks or unmarks a server (key, number or part of the name).
func (c *Client) SetFavorite(ctx context.Context, server string, on bool) (Server, error) {
	var s Server
	return s, c.do(ctx, http.MethodPost, "/v1/favorites", FavoriteRequest{Server: server, Favorite: on}, &s)
}

// Update refreshes one subscription or all (id "").
func (c *Client) Update(ctx context.Context, id string) ([]Subscription, error) {
	var s []Subscription
	return s, c.do(ctx, http.MethodPost, "/v1/update", UpdateRequest{ID: id}, &s)
}

// Test measures latency of the given servers (all when keys is empty) and
// returns the full server list with results.
func (c *Client) Test(ctx context.Context, keys []string) ([]Server, error) {
	var s []Server
	return s, c.do(ctx, http.MethodPost, "/v1/test", TestRequest{Keys: keys}, &s)
}

// AddLinks adds individual share links.
func (c *Client) AddLinks(ctx context.Context, links []string) ([]Server, error) {
	var s []Server
	return s, c.do(ctx, http.MethodPost, "/v1/links", AddLinksRequest{Links: links}, &s)
}

// RemoveServer deletes a manually added link.
func (c *Client) RemoveServer(ctx context.Context, key string) error {
	return c.do(ctx, http.MethodDelete, "/v1/servers/"+url.PathEscape(key), nil, nil)
}

// wrapErr turns socket errors into readable ones.
func (c *Client) wrapErr(err error) error {
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("%w (socket %s)", ErrNoDaemon, c.Socket)
	}
	if errors.Is(err, syscall.EACCES) {
		return fmt.Errorf("no access to %s: add yourself to the tuntop socket group", c.Socket)
	}
	return err
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader = http.NoBody
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://tuntopd"+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return c.wrapErr(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		var e Error
		if json.NewDecoder(resp.Body).Decode(&e) == nil && e.Error != "" {
			return errors.New(e.Error)
		}
		return fmt.Errorf("tuntopd: HTTP %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
