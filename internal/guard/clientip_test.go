package guard_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/guard"
)

func TestClientIP_PrefersCFConnectingIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "172.18.0.5:41234"
	r.Header.Set("CF-Connecting-IP", "203.0.113.9")
	if got := guard.ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("ClientIP = %q", got)
	}
}

func TestClientIP_IgnoresXForwardedFor(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "172.18.0.5:41234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.Header.Set("X-Real-IP", "203.0.113.10")
	if got := guard.ClientIP(r); got != "172.18.0.5" {
		t.Fatalf("ClientIP = %q, want RemoteAddr host", got)
	}
}

func TestClientIP_InvalidCFHeaderFallsBack(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "172.18.0.5:41234"
	r.Header.Set("CF-Connecting-IP", "not-an-ip")
	if got := guard.ClientIP(r); got != "172.18.0.5" {
		t.Fatalf("ClientIP = %q", got)
	}
}

func TestClientIP_RemoteAddrWithoutPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.RemoteAddr = "172.18.0.5"
	if got := guard.ClientIP(r); got != "172.18.0.5" {
		t.Fatalf("ClientIP = %q", got)
	}
}
