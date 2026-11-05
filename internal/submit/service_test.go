package submit_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/guard"
	"github.com/nahuelsantos/contact-api/internal/mail"
	"github.com/nahuelsantos/contact-api/internal/observability"
	"github.com/nahuelsantos/contact-api/internal/render"
	"github.com/nahuelsantos/contact-api/internal/submit"
	"github.com/prometheus/client_golang/prometheus"
)

var errBoom = errors.New("boom")

type fakeProvider struct {
	name string
	err  error
	mu   sync.Mutex
	sent []mail.Message
}

func (f *fakeProvider) Name() string { return f.name }

func (f *fakeProvider) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)

	return f.err
}

func (f *fakeProvider) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.sent)
}

type fakeVerifier struct {
	err   error
	calls int
}

func (v *fakeVerifier) Verify(context.Context, string, string, string) error {
	v.calls++

	return v.err
}

type env struct {
	svc      *submit.Service
	primary  *fakeProvider
	fallback *fakeProvider
	verifier *fakeVerifier
	reg      *prometheus.Registry
	logs     *bytes.Buffer
	now      time.Time
	cfg      *config.Config
}

func newEnv(t *testing.T, mutate func(*config.Config)) *env {
	t.Helper()
	cfg := &config.Config{
		Server:   config.Server{MaxBodyKB: 64},
		Defaults: config.Defaults{Provider: "main", Fallback: "", Rate: config.Rate{Count: 5, Per: config.Duration(10 * time.Minute)}},
		Sites: map[string]*config.Site{
			"example": {
				Name: "example", From: "Example <noreply@example.com>", To: []string{"inbox@example.com"},
				Origins: []string{"https://example.com"},
				Forms: map[string]*config.Form{
					"contact": {Name: "contact", Fields: []config.Field{
						{Name: "name", Type: "text", Required: true, MaxLen: 500},
						{Name: "email", Type: "email", Required: true, MaxLen: 500},
						{Name: "message", Type: "textarea", Required: true, MaxLen: 10000},
					}},
				},
			},
		},
	}
	if mutate != nil {
		mutate(cfg)
	}
	r, err := render.New("")
	if err != nil {
		t.Fatal(err)
	}
	e := &env{
		primary: &fakeProvider{name: "main"}, fallback: &fakeProvider{name: "backup"},
		verifier: &fakeVerifier{}, reg: prometheus.NewRegistry(), logs: &bytes.Buffer{},
		now: time.Unix(1_700_000_000, 0), cfg: cfg,
	}
	e.svc = &submit.Service{
		Config:    cfg,
		Providers: map[string]mail.Provider{"main": e.primary, "backup": e.fallback},
		Renderer:  r,
		Limiter:   guard.NewLimiter(func() time.Time { return e.now }),
		Turnstile: e.verifier,
		Metrics:   observability.NewMetrics(e.reg),
		Log:       slog.New(slog.NewJSONHandler(e.logs, nil)),
	}

	return e
}

func (e *env) counter(t *testing.T, name string, labels map[string]string) float64 {
	t.Helper()
	fams, err := e.reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fams {
		if f.GetName() != name {
			continue
		}
		for _, m := range f.GetMetric() {
			if len(m.GetLabel()) != len(labels) {
				continue
			}
			match := true
			for _, lp := range m.GetLabel() {
				if labels[lp.GetName()] != lp.GetValue() {
					match = false
				}
			}
			if match {
				return m.GetCounter().GetValue()
			}
		}
	}

	return 0
}

func req() submit.Request {
	return submit.Request{
		Site: "example", Form: "contact", Origin: "https://example.com", ClientIP: "203.0.113.9",
		Fields: map[string]string{"name": "Ana", "email": "ana@example.com", "message": "Hola"},
	}
}

func withAutoReply(c *config.Config) {
	c.Sites["example"].Forms["contact"].AutoReply = &config.AutoReply{Subject: "Thanks", Body: "Hi {{name}}"}
}

