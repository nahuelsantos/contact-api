package handlers_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/guard"
	"github.com/nahuelsantos/contact-api/internal/handlers"
	"github.com/nahuelsantos/contact-api/internal/mail"
	"github.com/nahuelsantos/contact-api/internal/observability"
	"github.com/nahuelsantos/contact-api/internal/render"
	"github.com/nahuelsantos/contact-api/internal/submit"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	path   = "/api/v2/sites/example/forms/contact"
	origin = "https://example.com"
)

var errBoom = errors.New("boom")

type fakeProvider struct {
	mu   sync.Mutex
	err  error
	sent []mail.Message
}

func (f *fakeProvider) Name() string { return "main" }

func (f *fakeProvider) Send(_ context.Context, m mail.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)

	return f.err
}

type rig struct {
	h    http.Handler
	prov *fakeProvider
}

func newRig(t *testing.T) *rig {
	t.Helper()
	cfg := &config.Config{
		Server:   config.Server{MaxBodyKB: 64},
		Defaults: config.Defaults{Provider: "main", Rate: config.Rate{Count: 5, Per: config.Duration(10 * time.Minute)}},
		Sites: map[string]*config.Site{"example": {
			Name: "example", From: "noreply@example.com", To: []string{"inbox@example.com"},
			Origins: []string{origin},
			Forms: map[string]*config.Form{"contact": {Name: "contact", Fields: []config.Field{
				{Name: "name", Type: "text", Required: true, MaxLen: 500},
				{Name: "email", Type: "email", Required: true, MaxLen: 500},
				{Name: "message", Type: "textarea", Required: true, MaxLen: 10000},
				{Name: "agree", Type: "checkbox", MaxLen: 500},
			}}},
		}},
	}
	rd, err := render.New("")
	if err != nil {
		t.Fatal(err)
	}
	prov := &fakeProvider{}
	svc := &submit.Service{
		Config: cfg, Providers: map[string]mail.Provider{"main": prov}, Renderer: rd,
		Limiter: guard.NewLimiter(time.Now), Metrics: observability.NewMetrics(prometheus.NewRegistry()),
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}

	return &rig{h: handlers.NewRouter(svc, cfg, handlers.BuildInfo{SHA: "x"}), prov: prov}
}

func (r *rig) do(method, p, ctype, org, payload string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, p, strings.NewReader(payload))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if org != "" {
		req.Header.Set("Origin", org)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)

	return rec
}

const goodJSON = `{"name":"Ana","email":"ana@example.com","message":"Hola"}`

func assertBody(t *testing.T, rec *httptest.ResponseRecorder, status int, want string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, status, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestV2_JSONSent(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodPost, path, "application/json; charset=utf-8", origin, goodJSON)
	assertBody(t, rec, 200, `{"ok":true}`)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
		t.Fatalf("ACAO = %q", got)
	}
}

func TestV2_JSONScalars(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodPost, path, "application/json", origin,
		`{"name":"Ana","email":"ana@example.com","message":"Hola","agree":true,"n":1.5,"o":{"a":1},"l":[1],"z":null}`)
	assertBody(t, rec, 200, `{"ok":true}`)
	if len(r.prov.sent) != 1 || !strings.Contains(r.prov.sent[0].Text, "sí") {
		t.Fatalf("checkbox true not honoured: %+v", r.prov.sent)
	}
}

func TestV2_URLEncodedForm(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodPost, path, "application/x-www-form-urlencoded", origin,
		"name=First&name=Second&email=ana%40example.com&message=Hola")
	assertBody(t, rec, 200, `{"ok":true}`)
	if len(r.prov.sent) != 1 || !strings.Contains(r.prov.sent[0].Text, "First") || strings.Contains(r.prov.sent[0].Text, "Second") {
		t.Fatalf("message = %+v", r.prov.sent)
	}
	if !strings.Contains(r.prov.sent[0].Text, "no") {
		t.Fatalf("absent checkbox should read no: %s", r.prov.sent[0].Text)
	}
}

func TestV2_StatusMapping(t *testing.T) {
	t.Run("403", func(t *testing.T) {
		assertBody(t, newRig(t).do(http.MethodPost, path, "application/json", "https://evil.example", goodJSON),
			403, `{"ok":false,"error":"forbidden"}`)
	})
	t.Run("404", func(t *testing.T) {
		assertBody(t, newRig(t).do(http.MethodPost, "/api/v2/sites/nope/forms/contact", "application/json", origin, goodJSON),
			404, `{"ok":false,"error":"not_found"}`)
	})
	t.Run("422", func(t *testing.T) {
		assertBody(t, newRig(t).do(http.MethodPost, path, "application/json", origin, `{"name":"Ana","email":"bad"}`),
			422, `{"ok":false,"error":"invalid","fields":{"email":"invalid_email","message":"required"}}`)
	})
	t.Run("429", func(t *testing.T) {
		r := newRig(t)
		for range 5 {
			r.do(http.MethodPost, path, "application/json", origin, goodJSON)
		}
		rec := r.do(http.MethodPost, path, "application/json", origin, goodJSON)
		assertBody(t, rec, 429, `{"ok":false,"error":"rate_limited"}`)
		if ra := rec.Header().Get("Retry-After"); ra != "120" && ra != "119" {
			t.Fatalf("Retry-After = %q", ra)
		}
	})
	t.Run("502", func(t *testing.T) {
		r := newRig(t)
		r.prov.err = errBoom
		assertBody(t, r.do(http.MethodPost, path, "application/json", origin, goodJSON),
			502, `{"ok":false,"error":"send_failed"}`)
	})
}

func TestV2_TooLarge(t *testing.T) {
	big := `{"name":"Ana","message":"` + strings.Repeat("a", 65*1024) + `"}`
	assertBody(t, newRig(t).do(http.MethodPost, path, "application/json", origin, big),
		413, `{"ok":false,"error":"too_large"}`)
	form := "message=" + strings.Repeat("a", 65*1024)
	assertBody(t, newRig(t).do(http.MethodPost, path, "application/x-www-form-urlencoded", origin, form),
		413, `{"ok":false,"error":"too_large"}`)
}

func TestV2_UnsupportedMediaType(t *testing.T) {
	assertBody(t, newRig(t).do(http.MethodPost, path, "text/plain", origin, goodJSON),
		415, `{"ok":false,"error":"unsupported_media_type"}`)
}

func TestV2_MalformedJSONIsEmptySubmission(t *testing.T) {
	rec := newRig(t).do(http.MethodPost, path, "application/json", origin, `{not json`)
	if rec.Code != 422 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestV2_CORSPreflight(t *testing.T) {
	r := newRig(t)
	rec := r.do(http.MethodOptions, path, "", origin, "")
	if rec.Code != 204 {
		t.Fatalf("status = %d", rec.Code)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":  origin,
		"Access-Control-Allow-Methods": "POST",
		"Access-Control-Allow-Headers": "Content-Type",
		"Vary":                         "Origin",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}

	for _, p := range []string{path, "/api/v2/sites/nope/forms/x"} {
		rec = r.do(http.MethodOptions, p, "", "https://evil.example", "")
		if rec.Code != 204 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("%s: status %d, ACAO %q", p, rec.Code, rec.Header().Get("Access-Control-Allow-Origin"))
		}
	}
}
