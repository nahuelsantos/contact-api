package email

import (
	"io"
	"strings"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/config"
)

// TestService_Send_Headers pins the message shape the relay depends on: the
// visitor's address belongs in Reply-To, never in From (a From claiming an
// arbitrary third-party domain fails SPF at the receiving end and is filed as
// spam), and the headers must be emitted in a stable order — the old map-based
// loop shuffled them on every send.
func TestService_Send_Headers(t *testing.T) {
	tests := []struct {
		name        string
		replyTo     string
		wantReplyTo bool
	}{
		{name: "Reply-To carried when set", replyTo: "visitor@example.com", wantReplyTo: true},
		{name: "No Reply-To header when unset", replyTo: "", wantReplyTo: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writer := &MockWriteCloser{}
			var envelopeFrom string
			dialer := func(addr string) (SMTPClient, error) {
				return &MockSMTPClient{
					MailFunc: func(from string) error { envelopeFrom = from; return nil },
					DataFunc: func() (io.WriteCloser, error) { return writer, nil },
				}, nil
			}

			cfg := config.Config{SMTPHost: "mail-server", SMTPPort: "25", DefaultFrom: "hi@example.com"}
			req := Request{
				From:    cfg.DefaultFrom,
				To:      "contact@example.com",
				Subject: "Test",
				Body:    "body",
				ReplyTo: tt.replyTo,
			}

			if err := NewService(dialer).Send(req, cfg); err != nil {
				t.Fatalf("Send returned an error: %v", err)
			}

			msg := string(writer.Data)
			if !strings.HasPrefix(msg, "From: hi@example.com\r\n") {
				t.Errorf("message must open with the From header, got %q", firstLine(msg))
			}
			if envelopeFrom != cfg.DefaultFrom {
				t.Errorf("envelope sender = %q, want %q", envelopeFrom, cfg.DefaultFrom)
			}
			if got := strings.Contains(msg, "Reply-To: "); got != tt.wantReplyTo {
				t.Errorf("Reply-To present = %v, want %v\nmessage: %q", got, tt.wantReplyTo, msg)
			}
			if tt.wantReplyTo && !strings.Contains(msg, "Reply-To: "+tt.replyTo) {
				t.Errorf("Reply-To header does not carry %q\nmessage: %q", tt.replyTo, msg)
			}
			if !strings.Contains(msg, "\r\n\r\nbody") {
				t.Errorf("body is not separated from the headers by a blank line\nmessage: %q", msg)
			}
		})
	}
}

func firstLine(s string) string {
	if i := strings.Index(s, "\r\n"); i >= 0 {
		return s[:i]
	}
	return s
}
