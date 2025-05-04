package config

import (
	"os"
	"testing"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name              string
		envVars           map[string]string
		expectedSMTPHost  string
		expectedSMTPPort  string
		expectedFromEmail string
		expectedToEmail   string
	}{
		{
			name:              "Default values",
			envVars:           map[string]string{},
			expectedSMTPHost:  "mail-server",
			expectedSMTPPort:  "25",
			expectedFromEmail: "noreply@dinky.local",
			expectedToEmail:   "noreply@dinky.local",
		},
		{
			name: "Custom environment variables",
			envVars: map[string]string{
				"SMTP_HOST":     "custom-smtp.example.com",
				"SMTP_PORT":     "587",
				"DEFAULT_FROM":  "sender@example.com",
				"DEFAULT_TO":    "recipient@example.com",
				"ALLOWED_HOSTS": "example.com,test.com",
			},
			expectedSMTPHost:  "custom-smtp.example.com",
			expectedSMTPPort:  "587",
			expectedFromEmail: "sender@example.com",
			expectedToEmail:   "recipient@example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Clear environment variables
			os.Unsetenv("SMTP_HOST")
			os.Unsetenv("SMTP_PORT")
			os.Unsetenv("DEFAULT_FROM")
			os.Unsetenv("DEFAULT_TO")
			os.Unsetenv("ALLOWED_HOSTS")

			// Set environment variables for this test
			for key, value := range tc.envVars {
				os.Setenv(key, value)
			}

			// Call the function under test
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() failed with error: %v", err)
			}

			// Check results
			if cfg.SMTPHost != tc.expectedSMTPHost {
				t.Errorf("SMTPHost = %q, want %q", cfg.SMTPHost, tc.expectedSMTPHost)
			}

			if cfg.SMTPPort != tc.expectedSMTPPort {
				t.Errorf("SMTPPort = %q, want %q", cfg.SMTPPort, tc.expectedSMTPPort)
			}

			if cfg.DefaultFrom != tc.expectedFromEmail {
				t.Errorf("DefaultFrom = %q, want %q", cfg.DefaultFrom, tc.expectedFromEmail)
			}

			if cfg.DefaultTo != tc.expectedToEmail {
				t.Errorf("DefaultTo = %q, want %q", cfg.DefaultTo, tc.expectedToEmail)
			}

			// Check if the allowed hosts were set correctly
			if allowedHosts, ok := tc.envVars["ALLOWED_HOSTS"]; ok && len(cfg.AllowedHosts) == 0 {
				t.Errorf("AllowedHosts is empty, expected to contain %q", allowedHosts)
			}
		})
	}
}
