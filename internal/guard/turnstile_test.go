package guard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nahuelsantos/contact-api/internal/guard"
)

func TestVerify_Success(t *testing.T) {
	var gotCT, gotMethod string
	var gotForm map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotForm = map[string]string{
			"secret":   r.PostForm.Get("secret"),
			"response": r.PostForm.Get("response"),
			"remoteip": r.PostForm.Get("remoteip"),
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()

	ts := guard.NewTurnstile(srv.Client(), srv.URL)
	if err := ts.Verify(context.Background(), "sek", "tok", "203.0.113.9"); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if gotMethod != http.MethodPost || !strings.HasPrefix(gotCT, "application/x-www-form-urlencoded") {
		t.Fatalf("method=%q content-type=%q", gotMethod, gotCT)
	}
	want := map[string]string{"secret": "sek", "response": "tok", "remoteip": "203.0.113.9"}
	for k, v := range want {
		if gotForm[k] != v {
			t.Errorf("form %s = %q, want %q", k, gotForm[k], v)
		}
	}
}

func TestVerify_Rejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"error-codes":["invalid-input-response"]}`))
	}))
	defer srv.Close()
	err := guard.NewTurnstile(srv.Client(), srv.URL).Verify(context.Background(), "sek", "tok", "")
	if !errors.Is(err, guard.ErrTurnstileRejected) {
		t.Fatalf("err = %v, want ErrTurnstileRejected", err)
	}
}

func TestVerify_EmptyToken(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer srv.Close()
	err := guard.NewTurnstile(srv.Client(), srv.URL).Verify(context.Background(), "sek", "  ", "")
	if !errors.Is(err, guard.ErrTurnstileRejected) {
		t.Fatalf("err = %v, want ErrTurnstileRejected", err)
	}
	if calls.Load() != 0 {
		t.Fatal("empty token must not trigger an HTTP call")
	}
}

func TestVerify_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	err := guard.NewTurnstile(srv.Client(), srv.URL).Verify(context.Background(), "sek", "tok", "")
	if !errors.Is(err, guard.ErrTurnstileUnavailable) {
		t.Fatalf("err = %v, want ErrTurnstileUnavailable", err)
	}
}

func TestVerify_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>`))
	}))
	defer srv.Close()
	err := guard.NewTurnstile(srv.Client(), srv.URL).Verify(context.Background(), "sek", "tok", "")
	if !errors.Is(err, guard.ErrTurnstileUnavailable) {
		t.Fatalf("err = %v, want ErrTurnstileUnavailable", err)
	}
}

func TestVerify_Timeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := guard.NewTurnstile(srv.Client(), srv.URL).Verify(ctx, "sek", "tok", "")
	if !errors.Is(err, guard.ErrTurnstileUnavailable) {
		t.Fatalf("err = %v, want ErrTurnstileUnavailable", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("did not honour the context deadline")
	}
}

func TestVerify_ErrorsDoNotLeakSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	srv.Close() // connection refused
	err := guard.NewTurnstile(nil, srv.URL).Verify(context.Background(), "TOPSECRET", "TOKEN123", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "TOPSECRET") || strings.Contains(err.Error(), "TOKEN123") {
		t.Fatalf("error leaks secret/token: %v", err)
	}
}
