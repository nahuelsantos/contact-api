# contact-api v2 — a shared forms service — Design

**Date:** 2026-11-05
**Status:** Approved in brainstorming; awaiting written-spec review
**Repo:** `nahuelsantos/contact-api` (this repo). Deployment config lives in `nahuelsantos/home-lab`.

## 1. Why

contact-api v0.0.2 sends one fixed contact form (name, email, subject, message) to one recipient
for every site, over plain SMTP to melee's `mail-server`. A new site — Red Analítica, a curated
psychoanalysis network for Argentines abroad — needs four different forms (patient admission,
supervision request, professional application, study-group interest), some carrying sensitive
data. Rather than build a second service, contact-api becomes **one forms service for every site
in the homelab**, with pluggable mail providers.

Defects in v0.0.2 this design fixes along the way:

- `From:` is the visitor's address, which fails SPF/DMARC → mail lands in spam.
- User input is interpolated into the HTML email without escaping.
- `getRecipientForWebsite` ignores the site: one recipient for everything.
- No spam protection.
- Image on GHCR, outside the homelab's Zot + deploy-gate pipeline; host port `3002` published.

### Success criteria

1. nahuelsantos.com and loopingbyte.com contact forms keep working with **no change on the sites**
   (v1 endpoint preserved).
2. A new site or form is added with a config change in home-lab and a restart — no code, no release.
3. Notifications are sent `From:` the site's own address with `Reply-To:` the visitor, via Resend
   with the melee SMTP server as fallback.
4. No form field value is ever written to logs, traces or metrics.
5. Spam is filtered by honeypot, rate limit and origin check everywhere, plus Turnstile where enabled.

### Non-goals (YAGNI)

- No database, no stored submissions, no admin UI (a later, separate sub-project may add one).
- No file uploads.
- No HTTP API providers other than Resend; other vendors are reached through SMTP.
- Removing the v1 endpoint (later, once both sites post to v2).
- Distributed rate limiting (single instance on melee).

## 2. Architecture

One Go binary (Gin, as today), stateless. Packages, each with one job:

| Package | Responsibility | Depends on |
|---|---|---|
| `internal/config` | Parse the TOML file, resolve `*_env` secrets from the environment, validate everything; refuse to start on any error | — |
| `internal/forms` | Validate a raw submission against a form definition → clean ordered values or `map[field]code` errors. Pure, no I/O | config types |
| `internal/guard` | Origin check, honeypot, in-memory rate limiter, Turnstile verifier | config types, `net/http` (Turnstile) |
| `internal/render` | Build notification and auto-reply emails (subject, HTML, text) with `html/template` / `text/template` | forms output |
| `internal/mail` | `Provider` interface; `smtp`, `resend`, `failover` implementations | — |
| `internal/handlers` | HTTP wiring: v2 route, v1 shim, health, metrics, version | all of the above |
| `internal/observability` | OTel tracing (existing) + Prometheus metrics | — |

### 2.1 Provider interface

```go
type Message struct {
    From    string   // site's own address, e.g. "Red Analítica <admisiones@…>"
    ReplyTo string   // visitor's address (notification) or empty
    To      []string
    Subject string
    HTML    string
    Text    string
}

type Provider interface {
    Name() string
    Send(ctx context.Context, m Message) error
}
```

- **`smtp`** — host, port, optional username/password (from env), `tls = "none" | "starttls" | "implicit"`.
  Builds a proper multipart/alternative MIME message with `Date`, `Message-ID`, encoded
  headers (RFC 2047 for non-ASCII subjects and names — Spanish accents).
- **`resend`** — `POST https://api.resend.com/emails` with bearer key from env; 10 s timeout.
- **`failover`** — wraps primary + fallback: on primary error, try fallback once; return a joined
  error if both fail. Not a config `type`; built from `provider` + `fallback`.

## 3. Configuration

File: `/etc/contact-api/forms.toml` (path overridable by `CONFIG_FILE`), mounted read-only.
Secrets are never in the TOML — only names of environment variables (`*_env`).

