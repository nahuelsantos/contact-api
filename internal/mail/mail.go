// Package mail defines the provider interface and failover between providers.
package mail

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/nahuelsantos/contact-api/internal/config"
)

// Message is one outgoing email.
type Message struct {
	From, ReplyTo string
	To            []string
	Subject       string
	HTML, Text    string
}

// Provider sends messages through one backend.
type Provider interface {
	Name() string
	Send(ctx context.Context, m Message) error
}

// Result describes one delivery attempt chain.
type Result struct {
	SentBy string   // provider that delivered; empty on failure
	Failed []string // providers that failed before success (or all, on error), in order tried
	Errors []error  // the error of each provider in Failed, same order
}

// SendError is a provider failure with a log-safe classification: the
// protocol stage that failed and the numeric reply (SMTP code or HTTP status,
// 0 when there is none). Err is the underlying error; it may carry server
// text, which can echo an address, so it is never logged; log ErrorClass.
type SendError struct {
	Stage string
	Code  int
	Err   error
}

func (e *SendError) Error() string { return e.Err.Error() }

func (e *SendError) Unwrap() error { return e.Err }

// ErrorClass returns a log-safe class for err, such as "rcpt_550",
// "status_502", "dial_timeout" or "canceled". It never includes server text
// or message content. It returns "" for nil and "error" when unclassified.
func ErrorClass(err error) string {
	if err == nil {
		return ""
	}
	suffix := ""
	var te interface{ Timeout() bool }
	switch {
	case errors.Is(err, context.Canceled):
		suffix = "canceled"
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &te) && te.Timeout():
		suffix = "timeout"
	}
	var se *SendError
	if !errors.As(err, &se) {
		if suffix == "" {
			return "error"
		}
		return suffix
	}
	class := se.Stage
	if se.Code != 0 {
		class += "_" + strconv.Itoa(se.Code)
	}
	if suffix != "" {
		class += "_" + suffix
	}
	return class
}

// Deliverer is implemented by providers that report per-call attribution.
type Deliverer interface {
	Deliver(ctx context.Context, m Message) (Result, error)
}

// Deliver sends m through p and reports which provider(s) were involved.
func Deliver(ctx context.Context, p Provider, m Message) (Result, error) {
	if d, ok := p.(Deliverer); ok {
		res, err := d.Deliver(ctx, m)
		if err != nil {
			return res, fmt.Errorf("deliver: %w", err)
		}
		return res, nil
	}
	if err := p.Send(ctx, m); err != nil {
		return Result{Failed: []string{p.Name()}, Errors: []error{err}}, fmt.Errorf("send via %s: %w", p.Name(), err)
	}
	return Result{SentBy: p.Name()}, nil
}

// Factory builds a Provider from its config.
type Factory func(name string, c config.ProviderConfig) (Provider, error)

// ErrUnknownProviderType is returned by Build for an unregistered provider type.
var ErrUnknownProviderType = errors.New("unknown provider type")

// Build constructs every configured provider using the factory registered for its type.
func Build(cfg map[string]config.ProviderConfig, factories map[string]Factory) (map[string]Provider, error) {
	out := make(map[string]Provider, len(cfg))
	for name, c := range cfg {
		f, ok := factories[c.Type]
		if !ok {
			return nil, fmt.Errorf("provider %q: %w %q", name, ErrUnknownProviderType, c.Type)
		}
		p, err := f(name, c)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", name, err)
		}
		out[name] = p
	}
	return out, nil
}

// WithFallback sends through primary and, if that fails, through fallback.
// primary must be non-nil. A nil fallback returns primary unchanged.
func WithFallback(primary, fallback Provider) Provider {
	if fallback == nil {
		return primary
	}
	return &composite{primary: primary, fallback: fallback}
}

type composite struct {
	primary, fallback Provider
}

// Name returns the primary provider's name.
func (c *composite) Name() string { return c.primary.Name() }

// Send delivers via Deliver and discards the attribution.
func (c *composite) Send(ctx context.Context, m Message) error {
	_, err := c.Deliver(ctx, m)
	return err
}

// Deliver tries the primary, then the fallback, reporting the outcome per call.
func (c *composite) Deliver(ctx context.Context, m Message) (Result, error) {
	perr := c.primary.Send(ctx, m)
	if perr == nil {
		return Result{SentBy: c.primary.Name()}, nil
	}
	failed := []string{c.primary.Name()}
	if ctx.Err() != nil {
		return Result{Failed: failed, Errors: []error{perr}}, fmt.Errorf("send failed via %s, fallback skipped: %w",
			c.primary.Name(), errors.Join(perr, ctx.Err()))
	}
	ferr := c.fallback.Send(ctx, m)
	if ferr == nil {
		return Result{SentBy: c.fallback.Name(), Failed: failed, Errors: []error{perr}}, nil
	}
	return Result{Failed: append(failed, c.fallback.Name()), Errors: []error{perr, ferr}},
		fmt.Errorf("send failed via %s and %s: %w", c.primary.Name(), c.fallback.Name(), errors.Join(perr, ferr))
}
