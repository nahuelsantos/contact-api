// Package submit runs the submission flow: lookup, origin, honeypot, rate
// limit, Turnstile, validation, rendering, delivery and auto-reply.
package submit

import (
	"context"
	"log/slog"
	"strings"
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
	unknownLabel  = observability.UnknownLabel
	maxLoggedName = 64

	// Delivery is detached from the request: a closed tab or a dropped
	// upstream must not abort a send half-way (and skip the fallback). These
	// bound the whole chain; each provider keeps its own shorter timeout.
	notifyTimeout    = 25 * time.Second
	autoReplyTimeout = 20 * time.Second
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

// send describes a delivery attempt for logging. The error fields hold
// log-safe classes only (mail.ErrorClass), never provider or server text.
type send struct {
	provider        string
	duration        time.Duration
	errorClass      string // final failure class on send_failed
	providerErrors  string // "name:class" for every provider that failed
	autoReplyFailed bool
	autoReplyError  string
}

// providerErrors renders the failed providers of res as "name:class,...".
func providerErrors(res mail.Result) string {
	parts := make([]string, 0, len(res.Failed))
	for i, name := range res.Failed {
		var err error
		if i < len(res.Errors) {
			err = res.Errors[i]
		}
		class := mail.ErrorClass(err)
		if class == "" {
			class = "error"
		}
		parts = append(parts, name+":"+class)
	}

	return strings.Join(parts, ",")
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
		if err := s.Turnstile.Verify(ctx, site.TurnstileSecret, req.Fields[config.TurnstileField], req.ClientIP); err != nil {
			return s.finish(ctx, req, Result{Outcome: Forbidden}, nil)
		}
	}
	values, errs := forms.Validate(form, req.Fields)
	if len(errs) > 0 {
		return s.finish(ctx, req, Result{Outcome: Invalid, Fields: errs}, nil)
	}
	note, err := s.Renderer.Notification(site, form, values)
	if err != nil {
		return s.finish(ctx, req, Result{Outcome: SendFailed}, &send{errorClass: "render"})
	}
	var reply *render.Email
	replyErr := ""
	if form.AutoReply != nil {
		r, rerr := s.Renderer.AutoReply(site, form, values)
		if rerr != nil {
			replyErr = "render"
		} else {
			reply = &r
		}
	}

	prov := s.provider(site)
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), notifyTimeout)
	defer cancel()
	start := time.Now()
	res, err := mail.Deliver(sendCtx, prov, mail.Message{
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
	sd := &send{provider: label, duration: d, providerErrors: providerErrors(res)}
	if err != nil {
		if n := len(res.Errors); n > 0 {
			sd.errorClass = mail.ErrorClass(res.Errors[n-1])
		} else {
			sd.errorClass = mail.ErrorClass(err)
		}
		return s.finish(ctx, req, Result{Outcome: SendFailed}, sd)
	}

	switch {
	case reply != nil && values.FirstEmail() != "":
		replyErr = s.autoReply(ctx, prov, site, reply, values.FirstEmail())
	case reply != nil:
		replyErr = "no_email"
	}
	if replyErr != "" {
		s.Metrics.AutoReplyFailure(req.Site, req.Form)
		sd.autoReplyFailed = true
		sd.autoReplyError = replyErr
	}

	return s.finish(ctx, req, Result{Outcome: Sent}, sd)
}

// autoReply sends the confirmation, detached from the request like the
// notification, and returns the log-safe failure class ("" on success).
func (s *Service) autoReply(ctx context.Context, prov mail.Provider, site *config.Site, reply *render.Email, to string) string {
	actx, cancel := context.WithTimeout(context.WithoutCancel(ctx), autoReplyTimeout)
	defer cancel()
	res, err := mail.Deliver(actx, prov, mail.Message{
		From: site.From, To: []string{to},
		Subject: reply.Subject, HTML: reply.HTML, Text: reply.Text,
	})
	if err == nil {
		return ""
	}
	if pe := providerErrors(res); pe != "" {
		return pe
	}

	return mail.ErrorClass(err)
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
		if sd.provider != "" {
			attrs = append(attrs, "provider", sd.provider, "duration_ms", sd.duration.Milliseconds())
		}
		if sd.errorClass != "" {
			attrs = append(attrs, "error_class", sd.errorClass)
		}
		if sd.providerErrors != "" {
			attrs = append(attrs, "provider_errors", sd.providerErrors)
		}
		if sd.autoReplyFailed {
			attrs = append(attrs, "auto_reply_failed", true, "auto_reply_error", sd.autoReplyError)
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
