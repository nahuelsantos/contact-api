package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected Config
	}{
		{
			name:    "Default values",
			envVars: map[string]string{},
			expected: Config{
				SMTPHost:     "mail-server",
				SMTPPort:     "25",
				DefaultFrom:  "noreply@example.com",
				DefaultTo:    "contact@example.com",
				Port:         "3002",
				MaxBodySize:  1024 * 1024,
				AllowedHosts: []string{},
			},
		},
		{
			name: "Custom environment variables",
			envVars: map[string]string{
				"SMTP_HOST":     "custom-smtp",
				"SMTP_PORT":     "587",
				"DEFAULT_FROM":  "custom@example.com",
				"DEFAULT_TO":    "custom-to@example.com",
				"PORT":          "3002",
				"ALLOWED_HOSTS": "example.com",
			},
			expected: Config{
				SMTPHost:     "custom-smtp",
				SMTPPort:     "587",
				DefaultFrom:  "custom@example.com",
				DefaultTo:    "custom-to@example.com",
				Port:         "3002",
				MaxBodySize:  1024 * 1024,
				AllowedHosts: []string{"example.com"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment variables
			os.Clearenv()

			// Set test environment variables
			for key, value := range tt.envVars {
				os.Setenv(key, value)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() returned error: %v", err)
			}

			// Check individual fields
			if cfg.SMTPHost != tt.expected.SMTPHost {
				t.Errorf("SMTPHost = %q, want %q", cfg.SMTPHost, tt.expected.SMTPHost)
			}
			if cfg.SMTPPort != tt.expected.SMTPPort {
				t.Errorf("SMTPPort = %q, want %q", cfg.SMTPPort, tt.expected.SMTPPort)
			}
			if cfg.DefaultFrom != tt.expected.DefaultFrom {
				t.Errorf("DefaultFrom = %q, want %q", cfg.DefaultFrom, tt.expected.DefaultFrom)
			}
			if cfg.DefaultTo != tt.expected.DefaultTo {
				t.Errorf("DefaultTo = %q, want %q", cfg.DefaultTo, tt.expected.DefaultTo)
			}
			if cfg.Port != tt.expected.Port {
				t.Errorf("Port = %q, want %q", cfg.Port, tt.expected.Port)
			}
			if cfg.MaxBodySize != tt.expected.MaxBodySize {
				t.Errorf("MaxBodySize = %d, want %d", cfg.MaxBodySize, tt.expected.MaxBodySize)
			}
		})
	}
}
