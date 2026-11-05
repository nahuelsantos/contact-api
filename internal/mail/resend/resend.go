// Package resend sends mail through the Resend HTTP API.
package resend

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/nahuelsantos/contact-api/internal/mail"
)

const (
	defaultBaseURL = "https://api.resend.com"
	defaultTimeout = 10 * time.Second
)

// ErrStatus is returned when Resend answers with a non-2xx status.
var ErrStatus = errors.New("resend: unexpected status")

// Provider implements mail.Provider over the Resend API.
type Provider struct {
	name, apiKey, baseURL string
	client                *http.Client
}

// Option customises a Provider.
type Option func(*Provider)

// WithBaseURL overrides the API base URL.
func WithBaseURL(u string) Option { return func(p *Provider) { p.baseURL = strings.TrimRight(u, "/") } }

// WithHTTPClient overrides the HTTP client.
func WithHTTPClient(c *http.Client) Option { return func(p *Provider) { p.client = c } }

// New returns a Resend provider with a 10 s default client timeout.
func New(name, apiKey string, opts ...Option) *Provider {
	p := &Provider{
		name:    name,
		apiKey:  apiKey,
		baseURL: defaultBaseURL,
		client:  &http.Client{Timeout: defaultTimeout},
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

// Name returns the configured provider name.
func (p *Provider) Name() string { return p.name }

type payload struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
	ReplyTo string   `json:"reply_to,omitempty"`
}

// Send posts the message to Resend. Errors never contain message content.
func (p *Provider) Send(ctx context.Context, m mail.Message) error {
	body, err := json.Marshal(payload{
		From: m.From, To: m.To, Subject: m.Subject, HTML: m.HTML, Text: m.Text, ReplyTo: m.ReplyTo,
	})
	if err != nil {
		return fmt.Errorf("resend: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/emails", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		// url.Error carries only method and URL, not the body.
		return &mail.SendError{Stage: "request", Err: fmt.Errorf("resend: send: %w", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &mail.SendError{
			Stage: "status", Code: resp.StatusCode,
			Err: fmt.Errorf("%w: %d", ErrStatus, resp.StatusCode),
		}
	}
	return nil
}
