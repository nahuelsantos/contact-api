# Contact API

[![Release](https://img.shields.io/github/v/release/nahuelsantos/contact-api?include_prereleases&style=for-the-badge)](https://github.com/nahuelsantos/contact-api/releases)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white&style=for-the-badge)](https://github.com/nahuelsantos/contact-api/blob/main/Dockerfile)

A small service that receives form submissions from websites and emails them.
Sites, forms, fields, recipients and mail providers are declared in one TOML
file; adding a site or a form needs no code change.

## Configuration

The config file is read from `CONFIG_FILE` (default `/etc/contact-api/forms.toml`).
Unknown keys are errors, and secrets are never written in the file: providers
and sites name an environment variable (`*_env`) instead. A full example lives
in [`config/example.toml`](config/example.toml).

```toml
[server]
port = 3002          # default
max_body_kb = 64     # default

[provider.resend]
type = "resend"
api_key_env = "RESEND_API_KEY"

[provider.melee]
type = "smtp"
host = "mail-server"
port = 25
tls  = "none"        # none | starttls | implicit

[defaults]
provider = "resend"
fallback = "melee"   # optional: used when the main provider fails
rate = { count = 5, per = "10m" }

[site.example]
from    = "Example <noreply@example.com>"
to      = ["owner@example.com"]
origins = ["https://example.com"]

[site.example.form.contact]
subject = "New message from {{name}}"

[[site.example.form.contact.fields]]
name = "name"
type = "text"
required = true

[[site.example.form.contact.fields]]
name = "email"
type = "email"
required = true

[[site.example.form.contact.fields]]
name = "message"
type = "textarea"
required = true
```

## API v2

`POST /api/v2/sites/{site}/forms/{form}` with `application/json` or
`application/x-www-form-urlencoded`. The request must come from one of the
site's `origins` (the `Origin` header, or the `Referer` as a fallback).

| Status | Body |
|---|---|
| 200 | `{"ok":true}` |
| 403 | `{"ok":false,"error":"forbidden"}` |
| 404 | `{"ok":false,"error":"not_found"}` |
| 413 | `{"ok":false,"error":"too_large"}` |
| 415 | `{"ok":false,"error":"unsupported_media_type"}` |
| 422 | `{"ok":false,"error":"invalid","fields":{"email":"invalid_email"}}` |
| 429 | `{"ok":false,"error":"rate_limited"}` plus `Retry-After` |
| 502 | `{"ok":false,"error":"send_failed"}` |

Field error codes: `required`, `too_long`, `invalid_email`, `invalid_option`,
`invalid_number`, `invalid_country`, `invalid_format`.

A filled `_gotcha` honeypot field gets a normal 200 and nothing is sent. Sites
can require a Cloudflare Turnstile token (`cf-turnstile-response`). `OPTIONS`
preflights are answered for allowed origins.

## v1 compatibility

`POST /api/v1/contact/{website}` keeps working for existing sites. It is the
v2 form named `contact` of the site `{website}` and answers with the old
bodies: `{"success":true,"message":"..."}` on success (and for spam),
`{"success":false,"message":"..."}` with 400 (invalid), 403, 404, 429 or 500
(send failed). It needs an `Origin` or `Referer` header, which browsers send on
every form POST. As in v0 the body is read as JSON whatever the `Content-Type`
(a `text/plain` fetch body works), up to the configured size. CORS headers and
`OPTIONS` preflights (204) are answered for the site's `origins`, like v2.

## Operations

| Route | Purpose |
|---|---|
| `GET /healthz`, `/api/healthz` | `ok` (text/plain) |
| `GET /health` | `{"status":"ok"}` (legacy checks) |
| `GET /version.json`, `/api/version.json` | `{"sha":"<git sha>"}` |
| `GET /metrics` | Prometheus metrics |

Logs are JSON on stdout and never contain submitted values. Each submission
logs one `submission` line. When delivery fails it names the failure class
only, never server text or message content: `error_class` (the final failure,
on `send_failed`), `provider_errors` (`name:class` for every provider that
failed, also when a fallback delivered) and `auto_reply_error`. A class is the
stage plus the numeric reply, such as `status_502` (Resend HTTP status),
`rcpt_550` or `auth_535` (SMTP stage and reply code), `dial_timeout`.

Delivery is detached from the request: a visitor closing the tab does not abort
a send. The notification chain is bounded at 25 s and the auto-reply at 20 s.

## Environment variables

| Variable | Purpose |
|---|---|
| `CONFIG_FILE` | Path to the TOML config (default `/etc/contact-api/forms.toml`) |
| `TEMPLATES_DIR` | Optional directory with custom email templates |
| `RESEND_API_KEY` | Example secret named by `api_key_env` in the config |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Enables OTLP/HTTP tracing when set |
| `OTEL_SERVICE_NAME` | Trace service name (`contact-api`) |

Copy `.env.example` to `.env` for local use.

## Development

```bash
make run     # run locally with config/example.toml
make test    # golangci-lint and go test -race
make start   # docker compose up (with a MailHog SMTP server)
make stop
```

The build sets the revision with `-ldflags "-X main.sha=<git sha>"`.

## License

See [LICENSE](LICENSE).
