package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func envMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func alwaysTrue(string) bool { return true }

func writeTOML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "forms.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return p
}

func TestLoad_Dev(t *testing.T) {
	cfg, err := Load("../../config/dev.toml", envMap(nil), alwaysTrue)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p := cfg.Providers["melee"]; p.Host != "mail-server" || p.Port != 1025 {
		t.Fatalf("dev smtp provider = %+v", p)
	}
	if _, _, ok := cfg.Lookup("example", "contact"); !ok {
		t.Fatal("example/contact not found")
	}
}

func TestLoad_Example(t *testing.T) {
	cfg, err := Load("../../config/example.toml", envMap(nil), alwaysTrue)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.Port != 3002 || cfg.Server.MaxBodyKB != 64 {
		t.Fatalf("server defaults = %+v", cfg.Server)
	}
	_, form, ok := cfg.Lookup("example", "contact")
	if !ok {
		t.Fatal("example/contact not found")
	}
	for _, f := range form.Fields {
		want := 500
		if f.Name == "message" {
			want = 10000
		}
		if f.MaxLen != want {
			t.Errorf("field %s MaxLen = %d, want %d", f.Name, f.MaxLen, want)
		}
	}
	if got, want := cfg.RateFor(form), (Rate{Count: 5, Per: Duration(10 * time.Minute)}); got != want {
		t.Fatalf("RateFor = %+v, want %+v", got, want)
	}
	if form.Rate != nil {
		t.Fatal("form without rate must have nil Rate")
	}
}

const base = `
[provider.p]
type = "smtp"
host = "h"
port = 25
tls = "none"
[defaults]
provider = "p"
`

func TestLoad_FormRateOverrides(t *testing.T) {
	p := writeTOML(t, base+`
[site.s]
from = "a@b.c"
to = ["o@b.c"]
origins = ["https://b.c"]
[site.s.form.f]
rate = { count = 3, per = "1h" }
[[site.s.form.f.fields]]
name = "name"
type = "text"
`)
	cfg, err := Load(p, envMap(nil), alwaysTrue)
	if err != nil {
		t.Fatal(err)
	}
	_, f, _ := cfg.Lookup("s", "f")
	if got, want := cfg.RateFor(f), (Rate{Count: 3, Per: Duration(time.Hour)}); got != want {
		t.Fatalf("RateFor = %+v, want %+v", got, want)
	}
}

func TestLoad_ResolvesSecrets(t *testing.T) {
	p := writeTOML(t, `
[provider.resend]
type = "resend"
api_key_env = "RESEND_API_KEY"
[defaults]
provider = "resend"
[site.s]
from = "a@b.c"
to = ["o@b.c"]
origins = ["https://b.c"]
turnstile_secret_env = "TS"
[site.s.form.f]
[[site.s.form.f.fields]]
name = "name"
type = "text"
`)
	cfg, err := Load(p, envMap(map[string]string{"RESEND_API_KEY": "k", "TS": "t"}), alwaysTrue)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Providers["resend"].APIKey != "k" {
		t.Fatalf("APIKey = %q", cfg.Providers["resend"].APIKey)
	}
	if cfg.Sites["s"].TurnstileSecret != "t" {
		t.Fatalf("TurnstileSecret = %q", cfg.Sites["s"].TurnstileSecret)
	}
}

func TestRecipients_FormOverridesSite(t *testing.T) {
	s := &Site{To: []string{"site@x"}}
	if got := s.Recipients(&Form{}); !reflect.DeepEqual(got, []string{"site@x"}) {
		t.Fatalf("got %v", got)
	}
	if got := s.Recipients(&Form{To: []string{"form@x"}}); !reflect.DeepEqual(got, []string{"form@x"}) {
		t.Fatalf("got %v", got)
	}
}

func TestProvidersFor_SiteOverridesDefaults(t *testing.T) {
	c := &Config{Defaults: Defaults{Provider: "a", Fallback: "b"}}
	if p, f := c.ProvidersFor(&Site{}); p != "a" || f != "b" {
		t.Fatalf("got %s %s", p, f)
	}
	if p, f := c.ProvidersFor(&Site{Provider: "x", Fallback: "y"}); p != "x" || f != "y" {
		t.Fatalf("got %s %s", p, f)
	}
}

func TestPlaceholders(t *testing.T) {
	got := Placeholders("Hola {{nombre}} {{ apellido }}")
	if !reflect.DeepEqual(got, []string{"nombre", "apellido"}) {
		t.Fatalf("got %v", got)
	}
}

func TestDuration_Zero(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText(nil); err != nil || d != 0 {
		t.Fatalf("d=%v err=%v", d, err)
	}
	if err := d.UnmarshalText([]byte("bogus")); err == nil {
		t.Fatal("expected error")
	}
}

func TestReplacePlaceholders(t *testing.T) {
	got := ReplacePlaceholders("Hola {{nombre}} {{ apellido }} {{x}}", func(n string) string {
		if n == "x" {
			return ""
		}

		return strings.ToUpper(n)
	})
	if want := "Hola NOMBRE APELLIDO "; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
