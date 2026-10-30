// SPDX-License-Identifier: GPL-3.0-or-later

package daemon

import (
	"context"
	"testing"

	"github.com/anton2901/tuntop/internal/api"
	"github.com/anton2901/tuntop/internal/store"
	"github.com/anton2901/tuntop/internal/sub"
)

func TestChoicesFollowChangedSettings(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: "https://a.example.com/s/a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.SetFavorite(ctx, "NL", true); err != nil {
		t.Fatal(err)
	}
	if _, err := e.client.Connect(ctx, "NL"); err != nil {
		t.Fatal(err)
	}
	// The provider moves NL to another host (new key) and its load changes.
	e.prov.bodies["https://a.example.com/s/a"] = vless("de.example.com", "🇩🇪 Germany") + "\n" + vless("nl2.example.com", "🇳🇱 NL · 🟡50%")
	if _, err := e.client.Update(ctx, ""); err != nil {
		t.Fatal(err)
	}
	st, _ := e.client.Status(ctx)
	if st.Server == nil || st.Server.Name != "🇳🇱 NL · 🟡50%" {
		t.Fatalf("running connection lost its server: %+v", st.Server)
	}
	list, _ := e.client.Servers(ctx)
	if !list[0].Favorite || list[0].Name != "🇳🇱 NL · 🟡50%" {
		t.Fatalf("favorite not carried over: %+v", list[0])
	}
	_, _ = e.client.Disconnect(ctx)
	if st, err := e.client.Connect(ctx, ""); err != nil || st.Server.Name != "🇳🇱 NL · 🟡50%" {
		t.Fatalf("selection not carried over: %+v, %v", st.Server, err)
	}
}

func TestRemapKeys(t *testing.T) {
	parse := func(body string) *sub.Subscription {
		p, err := sub.Parse([]byte(body), nil)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	old := parse(vless("a.example.com", "A · 🟢1%") + "\n" + vless("b.example.com", "Twin") + "\n" + vless("c.example.com", "Twin"))
	cur := parse(vless("a2.example.com", "A · 🔴99%") + "\n" + vless("b2.example.com", "Twin") + "\n" + vless("c.example.com", "Twin"))
	m := remapKeys(old, cur)
	if len(m) != 1 || m[old.Entries[0].Key] != cur.Entries[0].Key {
		t.Fatalf("remap = %v; want only A (Twin is ambiguous)", m)
	}
}

func TestFavoritesFirst(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, u := range []string{"https://a.example.com/s/a", "https://b.example.com/s/b"} {
		if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: u}); err != nil {
			t.Fatal(err)
		}
	}
	s, err := e.client.SetFavorite(ctx, "Finland", true)
	if err != nil || !s.Favorite || s.Index != 1 {
		t.Fatalf("set favorite: %+v, %v", s, err)
	}
	list, _ := e.client.Servers(ctx)
	if list[0].Name != "🇫🇮 Finland" || list[1].Name != "🇳🇱 NL · 🟢10%" || list[1].Favorite {
		t.Fatalf("order: %v / %v", list[0].Name, list[1].Name)
	}
	// Numbers follow the shown order.
	if st, err := e.client.Connect(ctx, "1"); err != nil || st.Server.Name != "🇫🇮 Finland" {
		t.Fatalf("connect 1: %+v, %v", st.Server, err)
	}
	if _, err := e.client.SetFavorite(ctx, "Finland", false); err != nil {
		t.Fatal(err)
	}
	if list, _ = e.client.Servers(ctx); list[0].Favorite || list[2].Name != "🇫🇮 Finland" {
		t.Fatalf("unfavorite: %+v", list)
	}
	// Removing a subscription drops its favorites.
	_, _ = e.client.SetFavorite(ctx, "Finland", true)
	subs, _ := e.client.Subscriptions(ctx)
	if err := e.client.RemoveSubscription(ctx, subs[1].ID); err != nil {
		t.Fatal(err)
	}
	if len(e.d.st.Favorites) != 0 {
		t.Fatalf("favorites of a removed subscription kept: %v", e.d.st.Favorites)
	}
}

func TestMoveSubscription(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	for _, u := range []string{"https://a.example.com/s/a", "https://b.example.com/s/b"} {
		if _, err := e.client.AddSubscription(ctx, api.AddSubscriptionRequest{URL: u}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := e.client.AddLinks(ctx, []string{vless("x.example.com", "Mine")}); err != nil {
		t.Fatal(err)
	}
	subs, _ := e.client.Subscriptions(ctx)
	b := subs[1].ID
	subs, err := e.client.MoveSubscription(ctx, b, 1)
	if err != nil || subs[0].ID != b || subs[2].Name != "Manual" {
		t.Fatalf("move up: %+v, %v", subs, err)
	}
	list, _ := e.client.Servers(ctx)
	if list[0].Name != "🇫🇮 Finland" || list[3].Name != "Mine" {
		t.Fatalf("servers follow the order: %v %v", list[0].Name, list[3].Name)
	}
	// Past the end: last before manual links.
	if subs, _ = e.client.MoveSubscription(ctx, b, 9); subs[1].ID != b || subs[2].Name != "Manual" {
		t.Fatalf("move down: %+v", subs)
	}
	e.d.mu.Lock()
	last := e.d.st.Subscriptions[len(e.d.st.Subscriptions)-1].ID
	e.d.mu.Unlock()
	if last != store.ManualID {
		t.Fatalf("manual links are not last in the state: %s", last)
	}
	if _, err := e.client.MoveSubscription(ctx, "manual", 1); err == nil {
		t.Fatal("manual links moved")
	}
}
