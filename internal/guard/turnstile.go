package guard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"
	turnstileTimeout = 5 * time.Second
	maxVerifyBody    = 64 << 10
)

var (
	// ErrTurnstileRejected means the token is missing or failed verification.
	ErrTurnstileRejected = errors.New("turnstile: token rejected")
	// ErrTurnstileUnavailable means verification could not be completed.
	ErrTurnstileUnavailable = errors.New("turnstile: verification unavailable")
)

// Turnstile verifies Cloudflare Turnstile tokens.
type Turnstile struct {
	client *http.Client
	url    string
}

// NewTurnstile returns a verifier. A nil client gets a 5 s timeout client and
// an empty verifyURL means Cloudflare's siteverify endpoint.
func NewTurnstile(c *http.Client, verifyURL string) *Turnstile {
	if c == nil {
		c = &http.Client{Timeout: turnstileTimeout}
	}
	if verifyURL == "" {
		verifyURL = defaultVerifyURL
	}
	return &Turnstile{client: c, url: verifyURL}
}

// Verify checks token (for the visitor at ip, when known) against secret.
// Errors never contain the secret or the token.
func (t *Turnstile) Verify(ctx context.Context, secret, token, ip string) error {
	if strings.TrimSpace(token) == "" {
		return ErrTurnstileRejected
	}
	ctx, cancel := context.WithTimeout(ctx, turnstileTimeout)
	defer cancel()

	form := url.Values{"secret": {secret}, "response": {token}}
	if ip != "" {
		form.Set("remoteip", ip)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("%w: building request", ErrTurnstileUnavailable)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := t.client.Do(req)
	if err != nil {
		// url.Error carries only the endpoint URL and the cause, never the body.
		return fmt.Errorf("%w: %w", ErrTurnstileUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: status %d", ErrTurnstileUnavailable, resp.StatusCode)
	}

	var out struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(nil, resp.Body, maxVerifyBody)).Decode(&out); err != nil {
		return fmt.Errorf("%w: decoding response", ErrTurnstileUnavailable)
	}
	if !out.Success {
		return ErrTurnstileRejected
	}
	return nil
}