```toml
[server]
port          = 3002
max_body_kb   = 64

[defaults]
provider = "resend"
fallback = "melee"            # optional
rate     = { count = 5, per = "10m" }

[provider.melee]
type = "smtp"
host = "mail-server"
port = 25
tls  = "none"

[provider.resend]
type        = "resend"
api_key_env = "RESEND_API_KEY"

[site.nahuelsantos]
from    = "Nahuel Santos <hi@nahuelsantos.com>"
to      = ["nahuelsantos@gmail.com"]
origins = ["https://nahuelsantos.com", "https://www.nahuelsantos.com", "https://cv.nahuelsantos.com"]
# provider / fallback / turnstile_secret_env optional per site

[site.nahuelsantos.form.contact]
subject = "[nahuelsantos] {{subject}}"
fields = [
  { name = "name",    label = "Name",    type = "text",     required = true, max_len = 200 },
  { name = "email",   label = "Email",   type = "email",    required = true },
  { name = "subject", label = "Subject", type = "text",     required = true, max_len = 200 },
  { name = "message", label = "Message", type = "textarea", required = true, max_len = 10000 },
]

# Example of a form with auto-reply (Red Analítica, added in its own sub-project):
# [site.redanalitica]
# turnstile_secret_env = "TURNSTILE_SECRET_REDANALITICA"
# [site.redanalitica.form.admision]
# to       = ["admisiones@…"]            # overrides site `to`
# rate     = { count = 3, per = "1h" }
# template = "default"                    # or the name of a template file under templates/
# auto_reply = { subject = "Recibimos tu solicitud", body = "Hola {{nombre}}, …" }
```

### 3.1 Field types

`text`, `textarea`, `email`, `tel`, `number`, `select` (requires `options = [...]`), `checkbox`
(value must be `"on"`/`"true"` when required), `country` (ISO 3166-1 alpha-2).
Per-field options: `required`, `max_len` (default 500; `textarea` default 10000), `pattern` (Go
regexp), `label` (shown in the email; defaults to `name`).

### 3.2 Startup validation (fail fast)

The service exits non-zero, with every problem listed, if:

- the TOML does not parse, or has unknown keys;
- a referenced `*_env` variable is empty;
- `defaults.provider`, a site `provider` or any `fallback` names an unknown provider;
- a site has no `from`, no `to` (at site or form level) or no `origins`;
- a `select` has no `options`, a `pattern` does not compile, a field name repeats or starts with `_`;
- a subject/auto-reply placeholder names a field that does not exist in the form;
- a form has `auto_reply` but its site has no `turnstile_secret_env`;
- a form has `auto_reply` but no `email`-type field (the reply recipient is the first `email` field).

## 4. HTTP API

| Method & path | Purpose |
|---|---|
| `POST /api/v2/sites/{site}/forms/{form}` | Submit a form. JSON or `application/x-www-form-urlencoded` |
| `POST /api/v1/contact/{website}` | **Compatibility shim**: same as v2 with form `contact`; same JSON response shape as v0.0.2 |
| `GET /health` | Liveness |
| `GET /metrics` | Prometheus |
| `GET /version.json` | `{version, commit}` — what the deploy gate checks |

The v0.0.2 routes `GET /api/v1/contact/{website}/health` and `/swagger/*` are removed (the former
leaked the recipient and SMTP host).

Reserved form fields (never forwarded): `_gotcha` (honeypot), `cf-turnstile-response`.

### 4.1 Responses (v2)

| Status | Body | When |
|---|---|---|
| 200 | `{"ok":true}` | Sent (also returned for honeypot hits) |
| 403 | `{"ok":false,"error":"forbidden"}` | Origin not allowed, Turnstile missing/invalid/unverifiable |
| 404 | `{"ok":false,"error":"not_found"}` | Unknown site **or** form (indistinguishable) |
| 413 | `{"ok":false,"error":"too_large"}` | Body over `max_body_kb` |
| 415 | `{"ok":false,"error":"unsupported_media_type"}` | Not JSON or urlencoded |
| 422 | `{"ok":false,"error":"invalid","fields":{"email":"invalid_email"}}` | Validation failed; all field errors at once |
| 429 | `{"ok":false,"error":"rate_limited"}` + `Retry-After` | Over the rate limit |
| 502 | `{"ok":false,"error":"send_failed"}` | Every provider failed |

Field error codes: `required`, `too_long`, `invalid_email`, `invalid_option`, `invalid_number`,
`invalid_country`, `invalid_format`. Sites translate codes into their own language.

Requests from a browser need CORS only when the site posts cross-origin; the service answers
preflight for the site's `origins` and nothing else.

## 5. Request flow

Each step either passes or ends the request.

