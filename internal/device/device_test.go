// SPDX-License-Identifier: GPL-3.0-or-later

package device

import "testing"

func TestDeriveHWIDStable(t *testing.T) {
	// Reference value: printf avpn-hwid | openssl dgst -sha256 -hmac 0123456789abcdef0123456789abcdef
	const want = "56bc6b50fd810414cbc0ee52f28ad2e3"
	got := DeriveHWID("0123456789abcdef0123456789abcdef\n")
	if len(got) != 32 {
		t.Fatalf("len = %d, want 32", len(got))
	}
	if got != DeriveHWID("0123456789abcdef0123456789abcdef") {
		t.Fatal("HWID depends on surrounding whitespace")
	}
	if got != want {
		t.Fatalf("HWID = %s, want %s (changing it re-registers every user's device)", got, want)
	}
}

func TestCurrent(t *testing.T) {
	i, err := Current()
	if err != nil {
		t.Skipf("no machine id on this host: %v", err)
	}
	h := i.Headers()
	if len(h["x-hwid"]) != 32 || h["x-device-os"] == "" || h["x-device-model"] != Model {
		t.Fatalf("headers = %v", h)
	}
}