func TestSubmit_Sent(t *testing.T) {
	e := newEnv(t, nil)
	res := e.svc.Submit(t.Context(), req())
	if res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if e.primary.count() != 1 {
		t.Fatalf("sent = %d", e.primary.count())
	}
	m := e.primary.sent[0]
	if m.From != "Example <noreply@example.com>" || m.ReplyTo != "ana@example.com" ||
		len(m.To) != 1 || m.To[0] != "inbox@example.com" {
		t.Fatalf("message = %+v", m)
	}
	if got := e.counter(t, "contact_submissions_total", map[string]string{"site": "example", "form": "contact", "outcome": "sent"}); got != 1 {
		t.Fatalf("metric = %v", got)
	}
}

func TestSubmit_UnknownSiteAndUnknownFormSameResult(t *testing.T) {
	e := newEnv(t, nil)
	a := req()
	a.Site = "nope"
	b := req()
	b.Form = "nope"
	if ra, rb := e.svc.Submit(t.Context(), a), e.svc.Submit(t.Context(), b); ra.Outcome != submit.NotFound || rb.Outcome != submit.NotFound {
		t.Fatalf("got %s, %s", ra.Outcome, rb.Outcome)
	}
	if got := e.counter(t, "contact_submissions_total", map[string]string{"site": "unknown", "form": "unknown", "outcome": "not_found"}); got != 2 {
		t.Fatalf("metric = %v", got)
	}
}

func TestSubmit_OriginRejected(t *testing.T) {
	e := newEnv(t, nil)
	r := req()
	r.Origin = "https://evil.example"
	if res := e.svc.Submit(t.Context(), r); res.Outcome != submit.Forbidden {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if e.primary.count() != 0 {
		t.Fatal("sent mail")
	}
}

func TestSubmit_HoneypotFakesSuccess(t *testing.T) {
	e := newEnv(t, nil)
	r := req()
	r.Fields["_gotcha"] = "bot"
	if res := e.svc.Submit(t.Context(), r); res.Outcome != submit.Spam {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if e.primary.count() != 0 {
		t.Fatal("sent mail")
	}
}

func TestSubmit_RateLimited(t *testing.T) {
	e := newEnv(t, nil)
	for i := range 5 {
		if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
			t.Fatalf("call %d outcome = %s", i, res.Outcome)
		}
	}
	res := e.svc.Submit(t.Context(), req())
	if res.Outcome != submit.RateLimited || res.RetryAfter <= 0 {
		t.Fatalf("res = %+v", res)
	}
}

func TestSubmit_TurnstileRejectedAndUnavailable(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Sites["example"].TurnstileSecret = "s3cret" })
	for _, verr := range []error{guard.ErrTurnstileRejected, guard.ErrTurnstileUnavailable} {
		e.verifier.err = verr
		if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Forbidden {
			t.Fatalf("%v: outcome = %s", verr, res.Outcome)
		}
	}
	if e.primary.count() != 0 {
		t.Fatal("sent mail")
	}

	open := newEnv(t, nil)
	if res := open.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if open.verifier.calls != 0 {
		t.Fatal("verifier called for site without secret")
	}
}

func TestSubmit_Invalid(t *testing.T) {
	e := newEnv(t, nil)
	r := req()
	r.Fields["email"] = "not-an-email"
	delete(r.Fields, "name")
	res := e.svc.Submit(t.Context(), r)
	if res.Outcome != submit.Invalid || res.Fields["email"] != "invalid_email" || res.Fields["name"] != "required" {
		t.Fatalf("res = %+v", res)
	}
	if e.primary.count() != 0 {
		t.Fatal("sent mail")
	}
}

func TestSubmit_FallbackUsed(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Defaults.Fallback = "backup" })
	e.primary.err = errBoom
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if e.fallback.count() != 1 {
		t.Fatalf("fallback sent = %d", e.fallback.count())
	}
	if got := e.counter(t, "contact_provider_failures_total", map[string]string{"provider": "main"}); got != 1 {
		t.Fatalf("failure metric = %v", got)
	}
}

func TestSubmit_AllProvidersFail(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Defaults.Fallback = "backup"; withAutoReply(c) })
	e.primary.err, e.fallback.err = errBoom, errBoom
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.SendFailed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if e.primary.count() != 1 || e.fallback.count() != 1 {
		t.Fatalf("attempts = %d, %d (auto-reply must not be sent)", e.primary.count(), e.fallback.count())
	}
	if !strings.Contains(e.logs.String(), `"level":"WARN"`) {
		t.Fatalf("expected WARN log: %s", e.logs)
	}
}

