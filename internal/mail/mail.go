// Package mail defines the provider interface and failover between providers.
package mail

import (
	"context"
	"errors"
	"fmt"
	"sync"

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

// SentBy is implemented by composite providers that report which backend
// delivered the last message.
type SentBy interface{ SentBy() string }

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
// A nil fallback returns primary unchanged.
func WithFallback(primary, fallback Provider) Provider {
	if fallback == nil {
		return primary
	}
	return &composite{primary: primary, fallback: fallback, last: primary.Name()}
}

type composite struct {
	primary, fallback Provider

	mu   sync.Mutex
	last string
}

// Name returns the name of the provider that sent last.
func (c *composite) Name() string { return c.SentBy() }

// SentBy returns the name of the provider that last sent successfully.
func (c *composite) SentBy() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *composite) record(name string) {
	c.mu.Lock()
	c.last = name
	c.mu.Unlock()
}

// Send tries the primary, then the fallback.
func (c *composite) Send(ctx context.Context, m Message) error {
	perr := c.primary.Send(ctx, m)
	if perr == nil {
		c.record(c.primary.Name())
		return nil
	}
	ferr := c.fallback.Send(ctx, m)
	if ferr == nil {
		c.record(c.fallback.Name())
		return nil
	}
	return fmt.Errorf("send failed via %s and %s: %w",
		c.primary.Name(), c.fallback.Name(), errors.Join(perr, ferr))
}