1. **Size cap** → 413.
2. **Lookup** site + form → 404.
3. **Origin** — `Origin` header (falling back to `Referer`'s origin) must be in the site's
   `origins` → 403.
4. **Honeypot** — `_gotcha` non-empty → fake 200, nothing sent, counted as `spam`.
5. **Rate limit** — token bucket keyed by `CF-Connecting-IP` (falling back to the remote address)
   + site + form; form `rate` overrides `defaults.rate` → 429.
6. **Turnstile** (site has `turnstile_secret_env`) — verify `cf-turnstile-response` against
   `https://challenges.cloudflare.com/turnstile/v0/siteverify` with the client IP, 5 s timeout.
   Invalid **or unverifiable** (timeout, Cloudflare error) → 403. *Fail closed.*
7. **Validate** with `internal/forms`; unknown fields dropped → 422.
8. **Render** notification (and auto-reply if configured). All values escaped; CR/LF stripped
   from every value placed in a header (subject, reply-to, names).
9. **Send** notification through the site's provider (failover if `fallback` set) → 502 if all fail.
10. **Auto-reply** (if configured), only after step 9 succeeded, `To:` the first `email` field,
    `From:` the site address, no `Reply-To`. Failure is logged and counted but the response stays 200.

## 6. Email rendering

- **Default template** (`templates/default.html.tmpl` + `.txt.tmpl`, embedded with `embed`):
  site + form badge, then every submitted field in config order as `label: value`, textareas with
  preserved line breaks. Neutral styling, inline CSS, readable in Gmail.
- A form may set `template = "<name>"` to use `templates/<name>.html.tmpl` from an optional
  mounted templates directory (`TEMPLATES_DIR`); missing template → startup error.
- Subject and auto-reply placeholders use `{{field}}`, filled with the validated value and
  escaped for the target (header-safe in subjects, HTML-escaped in bodies).
- Auto-reply bodies are plain text in config, rendered into the same neutral HTML shell plus a
  text part.

## 7. Privacy and observability

- **Never logged/traced/metered:** any form field value, including email addresses and names.
  This is a change from v0.0.2, which logs the visitor's email.
- **Logged** (slog JSON): site, form, outcome, provider used, duration, request id, client IP
  for 429/403 only.
- **Metrics:**
  - `contact_submissions_total{site,form,outcome}` — outcome ∈ `sent`, `spam`, `invalid`,
    `forbidden`, `rate_limited`, `send_failed`
  - `contact_send_duration_seconds{provider}` (histogram)
  - `contact_provider_failures_total{provider}`
  - `contact_autoreply_failures_total{site,form}`
- **Alerts** (in home-lab `docs/monitoring/alerts.md` / Prometheus rules on plunder):
  any `send_failed` in 15 min; primary provider failure rate > 0 for 30 min (fallback in use).
- OTel tracing kept (existing `OTEL_*` variables), spans without field attributes.

## 8. Deployment (home-lab)

- **CI/CD** moves to the homelab pipeline: plunder runners build once, push
  `release/contact-api:sha-<sha>` to Zot; CD deploys through the melee deploy gate. GHCR release
  workflow removed. Registered in `config/sites.toml` (it is a repo this lab builds and serves;
  `sites.py check` must pass).
- **Compose** (`compose/apis/contact-api/docker-compose.yml`): image from Zot pinned by `.env`
  variable; `forms.toml` mounted read-only at `/etc/contact-api/forms.toml`; `.env` holds
  `RESEND_API_KEY` and Turnstile secrets; `.env.example` committed with placeholders; **no host
  `ports:`** (Traefik only); existing Traefik routers, Prometheus labels, resource limits and
  healthcheck kept (healthcheck switched to `wget`/binary self-check if the image has no curl).
- `forms.toml` starts with `nahuelsantos` and `loopingbyte` sites, each with one `contact` form
  matching today's fields, plus a `smoke` site whose recipient is the owner's address.

### 8.1 Migration (melee is live production)

1. Release v2 with the v1 shim, `defaults.provider = "melee"`, no fallback. Behaviour identical to
   today except correct `From`/`Reply-To` and escaping. Smoke test: post to the `smoke` site and to
   both v1 routes.
2. Add `RESEND_API_KEY`, set `defaults.provider = "resend"`, `fallback = "melee"`. DNS: confirm
   Resend's SPF/DKIM records exist for each sending domain in Cloudflare. Config-only change.
3. Sites move to `/api/v2/...` and enable Turnstile at their own pace (not part of this spec).

**Rollback:** redeploy the previous digest through the gate. v0.0.2 ignores the TOML and uses its
old env variables, which stay in `.env` until step 3 is complete everywhere.

## 9. Testing

TDD throughout; `go test ./...` and `golangci-lint` in CI.

- **config** — table tests for each startup-validation rule in §3.2, and a valid full example.
- **forms** — every field type and option; unknown fields dropped; all errors returned together.
- **guard** — origin (header, Referer fallback, mismatch); honeypot; rate limiter with an injected
  clock; Turnstile against `httptest` (success, failure, timeout → reject).
- **render** — escaping of `<script>`, CR/LF header injection in subject and names, RFC 2047
  encoding of accented subjects, placeholder substitution.
- **mail** — `smtp` against an in-process fake SMTP server (plain and STARTTLS, auth);
  `resend` against `httptest` asserting method, headers and JSON body; `failover` with fake
  providers (primary ok, primary fails → fallback, both fail).
- **handlers** — full request flow through Gin with fake providers: each status in §4.1, auto-reply
  only after success, auto-reply failure still 200, v1 shim request/response compatibility with
  v0.0.2's.
- **Post-deploy smoke** — `scripts/smoke.sh` posts to the `smoke` site and checks for 200; the one
  test that sends real mail.
