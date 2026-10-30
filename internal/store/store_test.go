// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	st, err := s.Load()
	if err != nil || len(st.Subscriptions) != 0 {
		t.Fatalf("first load = %+v, %v", st, err)
	}
	id := NewID()
	st.Subscriptions = append(st.Subscriptions, Subscription{ID: id, URL: "https://sub.example.com/x"})
	st.Selected = id + ":x123"
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveBody(id, []byte("body")); err != nil {
		t.Fatal(err)
	}

	got, err := s.Load()
	if err != nil || got.Selected != st.Selected || got.Subscriptions[0].URL != "https://sub.example.com/x" {
		t.Fatalf("reload = %+v, %v", got, err)
	}
	b, err := s.Body(id)
	if err != nil || string(b) != "body" {
		t.Fatalf("body = %q, %v", b, err)
	}
	for _, f := range []string{"state.json", "subs/" + id + ".body"} {
		fi, err := os.Stat(filepath.Join(s.Dir, f))
		if err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: mode %v, %v (want 0600: holds credentials)", f, fi.Mode().Perm(), err)
		}
	}
	if err := s.DeleteBody(id); err != nil {
		t.Fatal(err)
	}
	if b, _ := s.Body(id); b != nil {
		t.Fatal("body not deleted")
	}
}

func TestRejectsPathTraversal(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	for _, id := range []string{"../x", "a/b", "", "UPPER"} {
		if err := s.SaveBody(id, nil); err == nil {
			t.Errorf("SaveBody(%q) succeeded", id)
		}
	}
}

func TestBrokenStateIsSetAside(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"subscriptions": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := Store{Dir: dir}
	st, err := s.Load()
	if err != nil || st == nil || len(st.Subscriptions) != 0 {
		t.Fatalf("Load = %+v, %v", st, err)
	}
	broken, _ := filepath.Glob(filepath.Join(dir, "state.json.broken-*"))
	if len(broken) != 1 {
		t.Fatalf("broken copies: %v", broken)
	}
	if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
		t.Fatal("broken state.json left in place")
	}
	if err := s.Save(st); err != nil {
		t.Fatal(err)
	}
}
