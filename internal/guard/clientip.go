package guard

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP returns the CF-Connecting-IP header when it is a valid IP, else
// the host part of RemoteAddr. X-Forwarded-For and X-Real-IP are never read:
// clients can set them freely.
func ClientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); v != "" {
		if ip := net.ParseIP(v); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
