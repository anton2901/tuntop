// SPDX-License-Identifier: GPL-3.0-or-later

package sub

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Info is provider metadata sent in subscription response headers (the
// de facto standard used by Happ, v2rayN and most panels).
type Info struct {
	Title string
	// Upload, Download and Total are bytes; Total 0 means unlimited.
	Upload, Download, Total int64
	// Expire is zero when the subscription does not expire.
	Expire time.Time
	// UpdateInterval is how often the provider asks clients to refresh.
	UpdateInterval time.Duration
	SupportURL     string
	WebPageURL     string
	Announce       string
}

// ParseInfo extracts Info from response headers; h may be nil.
func ParseInfo(h http.Header) Info {
	if h == nil {
		return Info{}
	}
	i := Info{
		Title:      decodeHeader(h.Get("Profile-Title")),
		SupportURL: h.Get("Support-Url"),
		WebPageURL: h.Get("Profile-Web-Page-Url"),
		Announce:   decodeHeader(h.Get("Announce")),
	}
	if n, err := strconv.Atoi(strings.TrimSpace(h.Get("Profile-Update-Interval"))); err == nil && n > 0 {
		i.UpdateInterval = time.Duration(n) * time.Hour
	}
	for _, kv := range strings.Split(h.Get("Subscription-Userinfo"), ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(kv), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			continue
		}
		switch k {
		case "upload":
			i.Upload = n
		case "download":
			i.Download = n
		case "total":
			i.Total = n
		case "expire":
			if n > 0 {
				i.Expire = time.Unix(n, 0)
			}
		}
	}
	return i
}

// decodeHeader handles the "base64:..." convention for non-ASCII values.
func decodeHeader(v string) string {
	v = strings.TrimSpace(v)
	if rest, ok := strings.CutPrefix(v, "base64:"); ok {
		if b, err := decodeBase64(rest); err == nil {
			return strings.TrimSpace(string(b))
		}
	}
	return v
}
