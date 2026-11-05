# contact-api v2 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn contact-api into one config-driven forms service for every homelab site, with Resend + SMTP providers, spam guards, and no field values in logs — while the two live v1 contact forms keep working unchanged.

**Architecture:** One stateless Go binary. Pure packages (`config`, `forms`, `render`, `guard`) feed an orchestrating `submit.Service`; `mail` providers sit behind one interface; Gin handlers stay thin. Deployed like Wisp: CI on plunder runners → Zot → melee deploy gate.

**Tech Stack:** Go 1.26, Gin, `github.com/pelletier/go-toml/v2`, `golang.org/x/time/rate`, `github.com/prometheus/client_golang`, OpenTelemetry (existing), `github.com/emersion/go-smtp` (tests only).

**Spec:** `docs/superpowers/specs/2026-11-05-contact-api-v2-design.md` (this repo). Tasks 12–13 change `nahuelsantos/home-lab`.

## Global Constraints

- Module path stays `github.com/nahuelsantos/contact-api`; `go 1.26` in `go.mod`.
- `golangci-lint run ./...` (existing `.golangci.yml`) and `go test -race ./...` pass at the end of every task.
- **No form field value** (names, emails, messages, any submitted string) in logs, trace attributes or metric labels. Logs: site, form, outcome, provider, duration, request id; client IP only for 403/429.
- Config is TOML at `CONFIG_FILE` (default `/etc/contact-api/forms.toml`); secrets only via `*_env` names. Unknown keys are errors.
- Defaults: `server.port = 3002`, `server.max_body_kb = 64`, field `max_len` 500 (`textarea` 10000), rate `{count = 5, per = "10m"}`.
- Timeouts: Resend HTTP 10 s, SMTP dial+send 10 s, Turnstile 5 s; HTTP server WriteTimeout stays 30 s (worst case 5 + 10 + 10 fits).
- Error codes (exact strings): `required`, `too_long`, `invalid_email`, `invalid_option`, `invalid_number`, `invalid_country`, `invalid_format`.
- v2 response bodies (exact): `{"ok":true}`; `{"ok":false,"error":"forbidden"|"not_found"|"too_large"|"unsupported_media_type"|"rate_limited"|"send_failed"}`; `{"ok":false,"error":"invalid","fields":{...}}`.
- Reserved fields never forwarded: `_gotcha`, `cf-turnstile-response`. Field names starting with `_` are invalid in config.
- Deploy conventions from `nahuelsantos/site-template`: `GET /healthz` → body `ok`; `GET /version.json` → `{"sha":"<git sha>"}` (this replaces the spec's `{version, commit}` shape so the shared CD smoke works unchanged); image has a `HEALTHCHECK`; build arg `GIT_SHA`.
- Commit messages end with the session's `Co-Authored-By` / `Claude-Session` lines.

## Review Focus

1. **Accented and non-ASCII input** (`ñ`, `á`, emoji) in names, subjects and messages must arrive intact through both providers; SMTP subjects need RFC 2047. → tests in Task 3 (`TestNotification_Accents`) and Task 5 (`TestBuildMIME_EncodesNonASCIISubject`).
2. **Messy emails** (`" Juan@Mail.com "`, `"Juan <j@x.com>"`): trim and accept a bare address; reject a display-name form, because it ends up in `Reply-To`. → Task 2 (`TestValidate_EmailTrimmedAndBareOnly`).
3. **Plain HTML forms without JavaScript**: urlencoded body, an unchecked checkbox simply absent, a field sent twice (first value wins). → Task 9 (`TestV2_URLEncodedForm`).
4. **Client IP spoofing**: `X-Forwarded-For` must never pick the rate-limit key; only `CF-Connecting-IP`, else the TCP remote address. → Task 7 (`TestClientIP_IgnoresXForwardedFor`).
5. **Windows line breaks and header injection** in textareas and subject placeholders: `\r\n` kept as line breaks in bodies, stripped in headers. → Task 3 (`TestSubject_StripsCRLF`, `TestNotification_PreservesLineBreaks`).

---

## File Structure

```
cmd/contact-api/main.go          composition root (package main): load config, build providers, router, serve
config/example.toml              valid config used by the image smoke test and as documentation
internal/config/config.go        types + Load (parse, resolve env, defaults)
internal/config/validate.go      every startup rule from spec §3.2
internal/config/placeholders.go  {{field}} parsing shared by validate + render
internal/forms/validate.go       Validate(form, input) → Values, FieldErrors
internal/forms/countries.go      ISO 3166-1 alpha-2 set
internal/render/render.go        Renderer, Notification, AutoReply, Subject
internal/render/templates/       default.html.tmpl, default.txt.tmpl, autoreply.html.tmpl (embedded)
internal/mail/mail.go            Message, Provider, failover, Build
internal/mail/smtp/smtp.go       SMTP provider + BuildMIME
internal/mail/resend/resend.go   Resend provider
internal/guard/{origin,honeypot,ratelimit,turnstile,clientip}.go
internal/observability/metrics.go
internal/submit/service.go       the request flow (spec §5 steps 2–10)
internal/handlers/router.go      routes, CORS, body cap, content types, v1 shim, health, version
scripts/image-smoke.sh           CI image test
scripts/smoke.sh                 post-deploy real-email smoke
.github/workflows/ci.yml, cd-main.yml   (from site-template)
```

Deleted: `main.go` (root), `docs/` (swagger), `internal/email/`, `internal/handlers/api*.go`, `.github/workflows/release.yml`, `docker-compose.prod.yml`, `scripts/health_check.sh`, swaggo dependencies.

---

### Task 1: Config — types, loading, defaults

**Files:**
- Create: `internal/config/config.go`, `internal/config/placeholders.go`, `internal/config/config_test.go` (replace the old test), `config/example.toml`
- Modify: `go.mod` (`go 1.26`; add `github.com/pelletier/go-toml/v2` as a direct dependency)

**Interfaces:**
- Produces:
  ```go
  type Duration time.Duration                    // UnmarshalText parses "10m", "1h"
  type Rate struct { Count int `toml:"count"`; Per Duration `toml:"per"` }
  type Server struct { Port int; MaxBodyKB int `toml:"max_body_kb"` }
  type Defaults struct { Provider, Fallback string; Rate Rate }
  type ProviderConfig struct {
      Type string; Host string; Port int; TLS string            // smtp: tls = none|starttls|implicit
      UsernameEnv string `toml:"username_env"`; PasswordEnv string `toml:"password_env"`
      APIKeyEnv string `toml:"api_key_env"`
      Username, Password, APIKey string `toml:"-"`               // resolved from env
  }
  type Field struct {
      Name, Label, Type, Pattern string; Required bool
      MaxLen int `toml:"max_len"`; Options []string
      Re *regexp.Regexp `toml:"-"`
  }
  type AutoReply struct { Subject, Body string }
  type Form struct {
      Name string `toml:"-"`; To []string; Subject string; Rate *Rate; Template string
      AutoReply *AutoReply `toml:"auto_reply"`; Fields []Field
  }
  type Site struct {
      Name string `toml:"-"`; From string; To []string; Origins []string
      Provider, Fallback string
      TurnstileSecretEnv string `toml:"turnstile_secret_env"`; TurnstileSecret string `toml:"-"`
      Forms map[string]*Form `toml:"form"`
  }
  type Config struct {
      Server Server; Defaults Defaults
      Providers map[string]ProviderConfig `toml:"provider"`
      Sites map[string]*Site `toml:"site"`
  }
  func Load(path string, getenv func(string) string, templateExists func(string) bool) (*Config, error)
  func (c *Config) Lookup(site, form string) (*Site, *Form, bool)
  func (c *Config) RateFor(f *Form) Rate                    // form rate else defaults.rate
  func (c *Config) ProvidersFor(s *Site) (primary, fallback string) // site overrides, else defaults
  func (s *Site) Recipients(f *Form) []string               // form.To if set, else site.To
  func Placeholders(s string) []string                      // names inside {{ name }}
  ```

- [ ] **Step 1: Write the failing tests** in `internal/config/config_test.go`:
  - `TestLoad_Example`: `Load("../../config/example.toml", envMap{...}, alwaysTrue)` returns no error; `cfg.Server.Port == 3002`; `cfg.Server.MaxBodyKB == 64`; `Lookup("example","contact")` found; field `message` has `MaxLen == 10000`, field `name` `MaxLen == 500`; `cfg.RateFor(form) == Rate{Count: 5, Per: Duration(10*time.Minute)}`.
  - `TestLoad_FormRateOverrides`: a form with `rate = { count = 3, per = "1h" }` → `RateFor` returns `{3, 1h}`.
  - `TestLoad_ResolvesSecrets`: `api_key_env = "RESEND_API_KEY"` with env `RESEND_API_KEY=k` → `Providers["resend"].APIKey == "k"`; same for `turnstile_secret_env`.
  - `TestRecipients_FormOverridesSite`, `TestProvidersFor_SiteOverridesDefaults`.
  - `TestPlaceholders`: `Placeholders("Hola {{nombre}} {{ apellido }}")` → `["nombre","apellido"]`.
- [ ] **Step 2:** `go test ./internal/config/...` → FAIL (undefined).
- [ ] **Step 3: Implement** `config.go` and `placeholders.go`. Decode with `toml.NewDecoder(f).DisallowUnknownFields()`; fill `Name` fields from map keys; apply defaults; compile `Pattern` into `Re`. Placeholder regexp: `\{\{\s*([a-z0-9_]+)\s*\}\}`. Write `config/example.toml`: `[provider.melee]` smtp `mail-server:25` `tls="none"`, `[defaults] provider = "melee"`, `[site.example]` with `from`, `to = ["owner@example.com"]`, `origins = ["https://example.com"]`, and `[site.example.form.contact]` with the four v0 fields (`name`, `email`, `subject`, `message`; types text/email/text/textarea; all required). Validation is Task 1b — `Load` calls `validate(cfg, getenv, templateExists)` which for now returns nil.
- [ ] **Step 4:** `go test ./internal/config/...` → PASS.
- [ ] **Step 5: Commit** `feat(config): TOML forms config with defaults and env secrets`.

### Task 1b: Config — startup validation

**Files:**
- Create: `internal/config/validate.go`, `internal/config/validate_test.go`

**Interfaces:**
- Consumes: Task 1 types.
- Produces: `func validate(c *Config, getenv func(string) string, templateExists func(string) bool) error` — returns `errors.Join` of **all** problems; each message names the site/form/field/provider involved.

- [ ] **Step 1: Write failing table test** `TestLoad_RejectsInvalid`, one case per rule, each asserting `err` contains the quoted substring:
  | Case | Expect substring |
  |---|---|
  | unknown key `[site.x] colour = "red"` | `colour` |
  | `api_key_env` whose variable is empty | `RESEND_API_KEY is empty` |
  | `defaults.provider = "nope"` | `unknown provider "nope"` |
  | site `fallback = "nope"` | `unknown provider "nope"` |
  | site without `from` | `site "x": from is required` |
  | site and form both without `to` | `site "x" form "f": no recipients` |
  | site without `origins` | `site "x": origins is required` |
  | `select` without `options` | `field "pais": select needs options` |
  | `pattern = "("` | `field "cp": invalid pattern` |
  | duplicate field name | `field "email" appears twice` |
  | field named `_x` | `field "_x": names starting with _ are reserved` |
  | unknown field `type = "date"` | `field "d": unknown type "date"` |
  | subject `{{nope}}` | `placeholder {{nope}} is not a field` |
  | `auto_reply` on a site without `turnstile_secret_env` | `auto_reply requires turnstile` |
  | `auto_reply` on a form without an `email` field | `auto_reply needs an email field` |
  | `template = "x"` and `templateExists("x") == false` | `template "x" not found` |
  | smtp `tls = "ssl"` | `tls must be none, starttls or implicit` |
  | provider `type = "mailgun"` | `unknown provider type "mailgun"` |
  Plus `TestLoad_ReportsAllProblems`: a file with three problems → all three substrings present.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement `validate`.** Known field types: `text, textarea, email, tel, number, select, checkbox, country`. Provider types: `smtp, resend`.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(config): fail fast on every invalid config`.

### Task 2: Forms validation

**Files:**
- Create: `internal/forms/validate.go`, `internal/forms/countries.go`, `internal/forms/validate_test.go`

**Interfaces:**
- Consumes: `config.Form`, `config.Field`.
- Produces:
  ```go
  type Value struct { Name, Label, Type, Value string }
  type Values []Value                       // config field order, only fields present in the form definition
  func (v Values) Get(name string) string
  func (v Values) FirstEmail() string       // value of the first email-type field
  type FieldErrors map[string]string        // field name → error code
  func Validate(f *config.Form, in map[string]string) (Values, FieldErrors)
  ```

- [ ] **Step 1: Write failing tests:**
  - `TestValidate_Required`: missing and whitespace-only required → `required`.
  - `TestValidate_TooLong`: 501 runes in a text field → `too_long`; 500 runes of `ñ` → ok (runes, not bytes).
  - `TestValidate_EmailTrimmedAndBareOnly`: `" Juan@Mail.com "` → value `Juan@Mail.com`; `"Juan <j@x.com>"` → `invalid_email`; `"nope"` → `invalid_email`.
  - `TestValidate_Select`: value not in `options` → `invalid_option`.
  - `TestValidate_Number`: `"12.5"` ok, `"doce"` → `invalid_number`.
  - `TestValidate_Country`: `"es"` → value `ES`; `"XX"` → `invalid_country`.
  - `TestValidate_Checkbox`: required + absent → `required`; `"on"` or `"true"` → value `"sí"`; optional + absent → value `"no"`.
  - `TestValidate_Pattern`: mismatch → `invalid_format`.
  - `TestValidate_DropsUnknownAndReserved`: input keys `hack`, `_gotcha`, `cf-turnstile-response` absent from `Values`.
  - `TestValidate_AllErrorsAtOnce`: three bad fields → `len(errs) == 3`.
  - `TestValues_OrderFollowsConfig`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** Trim every value with `strings.TrimSpace`; length by `utf8.RuneCountInString`; email: `net/mail.ParseAddress` and require `addr.Name == "" && addr.Address == value`; countries: a `map[string]struct{}` of the 249 alpha-2 codes.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(forms): validate submissions against form definitions`.

### Task 3: Rendering

**Files:**
- Create: `internal/render/render.go`, `internal/render/templates/default.html.tmpl`, `default.txt.tmpl`, `autoreply.html.tmpl`, `internal/render/render_test.go`, `internal/render/testdata/*.golden`

**Interfaces:**
- Consumes: `config.Site`, `config.Form`, `config.Placeholders`, `forms.Values`.
- Produces:
  ```go
  type Email struct { Subject, HTML, Text string }
  func New(templatesDir string) (*Renderer, error)        // "" = embedded only; dir adds <name>.html.tmpl / .txt.tmpl
  func (r *Renderer) Has(name string) bool                 // used as config.Load's templateExists
  func (r *Renderer) Notification(s *config.Site, f *config.Form, v forms.Values) (Email, error)
  func (r *Renderer) AutoReply(s *config.Site, f *config.Form, v forms.Values) (Email, error)
  func Subject(tmpl string, v forms.Values) string
  ```

- [ ] **Step 1: Write failing tests:**
  - `TestNotification_Golden`: example form + values → HTML and text equal `testdata/notification.html.golden` / `.txt.golden` (update with `-update` flag).
  - `TestNotification_EscapesHTML`: message `<script>alert(1)</script>` → HTML contains `&lt;script&gt;`, not `<script>`.
  - `TestNotification_Accents`: name `Ñandú Pérez 🙂` appears unchanged in HTML and text.
  - `TestNotification_PreservesLineBreaks`: textarea `"a\r\nb\nc"` → HTML contains `a<br>b<br>c`, text contains `a\nb\nc`.
  - `TestSubject_StripsCRLF`: `Subject("[x] {{subject}}", subject="hi\r\nBcc: a@b.c")` → `"[x] hi Bcc: a@b.c"` (no `\r`, no `\n`); result capped at 200 runes.
  - `TestSubject_DefaultWhenEmpty`: form without `subject` → `"[<site>] <form>"`.
  - `TestAutoReply_SubstitutesAndEscapes`: body `"Hola {{nombre}}"` with nombre `<b>Ana</b>` → HTML contains `Hola &lt;b&gt;Ana&lt;/b&gt;`, text `Hola <b>Ana</b>`.
  - `TestNew_LoadsCustomTemplate`: a temp dir with `fancy.html.tmpl` + `fancy.txt.tmpl` → `Has("fancy")`; form `template = "fancy"` renders it.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** with `html/template` and `text/template`, templates embedded via `//go:embed templates/*`. Template data: `{Site, Form string; Fields []forms.Value}`; a `lines` func splits on `\r?\n` for `<br>`. Default notification: site and form label at the top, then each field as label and value; inline CSS only; no external images.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(render): escaped notification and auto-reply emails`.

### Task 4: Mail interface and failover

**Files:**
- Create: `internal/mail/mail.go`, `internal/mail/mail_test.go`

**Interfaces:**
- Produces:
  ```go
  type Message struct { From, ReplyTo string; To []string; Subject, HTML, Text string }
  type Provider interface { Name() string; Send(ctx context.Context, m Message) error }
  func WithFallback(primary, fallback Provider) Provider   // fallback nil → returns primary
  type Factory func(name string, c config.ProviderConfig) (Provider, error)
  func Build(cfg map[string]config.ProviderConfig, factories map[string]Factory) (map[string]Provider, error)
  ```
  The composite's `Name()` returns the name of the provider that sent last; `submit` reads it for logs and metrics through `type SentBy interface{ SentBy() string }`.

- [ ] **Step 1: Write failing tests** with a fake provider: `TestFallback_PrimaryOK` (fallback never called), `TestFallback_PrimaryFails` (fallback called once, nil error, `SentBy()` = fallback name), `TestFallback_BothFail` (error wraps both, `errors.Is` matches each), `TestBuild_UnknownType`.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(mail): provider interface with fallback`.

### Task 5: SMTP provider

**Files:**
- Create: `internal/mail/smtp/smtp.go`, `internal/mail/smtp/smtp_test.go`
- Modify: `go.mod` (test dependency `github.com/emersion/go-smtp`)

**Interfaces:**
- Consumes: `mail.Message`, `config.ProviderConfig`.
- Produces: `func New(name string, c config.ProviderConfig) *Provider` (implements `mail.Provider`); `func BuildMIME(m mail.Message, now time.Time, messageID string) []byte`.

- [ ] **Step 1: Write failing tests:**
  - `TestBuildMIME_Structure`: `multipart/alternative` with a text part then an HTML part, both `charset=UTF-8`, `Content-Transfer-Encoding: quoted-printable`; headers `From`, `To`, `Reply-To`, `Subject`, `Date`, `Message-ID`, `MIME-Version: 1.0`.
  - `TestBuildMIME_EncodesNonASCIISubject`: subject `Nueva admisión: Ñandú` → header uses `=?UTF-8?q?` (from `mime.QEncoding.Encode`) and decodes back to the original.
  - `TestBuildMIME_NoReplyToWhenEmpty`.
  - `TestSend_Plain`: an in-process `go-smtp` server on `127.0.0.1:0` → received `MAIL FROM` is the bare address from `From`, one `RCPT` per recipient, body parses as the MIME built above.
  - `TestSend_StartTLSWithAuth`: server with a self-signed cert and PLAIN auth; provider with `tls="starttls"`, username/password → authenticated send succeeds (test-only `InsecureSkipVerify` through an unexported option).
  - `TestSend_Timeout`: a listener that accepts and never speaks → error within ~10 s (use a 200 ms timeout via the test option).
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** with `net/smtp`: `tls="implicit"` → `tls.Dial` + `smtp.NewClient`; `"starttls"` → `Dial` + `StartTLS`; auth `smtp.PlainAuth` only when a username is set; whole exchange bounded by the context deadline (default 10 s) via `net.Dialer` and `conn.SetDeadline`. Envelope sender = address parsed from `From`.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(mail): SMTP provider with STARTTLS, auth and MIME`.

### Task 6: Resend provider

**Files:**
- Create: `internal/mail/resend/resend.go`, `internal/mail/resend/resend_test.go`

**Interfaces:**
- Produces: `func New(name, apiKey string, opts ...Option) *Provider`; `func WithBaseURL(u string) Option`; `func WithHTTPClient(c *http.Client) Option`. Default base URL `https://api.resend.com`, timeout 10 s.

- [ ] **Step 1: Write failing tests** against `httptest.Server`:
  - `TestSend_Request`: `POST /emails`, `Authorization: Bearer <key>`, `Content-Type: application/json`, body JSON `{"from","to","subject","html","text","reply_to"}` with the message values (`reply_to` omitted when empty).
  - `TestSend_Non2xxIsError`: 422 with a JSON error → error containing the status code, and **not** containing the message subject or body.
  - `TestSend_Timeout`: handler sleeps past a 100 ms client timeout → error.
- [ ] **Step 2:** run → FAIL. **Step 3:** implement. **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(mail): Resend provider`.

### Task 7: Guards

**Files:**
- Create: `internal/guard/origin.go`, `honeypot.go`, `ratelimit.go`, `turnstile.go`, `clientip.go`, and a `_test.go` per file
- Modify: `go.mod` (`golang.org/x/time/rate`)

**Interfaces:**
- Produces:
  ```go
  func RequestOrigin(r *http.Request) string               // Origin header, else scheme://host of Referer, else ""
  func OriginAllowed(origin string, allowed []string) bool
  func Honeypot(in map[string]string) bool                 // _gotcha non-empty after TrimSpace
  func ClientIP(r *http.Request) string                    // CF-Connecting-IP, else host part of RemoteAddr
  type Limiter struct{ ... }
  func NewLimiter(now func() time.Time) *Limiter
  func (l *Limiter) Allow(key string, r config.Rate) (ok bool, retryAfter time.Duration)
  var ErrTurnstileRejected, ErrTurnstileUnavailable error
  type Turnstile struct{ ... }
  func NewTurnstile(c *http.Client, verifyURL string) *Turnstile  // verifyURL "" = Cloudflare's siteverify
  func (t *Turnstile) Verify(ctx context.Context, secret, token, ip string) error
  ```

- [ ] **Step 1: Write failing tests:**
  - Origin: `TestRequestOrigin_HeaderThenReferer`, `TestOriginAllowed_ExactMatchOnly` (`https://example.com.evil.net` and `http://example.com` rejected), `TestOriginAllowed_Empty` → false.
  - `TestHoneypot`.
  - `TestClientIP_PrefersCFConnectingIP`, `TestClientIP_IgnoresXForwardedFor` (only `X-Forwarded-For` set → RemoteAddr host).
  - Limiter with a fake clock: `TestLimiter_AllowsCountPerWindow` (5 allowed, 6th refused with `retryAfter > 0`), `TestLimiter_Refills` (after `per/count` one more allowed), `TestLimiter_KeysIndependent`, `TestLimiter_EvictsIdleKeys` (after `2*per` idle the internal map no longer holds the key).
  - Turnstile vs `httptest`: `TestVerify_Success` (`{"success":true}`; request is form-encoded `secret`, `response`, `remoteip`), `TestVerify_Rejected` → `ErrTurnstileRejected`, `TestVerify_EmptyToken` → `ErrTurnstileRejected` without any HTTP call, `TestVerify_Timeout` and `TestVerify_5xx` → `ErrTurnstileUnavailable`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement.** Limiter: per key a `rate.NewLimiter(rate.Every(per/count), count)`, using `ReserveN(now(), 1)`; when the delay > 0, `CancelAt(now())` and return the delay. Sweep idle keys at most once per minute under a mutex. Turnstile default URL `https://challenges.cloudflare.com/turnstile/v0/siteverify`, 5 s timeout.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Commit** `feat(guard): origin, honeypot, rate limit, Turnstile`.

### Task 8: Metrics

**Files:**
- Create: `internal/observability/metrics.go`, `internal/observability/metrics_test.go`

**Interfaces:**
- Produces:
  ```go
  type Metrics struct{ ... }
  func NewMetrics(reg prometheus.Registerer) *Metrics
  func (m *Metrics) Submission(site, form, outcome string)   // contact_submissions_total{site,form,outcome}
  func (m *Metrics) SendDuration(provider string, d time.Duration) // contact_send_duration_seconds{provider}
  func (m *Metrics) ProviderFailure(provider string)          // contact_provider_failures_total{provider}
  func (m *Metrics) AutoReplyFailure(site, form string)       // contact_autoreply_failures_total{site,form}
  ```
- [ ] **Step 1:** `TestMetrics_Names` with a fresh `prometheus.NewRegistry()` and `testutil.ToFloat64` / `testutil.CollectAndCount` for each metric and label set.
- [ ] **Step 2:** FAIL. **Step 3:** implement. **Step 4:** PASS.
- [ ] **Step 5: Commit** `feat(observability): submission and provider metrics`.

### Task 9: Submit service and v2 HTTP route

**Files:**
- Create: `internal/submit/service.go`, `internal/submit/service_test.go`, `internal/handlers/router.go`, `internal/handlers/router_test.go`

**Interfaces:**
- Consumes: Tasks 1–8.
- Produces:
  ```go
  // package submit
  type Outcome string
  const (Sent Outcome = "sent"; Spam = "spam"; Invalid = "invalid"; Forbidden = "forbidden";
         RateLimited = "rate_limited"; SendFailed = "send_failed"; NotFound = "not_found")
  type Request struct { Site, Form, Origin, ClientIP string; Fields map[string]string }
  type Result struct { Outcome Outcome; Fields forms.FieldErrors; RetryAfter time.Duration }
  type Verifier interface { Verify(ctx context.Context, secret, token, ip string) error }
  type Service struct {
      Config *config.Config; Providers map[string]mail.Provider; Renderer *render.Renderer
      Limiter *guard.Limiter; Turnstile Verifier; Metrics *observability.Metrics; Log *slog.Logger
  }
  func (s *Service) Submit(ctx context.Context, req Request) Result

  // package handlers
  type BuildInfo struct { SHA string }
  func NewRouter(svc *submit.Service, cfg *config.Config, info BuildInfo) *gin.Engine
  ```

- [ ] **Step 1: Write failing service tests** (fake providers that record messages; fake `Verifier`; buffer `slog` JSON handler):
  - `TestSubmit_Sent`: one notification, `From` = site `from`, `ReplyTo` = submitted email, `To` = recipients, outcome `sent`, metric `sent` = 1.
  - `TestSubmit_UnknownSiteAndUnknownFormSameResult` → both `not_found`.
  - `TestSubmit_OriginRejected` → `forbidden`, nothing sent.
  - `TestSubmit_HoneypotFakesSuccess` → outcome `spam` (the handler answers 200), nothing sent.
  - `TestSubmit_RateLimited` → sixth call `rate_limited` with `RetryAfter > 0`.
  - `TestSubmit_TurnstileRejectedAndUnavailable` → both `forbidden` (fail closed); site without a secret never calls the verifier.
  - `TestSubmit_Invalid` → `invalid` with field codes, nothing sent.
  - `TestSubmit_FallbackUsed` → `sent`; `ProviderFailure(primary)` counted.
  - `TestSubmit_AllProvidersFail` → `send_failed`; no auto-reply.
  - `TestSubmit_AutoReplyAfterNotification`: auto-reply sent second, to `FirstEmail()`, from site `from`, empty `ReplyTo`.
  - `TestSubmit_AutoReplyFailureStillSent` → `sent`; `AutoReplyFailure` counted.
  - `TestSubmit_LogsNoFieldValues`: submit with unique marker strings in every field → the captured log output contains none of them, and contains `site`, `form`, `outcome`.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement `Submit`** in the spec §5 order: lookup → origin → honeypot → rate (key `ClientIP|site|form`) → Turnstile (token from `Fields["cf-turnstile-response"]`) → validate → render → send through `mail.WithFallback(primary, fallback)` → auto-reply. Every return path records `Metrics.Submission` and one log line.
- [ ] **Step 4:** run → PASS.
- [ ] **Step 5: Write failing router tests** (`httptest` + the real service with fake providers):
  - `TestV2_JSONSent` → 200 `{"ok":true}`.
  - `TestV2_URLEncodedForm`: `application/x-www-form-urlencoded`, checkbox absent, `name` sent twice → 200, first value used.
  - `TestV2_StatusMapping`: 403 / 404 / 422 (with `fields`) / 429 (with `Retry-After` in whole seconds, minimum 1) / 502, each with the exact body from Global Constraints.
  - `TestV2_TooLarge`: 65 KB body → 413 `too_large`.
  - `TestV2_UnsupportedMediaType`: `text/plain` → 415.
  - `TestV2_CORSPreflight`: `OPTIONS` from an allowed origin → 204 with `Access-Control-Allow-Origin: <that origin>`, `Access-Control-Allow-Methods: POST`, `Access-Control-Allow-Headers: Content-Type`, `Vary: Origin`; from another origin → 204 with no `Access-Control-Allow-Origin`.
- [ ] **Step 6:** run → FAIL.
- [ ] **Step 7: Implement `NewRouter`** for `POST` and `OPTIONS /api/v2/sites/:site/forms/:form`: `http.MaxBytesReader` set to `MaxBodyKB*1024`; JSON is decoded into `map[string]any` and kept as strings (bools become `"true"`/`"false"`, numbers formatted); urlencoded keeps the first value of each key; the POST response also gets the CORS headers when the origin is allowed. Gin recovery and otelgin middleware are kept; request logging logs method, route template, status and latency only.
- [ ] **Step 8:** run → PASS.
- [ ] **Step 9: Commit** `feat: v2 submission flow and route`.

### Task 10: v1 shim, health, version, entrypoint, cleanup

**Files:**
- Modify: `internal/handlers/router.go`, `internal/handlers/router_test.go`, `cmd/contact-api/main.go` (rewrite as `package main`), `Makefile`, `README.md`, `.env.example`
- Delete: `main.go`, `docs/`, `internal/email/`, `internal/handlers/api.go`, `internal/handlers/api_test.go`, `internal/config/config_test.go` from v0 (if it still exists), `docker-compose.prod.yml`, `scripts/health_check.sh`; drop swaggo from `go.mod` (`go mod tidy`)

**Interfaces:**
- Consumes: `NewRouter`, `submit.Service`, `config.Load`, `render.New`, `mail.Build`, `smtp.New`, `resend.New`, `guard.*`, `observability.NewMetrics`, existing `observability.InitTracing`.
- Produces: the binary. Env vars: `CONFIG_FILE` (default `/etc/contact-api/forms.toml`), `TEMPLATES_DIR` (default empty), `OTEL_*` (unchanged). Build-time `-X main.sha=<GIT_SHA>`.

- [ ] **Step 1: Write failing router tests:**
  - `TestV1_Compatible`: `POST /api/v1/contact/example` with v0's JSON `{name,email,subject,message}` → 200 `{"success":true,"message":"Your message has been sent successfully! We will get back to you soon."}`; the fake provider received one message whose `ReplyTo` is the submitted email and whose `From` is the site `from`.
  - `TestV1_Errors`: invalid → 400 `{"success":false,"message":"Invalid request format"}`; all providers fail → 500 `{"success":false,"message":"Failed to send your message. Please try again later."}`; forbidden → 403, rate limited → 429, unknown site → 404, each `{"success":false,...}`.
  - `TestV1_RequiresOrigin`: a v1 request with neither `Origin` nor `Referer` gets 403 (browsers send `Origin` on every POST, so the live forms are unaffected).
  - `TestHealthAndVersion`: `GET /healthz` and `/api/healthz` → `ok`; `GET /version.json` and `/api/version.json` → `{"sha":"abc"}`; `GET /health` → 200; `GET /metrics` → 200; `GET /swagger/index.html` and `GET /api/v1/contact/example/health` → 404.
- [ ] **Step 2:** run → FAIL.
- [ ] **Step 3: Implement** the v1 route mapping to form `contact` and the health and version routes. Rewrite `cmd/contact-api/main.go`: slog JSON; `render.New(TEMPLATES_DIR)`; `config.Load(CONFIG_FILE, os.Getenv, renderer.Has)` — on error log every problem and `os.Exit(1)`; `mail.Build` with factories `smtp` and `resend`; `guard.NewLimiter(time.Now)`; `guard.NewTurnstile(nil, "")`; `NewRouter`; keep the existing graceful shutdown and timeouts. Delete the listed files. Update `Makefile` `run` to `go run ./cmd/contact-api` with `CONFIG_FILE=config/example.toml`. Update the README: v2 API, config format with an example, the v1 shim, `.env` variables.
- [ ] **Step 4:** `go mod tidy && golangci-lint run ./... && go test -race ./...` → all pass; `CONFIG_FILE=config/example.toml go run ./cmd/contact-api` starts and `curl -s localhost:3002/healthz` prints `ok`.
- [ ] **Step 5: Commit** `feat!: v2 entrypoint, v1 compatibility shim, drop swagger`.

### Task 11: Image, CI and CD

**Files:**
- Modify: `Dockerfile`, `.dockerignore` (create if missing)
- Create: `scripts/image-smoke.sh`, `scripts/smoke.sh`, `.github/workflows/cd-main.yml`
- Replace: `.github/workflows/ci.yml`; Delete: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: `/healthz`, `/version.json`, `config/example.toml`.
- Produces: image `registry.lab.staysy.co:5000/release/contact-api:sha-<sha>`; gate kind `contact-api`; runner pools `contact-api-pr` / `contact-api-release`.

- [ ] **Step 1: Dockerfile.** `golang:1.26-alpine` builder, `go mod download` after copying `go.mod` **and** `go.sum`; `ARG GIT_SHA`; `CGO_ENABLED=0 go build -ldflags "-s -w -X main.sha=${GIT_SHA}" -o /out/contact-api ./cmd/contact-api`. Runtime `alpine:3.21`, non-root user, `COPY config/example.toml /etc/contact-api/forms.toml`, `EXPOSE 3002`, `HEALTHCHECK --interval=10s --timeout=3s --retries=3 CMD wget -qO- http://127.0.0.1:3002/healthz || exit 1`.
- [ ] **Step 2: `scripts/image-smoke.sh <image>`**, following site-template's script (private network, wait until healthy, `get` helper through `alpine:3.21`), with no browser step. Checks:
  - `/healthz` = `ok`;
  - `/version.json` contains `"sha":`;
  - `POST /api/v2/sites/nope/forms/contact` → 404;
  - `POST /api/v2/sites/example/forms/contact` with `Origin: https://example.com` and `{}` → 422 and a body containing `"required"`.
  It never sends mail.
- [ ] **Step 3: `ci.yml`** from site-template's `ci.yml`. Substitute `__NAME__` → `contact-api` and `__REPO__` → `contact-api`, and drop the template-only `if:` lines. Add a `test` job on the same runner expression before `image`:
  - `actions/setup-go@v5` with `go-version: '1.26'`;
  - `go test -race ./...`;
  - `golangci/golangci-lint-action` (keep v7 with `version: v2.5.0` from the current workflow).

  `image` needs `test`.
- [ ] **Step 4: `cd-main.yml`** from site-template's `cd-main.yml` with the same substitutions, plus `SITE_URL` defaulting to `https://nahuelsantos.com/api`. The template's smoke checks `$SITE_URL/healthz` and `$SITE_URL/version.json`, both served by Task 10.
- [ ] **Step 5: `scripts/smoke.sh <base-url>`** posts `{"name":"smoke","email":"smoke@example.com","subject":"smoke <date>","message":"post-deploy smoke"}` with `Origin: https://nahuelsantos.com` to `<base-url>/api/v2/sites/smoke/forms/contact`. It expects 200 and prints "check the smoke inbox".
- [ ] **Step 6: Verify locally:**
  - `docker build --build-arg GIT_SHA=test -t contact-api:test . && bash scripts/image-smoke.sh contact-api:test` → `image-smoke: ok`;
  - `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 -shellcheck=` → no output.
- [ ] **Step 7: Commit** `ci: build once, push to Zot, deploy through the melee gate`.

### Task 12: home-lab — register, configure and route contact-api

Repository: `nahuelsantos/home-lab`, branch from main.

**Files:**
- Modify: `scripts/sites/sites.py`, `scripts/sites/test_sites.py`, `config/sites.toml`, gate files named by `sites.py check` (`scripts/melee/ci-deploy-gate/gate.sh`, `run.sh`, `scripts/melee/09-setup-ci-deploy-gate.sh`, `scripts/melee/test-ci-deploy-gate.sh`), `scripts/plunder/14-setup-ci-runners.sh` (via `generate`), `apps/homelab-panel/panel/catalog.toml`, `compose/plunder/prometheus/prometheus.yml` (via `generate`), `docs/inventory/services-matrix.md`, `docs/runbooks/loopingbyte.md`
- Create: `compose/sites/contact-api/docker-compose.yml`, `compose/sites/contact-api/forms.toml`, `compose/sites/contact-api/.env.example`, `compose/plunder/prometheus/rules/contact-api.rules.yml` (+ a case in `compose/plunder/prometheus/tests/` following the existing rule tests), `docs/runbooks/contact-api.md`
- Delete: `compose/apis/contact-api/` (the live container on melee keeps running from `/opt/server`; cutover is in the runbook)

**Interfaces:**
- Produces: registry entry `contact-api`; stack dir `contact-api`; container `contact-api`; gate row `{ key = "uat", kind = "contact-api", var = "CONTACT_API_IMAGE", service = "contact-api" }`; probe `https://nahuelsantos.com/api/healthz`.

- [ ] **Step 1: Failing test in `scripts/sites/test_sites.py`.** contact-api routes `/api` paths on hosts that belong to other stacks, so the registry needs a field for that. Add a new optional stack key `borrowed = [...]`: hosts owned by another stack that this stack serves by path. They count in the `Host()` comparison, but they're never tunnelled or probed.
  Test `test_borrowed_hosts_count_for_host_check_only`: a registry with a stack `borrowed = ["a.example"]` and a compose whose router rule has `Host(\`a.example\`)` → `check` reports no stack problem, and `generate` adds no tunnel hostname for it.
- [ ] **Step 2:** `python3 scripts/sites/test_sites.py` → FAIL.
- [ ] **Step 3: Implement `borrowed`** in `sites.py`:
  - parse and document it in the `config/sites.toml` header;
  - include it in the stacks `Host()` diff only (`set(st.public + st.lan + st.borrowed)`).
- [ ] **Step 4:** `python3 scripts/sites/test_sites.py` → PASS.
- [ ] **Step 5: Registry entry** in `config/sites.toml`:
  ```toml
  [[site]]
  name = "contact-api"
  title = "Contact API"
  repo = "nahuelsantos/contact-api"
  runners = { prefix = "contact-api", max = 1 }
  deploy = "gate"
  catalog = "Contact API"

    [[site.stack]]
    env = "prod"
    dir = "contact-api"
    container = "contact-api"
    port = 3002
    borrowed = ["nahuelsantos.com", "www.nahuelsantos.com", "cv.nahuelsantos.com", "loopingbyte.com", "www.loopingbyte.com"]
    probe = "https://nahuelsantos.com/api/healthz"
    note = "Shared forms service; sites route their /api paths to it"
    gate = { key = "uat", kind = "contact-api", var = "CONTACT_API_IMAGE", service = "contact-api" }
  ```
- [ ] **Step 6: Compose** `compose/sites/contact-api/docker-compose.yml`. It starts from `compose/apis/contact-api/docker-compose.yml`, with these changes:
  - `name: contact-api`;
  - `image: ${CONTACT_API_IMAGE}`;
  - **no `ports:`**;
  - volume `./forms.toml:/etc/contact-api/forms.toml:ro`;
  - `env_file: .env`, and the environment block drops the SMTP/DEFAULT variables;
  - logging `json-file` with `max-size: 10m`, `max-file: 3`;
  - no compose `healthcheck` (the image has one);
  - **the existing Traefik router and middleware labels, Prometheus labels, `mem_limit`, `cpus`, `mem_reservation` and `external_links` copied unchanged.**
- [ ] **Step 7: `forms.toml`** per spec §8:
  - Providers `melee` (smtp `mail-server:25`, `tls = "none"`) and `resend` (`api_key_env = "RESEND_API_KEY"`).
  - `[defaults] provider = "melee"` with **no fallback** (migration step 1).
  - Sites:
    - `nahuelsantos`: `from = "Nahuel Santos <hi@nahuelsantos.com>"`, `to = ["nahuelsantos@gmail.com"]`, origins for the three nahuelsantos hosts;
    - `loopingbyte`: from `hi@loopingbyte.com`, same `to`, its two origins;
    - `smoke`: `to = ["nahuelsantos@gmail.com"]`, origin `https://nahuelsantos.com`.

    Each has a `contact` form with v0's four fields.
  - `.env.example`: `CONTACT_API_IMAGE=registry.lab.staysy.co:5000/release/contact-api@sha256:<pinned by the gate>`, `RESEND_API_KEY=` (placeholder), `OTEL_EXPORTER_OTLP_ENDPOINT=http://10.10.10.2:4318`, `OTEL_SERVICE_NAME=contact-api`.
- [ ] **Step 8: Generate and check.**
  - `python3 scripts/sites/sites.py generate`, then `python3 scripts/sites/sites.py check`.
  - For every problem it names in the gate files or `catalog.toml`, add the missing row by copying the `wisp` row in the same file and substituting `contact-api` / `CONTACT_API_IMAGE` / `contact-api` (service and container).
  - Repeat until it prints `sites: ok`.
  - `bash scripts/melee/test-ci-deploy-gate.sh` passes (if it runs offline; otherwise note it for melee).
- [ ] **Step 9: Alerts** in `contact-api.rules.yml`, with severity and labels styled like the neighbouring rule files:
  - `ContactAPISendFailed`: `increase(contact_submissions_total{outcome="send_failed"}[15m]) > 0`;
  - `ContactAPIFallbackInUse`: `increase(contact_provider_failures_total[30m]) > 0`, `for: 30m`.

  Add a promtool unit test case beside the existing ones; run the repo's rule-test command (as documented in `docs/monitoring/alerts.md`) → PASS.
- [ ] **Step 10: Runbook** `docs/runbooks/contact-api.md`. It covers:
  - (a) what it is and the table header style of `wisp.md`;
  - (b) adding a site or form: edit `forms.toml`, ship, then on melee `sudo install -m 0644 compose/sites/contact-api/forms.toml /opt/server/sites/contact-api/` and `docker restart contact-api`;
  - (c) **first install and cutover** (the CLAUDE.md rule "never disrupt melee"): install the stack dir as `wisp.md` step 2 does; secrets on the repo as `wisp.md` step 5; first deploy pins the digest; at cutover, `docker stop contact-api && docker rename contact-api contact-api-old` in the old location, then the gate's deploy (or `docker compose up -d` in `/opt/server/sites/contact-api`); verify the nahuelsantos.com and loopingbyte.com forms plus `scripts/smoke.sh https://nahuelsantos.com`; rollback = `docker rm -f contact-api && docker rename contact-api-old contact-api && docker start contact-api`;
  - (d) migration step 2 (Resend: add the key, check Resend's DNS records per sending domain in Cloudflare, set `provider = "resend"` and `fallback = "melee"`, restart);
  - (e) alerts and what to do.

  Update `services-matrix.md` and `loopingbyte.md` to point at `compose/sites/contact-api`. Add the runbook link to CLAUDE.md's *Common Operations*.
- [ ] **Step 11: Commit** `contact-api: register as a gated stack with a forms config` and push the branch.

---

## Self-review notes

- **Spec coverage:**
  - §2 → Tasks 1–9; §3 → 1, 1b; §4 → 9, 10; §5 → 9; §6 → 3; §7 → 8, 9 and the log test; §8 → 11, 12; §9 → the tests in every task plus Task 11's smoke.
  - The CORS preflight (§4) is in Task 9.
  - Removing the v0 health route and swagger (§4) is in Task 10.
- **Deviations from the spec, to confirm in review:**
  - `/version.json` returns `{"sha":...}`, not `{version, commit}` (shared CD smoke).
  - `/healthz` and `/api/*` aliases are added so CD can check health through `nahuelsantos.com/api`.
  - The stack moves from `compose/apis/` to `compose/sites/` so the sites registry and gate tooling apply, and `sites.py` gains `borrowed` hosts.
  - The v1 shim requires an allowed `Origin`/`Referer` (new behaviour, consistent with v2).
