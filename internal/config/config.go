// Package config loads and validates the TOML forms configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

const (
	defaultPort      = 3002
	defaultMaxBodyKB = 64
	defaultRateCount = 5
	defaultRatePer   = 10 * time.Minute
	defaultMaxLen    = 500
	textareaMaxLen   = 10000
)

// ErrUnknownKey is returned when the config contains a key that is not recognised.
var ErrUnknownKey = errors.New("unknown key in config")

// Duration is a time.Duration that decodes from strings such as "10m".
type Duration time.Duration

// UnmarshalText parses a duration string; empty input yields zero.
func (d *Duration) UnmarshalText(b []byte) error {
	if len(b) == 0 {
		*d = 0

		return nil
	}
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", b, err)
	}
	*d = Duration(v)

	return nil
}

// Rate limits submissions to Count per Per window.
type Rate struct {
	Count int      `toml:"count"`
	Per   Duration `toml:"per"`
}

// Server holds HTTP server settings.
type Server struct {
	Port      int `toml:"port"`
	MaxBodyKB int `toml:"max_body_kb"`
}

// Defaults are inherited by sites and forms that do not override them.
type Defaults struct {
	Provider string `toml:"provider"`
	Fallback string `toml:"fallback"`
	Rate     Rate   `toml:"rate"`
}

// ProviderConfig describes one mail provider.
type ProviderConfig struct {
	Type        string `toml:"type"`
	Host        string `toml:"host"`
	Port        int    `toml:"port"`
	TLS         string `toml:"tls"` // smtp: none|starttls|implicit
	UsernameEnv string `toml:"username_env"`
	PasswordEnv string `toml:"password_env"`
	APIKeyEnv   string `toml:"api_key_env"`

	Username string `toml:"-"`
	Password string `toml:"-"`
	APIKey   string `toml:"-"`
}

// Field is one form input.
type Field struct {
	Name     string   `toml:"name"`
	Label    string   `toml:"label"`
	Type     string   `toml:"type"`
	Pattern  string   `toml:"pattern"`
	Required bool     `toml:"required"`
	MaxLen   int      `toml:"max_len"`
	Options  []string `toml:"options"`

	Re *regexp.Regexp `toml:"-"`
}

// AutoReply is the confirmation mail sent to the submitter.
type AutoReply struct {
	Subject string `toml:"subject"`
	Body    string `toml:"body"`
}

// Form is one submittable form of a site.
type Form struct {
	Name      string     `toml:"-"`
	To        []string   `toml:"to"`
	Subject   string     `toml:"subject"`
	Rate      *Rate      `toml:"rate"`
	Template  string     `toml:"template"`
	AutoReply *AutoReply `toml:"auto_reply"`
	Fields    []Field    `toml:"fields"`
}

// Site groups forms served for one set of origins.
type Site struct {
	Name               string           `toml:"-"`
	From               string           `toml:"from"`
	To                 []string         `toml:"to"`
	Origins            []string         `toml:"origins"`
	Provider           string           `toml:"provider"`
	Fallback           string           `toml:"fallback"`
	TurnstileSecretEnv string           `toml:"turnstile_secret_env"`
	Forms              map[string]*Form `toml:"form"`

	TurnstileSecret string `toml:"-"`
}

// Config is the whole forms configuration.
type Config struct {
	Server    Server                    `toml:"server"`
	Defaults  Defaults                  `toml:"defaults"`
	Providers map[string]ProviderConfig `toml:"provider"`
	Sites     map[string]*Site          `toml:"site"`
}

// Load reads, defaults, resolves secrets for and validates the config at path.
func Load(path string, getenv func(string) string, templateExists func(string) bool) (*Config, error) {
	f, err := os.Open(path) //nolint:gosec // path comes from operator-controlled CONFIG_FILE
	if err != nil {
		return nil, fmt.Errorf("open config: %w", err)
	}
	defer func() { _ = f.Close() }()

	var cfg Config
	dec := toml.NewDecoder(f).DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		var sme *toml.StrictMissingError
		if errors.As(err, &sme) {
			return nil, fmt.Errorf("%w: %s", ErrUnknownKey, sme.String())
		}

		return nil, fmt.Errorf("parse config: %w", err)
	}

	cfg.applyDefaults(getenv)
	if err := validate(&cfg, getenv, templateExists); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func (c *Config) applyDefaults(getenv func(string) string) {
	if c.Server.Port == 0 {
		c.Server.Port = defaultPort
	}
	if c.Server.MaxBodyKB == 0 {
		c.Server.MaxBodyKB = defaultMaxBodyKB
	}
	if c.Defaults.Rate.Count == 0 {
		c.Defaults.Rate.Count = defaultRateCount
	}
	if c.Defaults.Rate.Per == 0 {
		c.Defaults.Rate.Per = Duration(defaultRatePer)
	}

	for name, p := range c.Providers {
		if p.UsernameEnv != "" {
			p.Username = getenv(p.UsernameEnv)
		}
		if p.PasswordEnv != "" {
			p.Password = getenv(p.PasswordEnv)
		}
		if p.APIKeyEnv != "" {
			p.APIKey = getenv(p.APIKeyEnv)
		}
		c.Providers[name] = p
	}

	for sn, s := range c.Sites {
		if s == nil {
			s = &Site{}
			c.Sites[sn] = s
		}
		s.Name = sn
		if s.TurnstileSecretEnv != "" {
			s.TurnstileSecret = getenv(s.TurnstileSecretEnv)
		}
		for fn, form := range s.Forms {
			if form == nil {
				form = &Form{}
				s.Forms[fn] = form
			}
			form.Name = fn
			for i := range form.Fields {
				fld := &form.Fields[i]
				if fld.MaxLen == 0 {
					fld.MaxLen = defaultMaxLen
					if fld.Type == "textarea" {
						fld.MaxLen = textareaMaxLen
					}
				}
				if fld.Pattern != "" {
					// Invalid patterns are reported by validate.
					if re, err := regexp.Compile(fld.Pattern); err == nil {
						fld.Re = re
					}
				}
			}
		}
	}
}

// Lookup finds a site and one of its forms.
func (c *Config) Lookup(site, form string) (*Site, *Form, bool) {
	s, ok := c.Sites[site]
	if !ok {
		return nil, nil, false
	}
	f, ok := s.Forms[form]
	if !ok {
		return nil, nil, false
	}

	return s, f, true
}

// RateFor returns the form's rate, else the default rate.
func (c *Config) RateFor(f *Form) Rate {
	if f != nil && f.Rate != nil {
		return *f.Rate
	}

	return c.Defaults.Rate
}

// ProvidersFor returns the site's primary and fallback providers, else the defaults.
func (c *Config) ProvidersFor(s *Site) (primary, fallback string) {
	primary, fallback = c.Defaults.Provider, c.Defaults.Fallback
	if s.Provider != "" {
		primary = s.Provider
	}
	if s.Fallback != "" {
		fallback = s.Fallback
	}

	return primary, fallback
}

// Recipients returns the form's recipients if set, else the site's.
func (s *Site) Recipients(f *Form) []string {
	if f != nil && len(f.To) > 0 {
		return f.To
	}

	return s.To
}
