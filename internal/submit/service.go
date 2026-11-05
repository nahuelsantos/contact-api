// Package submit runs the submission flow: lookup, origin, honeypot, rate
// limit, Turnstile, validation, rendering, delivery and auto-reply.
package submit

import (
	"context"
	"log/slog"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/forms"
	"github.com/nahuelsantos/contact-api/internal/guard"
	"github.com/nahuelsantos/contact-api/internal/mail"
	"github.com/nahuelsantos/contact-api/internal/observability"
	"github.com/nahuelsantos/contact-api/internal/render"
)

// Outcome is how a submission ended.
type Outcome string

// Submission outcomes; the string values are metric label values.
const (
	Sent        Outcome = "sent"
	Spam        Outcome = "spam"
	Invalid     Outcome = "invalid"
	Forbidden   Outcome = "forbidden"
	RateLimited Outcome = "rate_limited"
	SendFailed  Outcome = "send_failed"
	NotFound    Outcome = "not_found"
)

const (
	turnstileField = "cf-turnstile-response"
	unknownLabel   = "unknown"
	maxLoggedName  = 64
)

// Request is one submission, already parsed by the transport layer.
type Request struct {
	Site, Form, Origin, ClientIP string
	Fields                       map[string]string
}

// Result is the outcome of Submit plus what the transport needs to answer.
type Result struct {
	Outcome    Outcome
	Fields     forms.FieldErrors
	RetryAfter time.Duration
}

// Verifier checks a Turnstile token.
type Verifier interface {
	Verify(ctx context.Context, secret, token, ip string) error
}

// Service orchestrates a submission.
type Service struct {
	Config    *config.Config
	Providers map[string]mail.Provider
	Renderer  *render.Renderer
	Limiter   *guard.Limiter
	Turnstile Verifier
	Metrics   *observability.Metrics
	Log       *slog.Logger
}

// send describes a delivery attempt for logging.
type send struct {
	provider        string
	duration        time.Duration
	autoReplyFailed bool
}

// Submit runs the request flow and records one metric and one log line.
func (s *Service) Submit(ctx context.Context, req Request) Result {
	site, form, ok := s.Config.Lookup(req.Site, req.Form)
	if !ok {
		return s.finish(ctx, req, Result{Outcome: NotFound}, nil)
	}
	if !guard.OriginAllowed(req.Origin, site.Origins) {
		return s.finish(ctx, req, Result{Outcome: Forbidden}, nil)
	}
	if guard.Honeypot(req.Fields) {
		return s.finish(ctx, req, Result{Outcome: Spam}, nil)
	}
	key := req.ClientIP + "|" + req.Site + "|" + req.Form
	if allowed, wait := s.Limiter.Allow(key, s.Config.RateFor(form)); !allowed {
		return s.finish(ctx, req, Result{Outcome: RateLimited, RetryAfter: wait}, nil)
	}
	if site.TurnstileSecret != "" {
		if err := s.Turnstile.Verify(ctx, site.TurnstileSecret, req.Fields[turnstileField], req.ClientIP); err != nil {
			return s.finish(ctx, req, Result{Outcome: Forbidden}, nil)
		}
	}
	values, errs := forms.Validate(form, req.Fields)
	if len(errs) > 0 {
		return s.finish(ctx, req, Result{Outcome: Invalid, Fields: errs}, nil)
	}
	note, err := s.Renderer.Notification(site, form, values)
	if err != nil {
		return s.finish(ctx, req, Result{Outcome: SendFailed}, nil)
	}
	var reply *render.Email
	if form.AutoReply != nil {
		r, rerr := s.Renderer.AutoReply(site, form, values)
		if rerr != nil {
			s.Metrics.AutoReplyFailure(req.Site, req.Form)
		} else {
			reply = &r
		}
	}

	prov := s.provider(site)
	start := time.Now()
	res, err := mail.Deliver(ctx, prov, mail.Message{
		From: site.From, ReplyTo: values.FirstEmail(), To: site.Recipients(form),
		Subject: note.Subject, HTML: note.HTML, Text: note.Text,
	})
	d := time.Since(start)
	for _, name := range res.Failed {
		s.Metrics.ProviderFailure(name)
	}
	label := res.SentBy
	if label == "" {
		label = prov.Name()
		if n := len(res.Failed); n > 0 {
			label = res.Failed[n-1]
		}
	}
	s.Metrics.SendDuration(label, d)
	sd := &send{provider: label, duration: d}
	if err != nil {
		return s.finish(ctx, req, Result{Outcome: SendFailed}, sd)
	}

	if reply != nil && values.FirstEmail() != "" {
		if _, aerr := mail.Deliver(ctx, prov, mail.Message{
			From: site.From, To: []string{values.FirstEmail()},
			Subject: reply.Subject, HTML: reply.HTML, Text: reply.Text,
		}); aerr != nil {
			s.Metrics.AutoReplyFailure(req.Site, req.Form)
			sd.autoReplyFailed = true
		}
	} else if reply != nil {
		s.Metrics.AutoReplyFailure(req.Site, req.Form)
		sd.autoReplyFailed = true
	}

	return s.finish(ctx, req, Result{Outcome: Sent}, sd)
}

// provider composes the site's primary and fallback providers.
func (s *Service) provider(site *config.Site) mail.Provider {
	primary, fallback := s.Config.ProvidersFor(site)
	var fb mail.Provider
	if fallback != "" {
		fb = s.Providers[fallback]
	}

	return mail.WithFallback(s.Providers[primary], fb)
}

// finish records the metric and the single log line for a submission.
func (s *Service) finish(ctx context.Context, req Request, res Result, sd *send) Result {
	site, form := req.Site, req.Form
	if res.Outcome == NotFound {
		s.Metrics.Submission(unknownLabel, unknownLabel, string(res.Outcome))
		site, form = truncate(site), truncate(form)
	} else {
		s.Metrics.Submission(site, form, string(res.Outcome))
	}

	attrs := []any{"site", site, "form", form, "outcome", string(res.Outcome)}
	if sd != nil {
		attrs = append(attrs, "provider", sd.provider, "duration_ms", sd.duration.Milliseconds())
		if sd.autoReplyFailed {
			attrs = append(attrs, "auto_reply_failed", true)
		}
	}
	if res.Outcome == Forbidden || res.Outcome == RateLimited {
		attrs = append(attrs, "client_ip", req.ClientIP)
	}
	level := slog.LevelInfo
	if res.Outcome == SendFailed {
		level = slog.LevelWarn
	}
	s.Log.Log(ctx, level, "submission", attrs...)

	return res
}

func truncate(v string) string {
	if r := []rune(v); len(r) > maxLoggedName {
		return string(r[:maxLoggedName])
	}

	return v
}
