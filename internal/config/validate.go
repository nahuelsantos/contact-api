package config

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

var (
	knownFieldTypes = []string{"text", "textarea", "email", "tel", "number", "select", "checkbox", "country"}
	knownTLS        = []string{"none", "starttls", "implicit"}
	// fieldNameRe matches what a {{ placeholder }} can reference.
	fieldNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)
)

// TurnstileField is posted by the Turnstile widget and never a form field.
const TurnstileField = "cf-turnstile-response"

// problems collects validation messages.
type problems []error

func (p *problems) addf(format string, args ...any) {
	*p = append(*p, fmt.Errorf(format, args...)) //nolint:err113 // messages are built from config values
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}

// validate reports every problem in the config, joined into one error.
func validate(c *Config, getenv func(string) string, templateExists func(string) bool) error {
	var p problems

	for _, name := range sortedKeys(c.Providers) {
		validateProvider(&p, name, c.Providers[name], getenv)
	}
	c.checkProviderRef(&p, "defaults", c.Defaults.Provider)
	c.checkProviderRef(&p, "defaults", c.Defaults.Fallback)

	for _, sn := range sortedKeys(c.Sites) {
		c.validateSite(&p, c.Sites[sn], getenv, templateExists)
	}

	return errors.Join(p...)
}

func (c *Config) checkProviderRef(p *problems, where, name string) {
	if name == "" {
		return
	}
	if _, ok := c.Providers[name]; !ok {
		p.addf("%s: unknown provider %q", where, name)
	}
}

func validateProvider(p *problems, name string, pc ProviderConfig, getenv func(string) string) {
	switch pc.Type {
	case "smtp":
		if pc.Host == "" {
			p.addf("provider %q: host is required", name)
		}
		if pc.TLS != "" && !slices.Contains(knownTLS, pc.TLS) {
			p.addf("provider %q: tls must be none, starttls or implicit", name)
		}
	case "resend":
		if pc.APIKeyEnv == "" {
			p.addf("provider %q: api_key_env is required", name)
		}
	default:
		p.addf("provider %q: unknown provider type %q", name, pc.Type)
	}
	for _, env := range []string{pc.APIKeyEnv, pc.UsernameEnv, pc.PasswordEnv} {
		if env != "" && getenv(env) == "" {
			p.addf("provider %q: environment variable %s is empty", name, env)
		}
	}
}

func (c *Config) validateSite(p *problems, s *Site, getenv func(string) string, templateExists func(string) bool) {
	where := fmt.Sprintf("site %q", s.Name)

	if s.From == "" {
		p.addf("%s: from is required", where)
	}
	if len(s.Origins) == 0 {
		p.addf("%s: origins is required", where)
	}
	c.checkProviderRef(p, where, s.Provider)
	c.checkProviderRef(p, where, s.Fallback)
	if primary, _ := c.ProvidersFor(s); primary == "" {
		p.addf("%s: no provider configured", where)
	}
	if s.TurnstileSecretEnv != "" && getenv(s.TurnstileSecretEnv) == "" {
		p.addf("%s: environment variable %s is empty", where, s.TurnstileSecretEnv)
	}
	if len(s.Forms) == 0 {
		p.addf("%s: at least one form is required", where)
	}

	for _, fn := range sortedKeys(s.Forms) {
		validateForm(p, s, s.Forms[fn], templateExists)
	}
}

func validateForm(p *problems, s *Site, f *Form, templateExists func(string) bool) {
	where := fmt.Sprintf("site %q form %q", s.Name, f.Name)

	if len(s.Recipients(f)) == 0 {
		p.addf("%s: no recipients", where)
	}
	if f.Template != "" && !templateExists(f.Template) {
		p.addf("%s: template %q not found", where, f.Template)
	}
	if f.Rate != nil && (f.Rate.Count <= 0 || f.Rate.Per <= 0) {
		p.addf("%s: rate needs a positive count and per", where)
	}

	names := validateFields(p, where, f.Fields)

	checkPlaceholders(p, where, "subject", f.Subject, names)
	if f.AutoReply != nil {
		if s.TurnstileSecretEnv == "" {
			p.addf("%s: auto_reply requires turnstile (set turnstile_secret_env on the site)", where)
		}
		hasEmail := false
		for _, fld := range f.Fields {
			hasEmail = hasEmail || fld.Type == "email"
		}
		if !hasEmail {
			p.addf("%s: auto_reply needs an email field", where)
		}
		checkPlaceholders(p, where, "auto_reply subject", f.AutoReply.Subject, names)
		checkPlaceholders(p, where, "auto_reply body", f.AutoReply.Body, names)
	}
}

func checkPlaceholders(p *problems, where, what, text string, fields map[string]bool) {
	for _, ph := range Placeholders(text) {
		if !fields[ph] {
			p.addf("%s: %s: placeholder {{%s}} is not a field", where, what, ph)
		}
	}
}

func validateFields(p *problems, where string, fields []Field) map[string]bool {
	seen := make(map[string]bool, len(fields))
	for _, fld := range fields {
		fw := fmt.Sprintf("%s: field %q", where, fld.Name)

		switch {
		case fld.Name == "":
			p.addf("%s: field name is required", where)
		case strings.HasPrefix(fld.Name, "_"):
			p.addf("%s: names starting with _ are reserved", fw)
		case fld.Name == TurnstileField:
			p.addf("%s: name is reserved", fw)
		case !fieldNameRe.MatchString(fld.Name):
			p.addf("%s: names must match %s", fw, fieldNameRe)
		case seen[fld.Name]:
			p.addf("%s appears twice", fw)
		}
		seen[fld.Name] = true

		if !slices.Contains(knownFieldTypes, fld.Type) {
			p.addf("%s: unknown type %q", fw, fld.Type)
		}
		if fld.Type == "select" && len(fld.Options) == 0 {
			p.addf("%s: select needs options", fw)
		}
		if fld.Pattern != "" {
			if _, err := regexp.Compile(fld.Pattern); err != nil {
				p.addf("%s: invalid pattern: %v", fw, err)
			}
		}
	}

	return seen
}
