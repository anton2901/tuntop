// SPDX-License-Identifier: GPL-3.0-or-later

package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

type toServer struct{ u *url.URL }

func (t toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = t.u.Scheme, t.u.Host
	return http.DefaultTransport.RoundTrip(r)
}

// An error response may carry what was done anyway; the caller gets both.
func TestErrorCarriesPartialData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"save state: disk full","data":{"auto_connect":true}}`))
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := &Client{http: &http.Client{Transport: toServer{u}}}
	var s Settings
	err := c.do(context.Background(), "PUT", "/v1/settings", Settings{}, &s)
	if err == nil || err.Error() != "save state: disk full" {
		t.Fatalf("err = %v", err)
	}
	if !s.AutoConnect {
		t.Fatalf("data lost: %+v", s)
	}
}
