// SPDX-License-Identifier: GPL-3.0-or-later

package version

import (
	"regexp"
	"strings"
	"testing"
)

// SemVer 2.0.0, see https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func TestVersionIsSemVer(t *testing.T) {
	if !semver.MatchString(Version) {
		t.Fatalf("Version %q is not a valid SemVer string", Version)
	}
}

func TestStringContainsVersion(t *testing.T) {
	if !strings.HasPrefix(String(), Version) {
		t.Fatalf("String() = %q, want prefix %q", String(), Version)
	}
}