func TestSubmit_AutoReplyAfterNotification(t *testing.T) {
	e := newEnv(t, withAutoReply)
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if e.primary.count() != 2 {
		t.Fatalf("sent = %d", e.primary.count())
	}
	ar := e.primary.sent[1]
	if len(ar.To) != 1 || ar.To[0] != "ana@example.com" || ar.From != "Example <noreply@example.com>" || ar.ReplyTo != "" {
		t.Fatalf("auto-reply = %+v", ar)
	}
}

func TestSubmit_AutoReplyFailureStillSent(t *testing.T) {
	e := newEnv(t, withAutoReply)
	e.primary.err = nil
	failSecond := &secondFails{fakeProvider: e.primary}
	e.svc.Providers["main"] = failSecond
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	if got := e.counter(t, "contact_autoreply_failures_total", map[string]string{"site": "example", "form": "contact"}); got != 1 {
		t.Fatalf("metric = %v", got)
	}
}

type secondFails struct{ *fakeProvider }

func (s *secondFails) Send(ctx context.Context, m mail.Message) error {
	_ = s.fakeProvider.Send(ctx, m)
	if s.count() >= 2 {
		return errBoom
	}

	return nil
}

func TestSubmit_LogsNoFieldValues(t *testing.T) {
	e := newEnv(t, withAutoReply)
	r := req()
	markers := []string{"MARK-NAME-7731", "mark-mail-7731@leak.test", "MARK-MSG-7731"}
	r.Fields = map[string]string{"name": markers[0], "email": markers[1], "message": markers[2], "extra": "MARK-EXTRA-7731"}
	calls := 0
	submitOnce := func(rq submit.Request) submit.Outcome {
		calls++
		return e.svc.Submit(t.Context(), rq).Outcome
	}

	if o := submitOnce(r); o != submit.Sent {
		t.Fatalf("sent: %s", o)
	}
	// A provider error that echoes the visitor address, as an SMTP RCPT
	// rejection can, must not reach the log.
	e.primary.err = &mail.SendError{Stage: "rcpt", Code: 550, Err: errors.New("550 <" + markers[1] + "> MARK-SERVER-7731")}
	if o := submitOnce(r); o != submit.SendFailed {
		t.Fatalf("send_failed: %s", o)
	}
	e.primary.err = errors.New("plain error " + markers[0])
	if o := submitOnce(r); o != submit.SendFailed {
		t.Fatalf("send_failed plain: %s", o)
	}
	e.primary.err = nil
	bad := req()
	bad.Fields = map[string]string{"name": markers[0], "email": "MARK-BAD-7731", "message": markers[2]}
	if o := submitOnce(bad); o != submit.Invalid {
		t.Fatalf("invalid: %s", o)
	}
	evil := r
	evil.Origin = "https://evil.example"
	if o := submitOnce(evil); o != submit.Forbidden {
		t.Fatalf("forbidden: %s", o)
	}
	spam := req()
	spam.Fields = map[string]string{"name": markers[0], "email": markers[1], "message": markers[2], "_gotcha": "MARK-GOTCHA-7731"}
	if o := submitOnce(spam); o != submit.Spam {
		t.Fatalf("spam: %s", o)
	}
	for o := submit.Outcome(""); o != submit.RateLimited; {
		if calls > 20 {
			t.Fatal("never rate limited")
		}
		o = submitOnce(r)
	}

	out := e.logs.String()
	for _, m := range append(markers, "MARK-EXTRA-7731", "MARK-BAD-7731", "MARK-SERVER-7731", "MARK-GOTCHA-7731") {
		if strings.Contains(out, m) {
			t.Fatalf("log contains %q: %s", m, out)
		}
	}
	lines := logLines(t, e)
	if len(lines) != calls {
		t.Fatalf("%d log lines for %d submissions:\n%s", len(lines), calls, out)
	}
	for _, l := range lines {
		for _, k := range []string{"site", "form", "outcome"} {
			if _, ok := l[k]; !ok {
				t.Fatalf("log line lacks %s: %v", k, l)
			}
		}
	}
}

func logLines(t *testing.T, e *env) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(e.logs.String()), "\n") {
		if raw == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(raw), &m); err != nil {
			t.Fatalf("bad log line %q: %v", raw, err)
		}
		out = append(out, m)
	}

	return out
}

