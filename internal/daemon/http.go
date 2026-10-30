// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"time"

	"github.com/anton2901/tuntop/internal/api"
)

// Handler serves the API described in package api.
func (d *Daemon) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, d.Status(), nil)
	})
	mux.HandleFunc("GET /v1/log", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, d.logs.Lines(), nil)
	})
	mux.HandleFunc("GET /v1/events", d.serveEvents)
	mux.HandleFunc("GET /v1/traffic", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, d.Traffic(), nil)
	})
	mux.HandleFunc("GET /v1/settings", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, d.Settings(), nil)
	})
	mux.HandleFunc("PUT /v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var req api.Settings
		if !decode(w, r, &req) {
			return
		}
		s, err := d.SetSettings(req)
		reply(w, s, err)
	})
	mux.HandleFunc("GET /v1/servers", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, nonNil(d.Servers()), nil)
	})
	mux.HandleFunc("POST /v1/connect", func(w http.ResponseWriter, r *http.Request) {
		var req api.ConnectRequest
		if !decode(w, r, &req) {
			return
		}
		st, err := d.Connect(req.Server)
		reply(w, st, err)
	})
	mux.HandleFunc("POST /v1/disconnect", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, d.Disconnect(), nil)
	})
	mux.HandleFunc("GET /v1/subscriptions", func(w http.ResponseWriter, _ *http.Request) {
		reply(w, d.Subscriptions(), nil)
	})
	mux.HandleFunc("POST /v1/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		var req api.AddSubscriptionRequest
		if !decode(w, r, &req) {
			return
		}
		s, err := d.AddSubscription(r.Context(), req)
		reply(w, s, err)
	})
	mux.HandleFunc("DELETE /v1/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, struct{}{}, d.RemoveSubscription(r.PathValue("id")))
	})
	mux.HandleFunc("POST /v1/subscriptions/{id}/move", func(w http.ResponseWriter, r *http.Request) {
		var req api.MoveRequest
		if !decode(w, r, &req) {
			return
		}
		subs, err := d.MoveSubscription(r.PathValue("id"), req.To)
		reply(w, subs, err)
	})
	mux.HandleFunc("POST /v1/favorites", func(w http.ResponseWriter, r *http.Request) {
		var req api.FavoriteRequest
		if !decode(w, r, &req) {
			return
		}
		s, err := d.SetFavorite(req.Server, req.Favorite)
		reply(w, s, err)
	})
	mux.HandleFunc("POST /v1/update", func(w http.ResponseWriter, r *http.Request) {
		var req api.UpdateRequest
		if !decode(w, r, &req) {
			return
		}
		subs, err := d.Update(r.Context(), req.ID)
		reply(w, subs, err)
	})
	mux.HandleFunc("POST /v1/test", func(w http.ResponseWriter, r *http.Request) {
		var req api.TestRequest
		if !decode(w, r, &req) {
			return
		}
		s, err := d.Test(r.Context(), req.Keys)
		reply(w, nonNil(s), err)
	})
	mux.HandleFunc("POST /v1/links", func(w http.ResponseWriter, r *http.Request) {
		var req api.AddLinksRequest
		if !decode(w, r, &req) {
			return
		}
		s, err := d.AddLinks(req.Links)
		reply(w, nonNil(s), err)
	})
	mux.HandleFunc("DELETE /v1/servers/{key}", func(w http.ResponseWriter, r *http.Request) {
		reply(w, struct{}{}, d.RemoveServer(r.PathValue("key")))
	})
	return mux
}

func nonNil(s []api.Server) []api.Server {
	if s == nil {
		return []api.Server{}
	}
	return s
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if r.ContentLength == 0 {
		return true
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		reply(w, nil, fmt.Errorf("bad request: %w", err))
		return false
	}
	return true
}

// reply writes v, or err as {"error": ...} with status 400. A v returned
// together with the error (a partly failed update) travels as "data".
func reply(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		e := api.Error{Error: err.Error()}
		if v != nil {
			if b, merr := json.Marshal(v); merr == nil && string(b) != "null" && string(b) != "{}" {
				e.Data = b
			}
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(e)
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

// ListenUnix opens the control socket. Access is limited by file mode
// 0660 and group: members of group (e.g. "wheel") may control tuntop,
// everyone else may not. An empty group leaves the socket root-only.
func ListenUnix(path, group string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	mode := os.FileMode(0o600)
	if group != "" {
		g, err := user.LookupGroup(group)
		if err != nil {
			_ = l.Close()
			return nil, fmt.Errorf("socket group: %w", err)
		}
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(path, 0, gid); err != nil {
			_ = l.Close()
			return nil, err
		}
		mode = 0o660
	}
	if err := os.Chmod(path, mode); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// serveEvents streams events as server-sent events until the client goes.
func (d *Daemon) serveEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		reply(w, nil, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	ch, stop := d.Subscribe()
	defer stop()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e := <-ch:
			b, _ := json.Marshal(e)
			if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
				return
			}
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
		}
		fl.Flush()
	}
}
