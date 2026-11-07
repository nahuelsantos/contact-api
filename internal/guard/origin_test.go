package guard_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/guard"
)

func TestRequestOrigin_HeaderThenReferer(t *testing.T) {
	tests := []struct {
		name    string
		origin  string
		referer string
		want    string
	}{
		{"origin header", "https://example.com", "https://other.test/x", "https://example.com"},
		{"null origin falls to referer", "null", "https://example.com:8443/contact?a=b", "https://example.com:8443"},
		{"referer only", "", "https://example.com/contact", "https://example.com"},
		{"neither", "", "", ""},
		{"bad referer", "", "://nope", ""},
		{"referer without host", "", "/relative", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			if tt.origin != "" {
				r.Header.Set("Origin", tt.origin)
			}
			if tt.referer != "" {
				r.Header.Set("Referer", tt.referer)
			}
			if got := guard.RequestOrigin(r); got != tt.want {
				t.Fatalf("RequestOrigin = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOriginAllowed_ExactMatchOnly(t *testing.T) {
	allowed := []string{"https://example.com", "https://www.example.com/"}
	tests := []struct {
		origin string
		want   bool
	}{
		{"https://example.com", true},
		{"https://example.com/", true},
		{"https://EXAMPLE.com", true},
		{"https://www.example.com", true},
		{"https://example.com.evil.net", false},
		{"http://example.com", false},
		{"https://example.com:8443", false},
		{"https://sub.example.com", false},
	}
	for _, tt := range tests {
		if got := guard.OriginAllowed(tt.origin, allowed); got != tt.want {
			t.Errorf("OriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
		}
	}
}

func TestOriginAllowed_Empty(t *testing.T) {
	if guard.OriginAllowed("", []string{"https://example.com"}) {
		t.Fatal("empty origin must not be allowed")
	}
	if guard.OriginAllowed("", []string{""}) {
		t.Fatal("empty origin must not match an empty allowed entry")
	}
	if guard.OriginAllowed("https://example.com", nil) {
		t.Fatal("no allowed origins must reject")
	}
}