func lastLine(t *testing.T, e *env) map[string]any {
	t.Helper()
	lines := logLines(t, e)
	if len(lines) == 0 {
		t.Fatal("no log line")
	}

	return lines[len(lines)-1]
}

func TestSubmit_SendFailedLogsErrorClass(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Defaults.Fallback = "backup" })
	e.primary.err = &mail.SendError{Stage: "rcpt", Code: 550, Err: errors.New("550 ana@example.com unknown")}
	e.fallback.err = &mail.SendError{Stage: "status", Code: 502, Err: errBoom}
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.SendFailed {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	l := lastLine(t, e)
	if l["error_class"] != "status_502" || l["provider_errors"] != "main:rcpt_550,backup:status_502" {
		t.Fatalf("log line = %v", l)
	}
	if strings.Contains(e.logs.String(), "ana@example.com") {
		t.Fatalf("server text leaked: %s", e.logs)
	}
}

func TestSubmit_FallbackSuccessLogsPrimaryErrorClass(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Defaults.Fallback = "backup" })
	e.primary.err = &mail.SendError{Stage: "dial", Err: context.DeadlineExceeded}
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	l := lastLine(t, e)
	if l["provider_errors"] != "main:dial_timeout" || l["provider"] != "backup" {
		t.Fatalf("log line = %v", l)
	}
	if _, ok := l["error_class"]; ok {
		t.Fatalf("error_class on a sent submission: %v", l)
	}
}

func TestSubmit_AutoReplyFailureLogsErrorClass(t *testing.T) {
	e := newEnv(t, withAutoReply)
	e.svc.Providers["main"] = &secondFails{fakeProvider: e.primary}
	if res := e.svc.Submit(t.Context(), req()); res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	l := lastLine(t, e)
	if l["auto_reply_failed"] != true || l["auto_reply_error"] != "main:error" {
		t.Fatalf("log line = %v", l)
	}
}

// blockingProvider holds every send until released, then fails if its
// context was cancelled, as a real provider would.
type blockingProvider struct {
	name     string
	started  chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	sent     int
	deadline []bool
}

func (b *blockingProvider) Name() string { return b.name }

func (b *blockingProvider) Send(ctx context.Context, _ mail.Message) error {
	b.once.Do(func() { close(b.started) })
	<-b.release
	if err := ctx.Err(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sent++
	_, ok := ctx.Deadline()
	b.deadline = append(b.deadline, ok)

	return nil
}

func TestSubmit_DeliveryOutlivesRequestCancellation(t *testing.T) {
	e := newEnv(t, withAutoReply)
	bp := &blockingProvider{name: "main", started: make(chan struct{}), release: make(chan struct{})}
	e.svc.Providers["main"] = bp

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan submit.Result, 1)
	go func() { done <- e.svc.Submit(ctx, req()) }()
	<-bp.started
	cancel() // the visitor closed the tab
	close(bp.release)

	if res := <-done; res.Outcome != submit.Sent {
		t.Fatalf("outcome = %s", res.Outcome)
	}
	bp.mu.Lock()
	defer bp.mu.Unlock()
	if bp.sent != 2 {
		t.Fatalf("sent = %d, want notification and auto-reply", bp.sent)
	}
	for i, ok := range bp.deadline {
		if !ok {
			t.Fatalf("send %d had no deadline: delivery must stay bounded", i)
		}
	}
	if got := e.counter(t, "contact_provider_failures_total", map[string]string{"provider": "main"}); got != 0 {
		t.Fatalf("provider failure counted: %v", got)
	}
}

func TestOutcomesMatchPreinitialisedMetrics(t *testing.T) {
	got := []string{
		string(submit.Sent), string(submit.Spam), string(submit.Invalid),
		string(submit.Forbidden), string(submit.RateLimited), string(submit.SendFailed),
	}
	if !slices.Equal(got, observability.SubmissionOutcomes) {
		t.Fatalf("outcomes %v != observability.SubmissionOutcomes %v", got, observability.SubmissionOutcomes)
	}
	if string(submit.NotFound) != observability.NotFoundOutcome {
		t.Fatalf("not found outcome %q != %q", submit.NotFound, observability.NotFoundOutcome)
	}
}
