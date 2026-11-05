package resend_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nahuelsantos/contact-api/internal/mail"
	"github.com/nahuelsantos/contact-api/internal/mail/resend"
)

var _ mail.Provider = (*resend.Provider)(nil)

func TestSend_Request(t *testing.T) {
	var gotBody map[string]any
	var gotMethod, gotPath, gotAuth, gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth, gotCT = r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer srv.Close()

	p := resend.New("resend", "key123", resend.WithBaseURL(srv.URL))
	if p.Name() != "resend" {
		t.Fatalf("Name = %q", p.Name())
	}
	msg := mail.Message{From: "a@x.io", To: []string{"b@x.io", "c@x.io"}, Subject: "Hi", HTML: "<p>h</p>", Text: "t", ReplyTo: "r@x.io"}
	if err := p.Send(context.Background(), msg); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/emails" {
		t.Fatalf("%s %s", gotMethod, gotPath)
	}
	if gotAuth != "Bearer key123" {
		t.Fatalf("auth = %q", gotAuth)
	}
	if !strings.HasPrefix(gotCT, "application/json") {
		t.Fatalf("content-type = %q", gotCT)
	}
	to, _ := gotBody["to"].([]any)
	if gotBody["from"] != "a@x.io" || gotBody["subject"] != "Hi" || gotBody["html"] != "<p>h</p>" ||
		gotBody["text"] != "t" || gotBody["reply_to"] != "r@x.io" || len(to) != 2 || to[0] != "b@x.io" {
		t.Fatalf("body = %v", gotBody)
	}
}

func TestSend_OmitsEmptyReplyTo(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
	}))
	defer srv.Close()
	p := resend.New("resend", "k", resend.WithBaseURL(srv.URL))
	if err := p.Send(context.Background(), mail.Message{From: "a@x.io", To: []string{"b@x.io"}, Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := gotBody["reply_to"]; ok {
		t.Fatalf("reply_to present: %v", gotBody)
	}
}

func TestSend_Non2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"invalid from"}`))
	}))
	defer srv.Close()
	p := resend.New("resend", "k", resend.WithBaseURL(srv.URL))
	err := p.Send(context.Background(), mail.Message{
		From: "a@x.io", To: []string{"secret-rcpt@x.io"}, Subject: "SECRET-SUBJECT", HTML: "SECRET-BODY", Text: "SECRET-BODY",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	if !strings.Contains(s, "422") {
		t.Fatalf("no status code: %s", s)
	}
	for _, bad := range []string{"SECRET-SUBJECT", "SECRET-BODY", "secret-rcpt"} {
		if strings.Contains(s, bad) {
			t.Fatalf("error leaks %q: %s", bad, s)
		}
	}
}

func TestSend_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	p := resend.New("resend", "k", resend.WithBaseURL(srv.URL), resend.WithHTTPClient(&http.Client{Timeout: 100 * time.Millisecond}))
	if err := p.Send(context.Background(), mail.Message{To: []string{"b@x.io"}}); err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestSend_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := resend.New("resend", "k", resend.WithBaseURL(srv.URL))
	if err := p.Send(ctx, mail.Message{To: []string{"b@x.io"}}); err == nil {
		t.Fatal("expected error")
	}
}
