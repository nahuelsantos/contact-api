package config

import (
	"strings"
	"testing"
)

const okProvider = `
[provider.p]
type = "smtp"
host = "h"
port = 25
tls = "none"
[defaults]
provider = "p"
`

const okSite = `
[site.x]
from = "a@b.c"
to = ["o@b.c"]
origins = ["https://b.c"]
`

const nameField = `
[[site.x.form.f.fields]]
name = "name"
type = "text"
`

func TestLoad_RejectsInvalid(t *testing.T) {
	tests := []struct {
		name string
		toml string
		env  map[string]string
		want string
	}{
		{"unknown key", okProvider + okSite + "colour = \"red\"\n[site.x.form.f]" + nameField, nil, "colour"},
		{"empty api key env", `
[provider.r]
type = "resend"
api_key_env = "RESEND_API_KEY"
[defaults]
provider = "r"
` + okSite + "[site.x.form.f]" + nameField, nil, "RESEND_API_KEY is empty"},
		{"unknown default provider", strings.Replace(okProvider, `provider = "p"`, `provider = "nope"`, 1) + okSite + "[site.x.form.f]" + nameField, nil, `unknown provider "nope"`},
		{"unknown site fallback", okProvider + okSite + "fallback = \"nope\"\n[site.x.form.f]" + nameField, nil, `unknown provider "nope"`},
		{"no from", okProvider + `
[site.x]
to = ["o@b.c"]
origins = ["https://b.c"]
[site.x.form.f]` + nameField, nil, `site "x": from is required`},
		{"no recipients", okProvider + `
[site.x]
from = "a@b.c"
origins = ["https://b.c"]
[site.x.form.f]` + nameField, nil, `site "x" form "f": no recipients`},
		{"no origins", okProvider + `
[site.x]
from = "a@b.c"
to = ["o@b.c"]
[site.x.form.f]` + nameField, nil, `site "x": origins is required`},
		{"select no options", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "pais"
type = "select"
`, nil, `field "pais": select needs options`},
		{"bad pattern", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "cp"
type = "text"
pattern = "("
`, nil, `field "cp": invalid pattern`},
		{"duplicate field", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "email"
type = "email"
[[site.x.form.f.fields]]
name = "email"
type = "email"
`, nil, `field "email" appears twice`},
		{"reserved name", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "_x"
type = "text"
`, nil, `field "_x": names starting with _ are reserved`},
		{"turnstile field reserved", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "cf-turnstile-response"
type = "text"
`, nil, `field "cf-turnstile-response": name is reserved`},
		{"uppercase name", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "Email"
type = "text"
`, nil, `field "Email": names must match ^[a-z0-9_]+$`},
		{"dash in name", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "first-name"
type = "text"
`, nil, `field "first-name": names must match ^[a-z0-9_]+$`},
		{"space in name", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "first name"
type = "text"
`, nil, `field "first name": names must match ^[a-z0-9_]+$`},
		{"unknown type", okProvider + okSite + `[site.x.form.f]
[[site.x.form.f.fields]]
name = "d"
type = "date"
`, nil, `field "d": unknown type "date"`},
		{"bad placeholder", okProvider + okSite + "[site.x.form.f]\nsubject = \"hi {{nope}}\"" + nameField, nil, `placeholder {{nope}} is not a field`},
		{"auto_reply no turnstile", okProvider + okSite + `[site.x.form.f]
[site.x.form.f.auto_reply]
subject = "s"
body = "b"
[[site.x.form.f.fields]]
name = "email"
type = "email"
`, nil, `auto_reply requires turnstile`},
		{"auto_reply no email", okProvider + okSite + "turnstile_secret_env = \"TS\"\n[site.x.form.f]\n[site.x.form.f.auto_reply]\nsubject = \"s\"\nbody = \"b\"" + nameField, map[string]string{"TS": "t"}, `auto_reply needs an email field`},
		{"template missing", okProvider + okSite + "[site.x.form.f]\ntemplate = \"x\"" + nameField, nil, `template "x" not found`},
		{"bad tls", strings.Replace(okProvider, `tls = "none"`, `tls = "ssl"`, 1) + okSite + "[site.x.form.f]" + nameField, nil, `tls must be none, starttls or implicit`},
		{"bad provider type", strings.Replace(okProvider, `type = "smtp"`, `type = "mailgun"`, 1) + okSite + "[site.x.form.f]" + nameField, nil, `unknown provider type "mailgun"`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exists := func(n string) bool { return n != "x" }
			_, err := Load(writeTOML(t, tc.toml), envMap(tc.env), exists)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestLoad_ReportsAllProblems(t *testing.T) {
	body := okProvider + `
[site.x]
to = ["o@b.c"]
[site.x.form.f]
[[site.x.form.f.fields]]
name = "_x"
type = "date"
`
	_, err := Load(writeTOML(t, body), envMap(nil), alwaysTrue)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{`site "x": from is required`, `site "x": origins is required`, `field "_x": names starting with _ are reserved`, `unknown type "date"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("missing %q in %q", want, err)
		}
	}
}
