package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/email"
	"github.com/nahuelsantos/contact-api/internal/metrics"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// A submission that reaches the mail relay must (a) move the counters the
// scrape rules read, and (b) hand the relay the site's own sender with the
// visitor in Reply-To. Without the counters a form that accepts every message
// and then fails to relay it is indistinguishable from a healthy one.
func TestContactHandler_CountersAndSender(t *testing.T) {
	writer := &email.MockWriteCloser{}
	var envelopeFrom string

	originalDialer := email.DefaultSMTPDialer
	email.DefaultSMTPDialer = func(addr string) (email.SMTPClient, error) {
		return &email.MockSMTPClient{
			MailFunc: func(from string) error { envelopeFrom = from; return nil },
			DataFunc: func() (io.WriteCloser, error) { return writer, nil },
		}, nil
	}
	defer func() { email.DefaultSMTPDialer = originalDialer }()

	okBefore := testutil.ToFloat64(metrics.Submissions.WithLabelValues("main", "ok"))

	form := ContactFormData{
		Name:    "John Doe",
		Email:   "john@example.com",
		Subject: "Test Subject",
		Message: "Test message",
	}
	jsonData, err := json.Marshal(form)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}

	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), "POST", "/api/v1/contact/main", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setupTestAPI().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected status code %d, got %d (body: %s)", http.StatusOK, w.Code, w.Body.String())
	}

	if envelopeFrom != "test@example.com" {
		t.Errorf("envelope sender = %q, want the configured DEFAULT_FROM test@example.com", envelopeFrom)
	}
	msg := string(writer.Data)
	if !strings.Contains(msg, "From: test@example.com") {
		t.Errorf("From: header missing the configured sender\nmessage: %q", msg)
	}
	if !strings.Contains(msg, "Reply-To: john@example.com") {
		t.Errorf("Reply-To: header missing the visitor's address\nmessage: %q", msg)
	}

	after := testutil.ToFloat64(metrics.Submissions.WithLabelValues("main", "ok"))
	if after != okBefore+1 {
		t.Errorf("contact_form_submissions_total{result=ok} = %v, want %v", after, okBefore+1)
	}
}

// A rejected submission is counted too, by outcome and by the stage that
// rejected it, so an alert can tell a malformed client from a broken relay.
func TestContactHandler_InvalidCounted(t *testing.T) {
	before := testutil.ToFloat64(metrics.Submissions.WithLabelValues("main", "invalid"))

	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), "POST", "/api/v1/contact/main", bytes.NewBufferString("not json"))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	setupTestAPI().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
	}
	after := testutil.ToFloat64(metrics.Submissions.WithLabelValues("main", "invalid"))
	if after != before+1 {
		t.Errorf("contact_form_submissions_total{result=invalid} = %v, want %v", after, before+1)
	}
	if v := testutil.ToFloat64(metrics.Errors.WithLabelValues("main", "bind")); v < 1 {
		t.Errorf("contact_form_errors_total{stage=bind} = %v, want at least 1", v)
	}
}
