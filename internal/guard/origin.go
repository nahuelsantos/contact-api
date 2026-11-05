// Package guard holds the spam and abuse checks run on every submission.
package guard

import (
	"net/http"
	"net/url"
	"strings"
)

// RequestOrigin returns the request's Origin header, falling back to the
// scheme://host of the Referer header, or "" when neither is usable.
func RequestOrigin(r *http.Request) string {
	if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" && o != "null" {
		return o
	}
	ref := strings.TrimSpace(r.Header.Get("Referer"))
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// OriginAllowed reports whether origin exactly matches one of allowed
// (scheme://host[:port], trailing slash and host case ignored).
func OriginAllowed(origin string, allowed []string) bool {
	o := normalizeOrigin(origin)
	if o == "" {
		return false
	}
	for _, a := range allowed {
		if n := normalizeOrigin(a); n != "" && n == o {
			return true
		}
	}
	return false
}

// normalizeOrigin trims a trailing "/" and lower-cases the scheme and host.
func normalizeOrigin(s string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(s), "/"))
}
