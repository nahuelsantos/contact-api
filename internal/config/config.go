package config

import (
	"os"
)

// Config holds the contact API server configuration
type Config struct {
	SMTPHost     string   `json:"smtp_host"`
	SMTPPort     string   `json:"smtp_port"`
	DefaultFrom  string   `json:"default_from"`
	DefaultTo    string   `json:"default_to"`
	Port         string   `json:"port"`
	MaxBodySize  int64    `json:"max_body_size"`
	AllowedHosts []string `json:"allowed_hosts"`
}

// Load initializes configuration from environment variables
func Load() (Config, error) {
	// Default configuration
	cfg := Config{
		SMTPHost:     os.Getenv("SMTP_HOST"),
		SMTPPort:     os.Getenv("SMTP_PORT"),
		DefaultFrom:  os.Getenv("DEFAULT_FROM"),
		DefaultTo:    os.Getenv("DEFAULT_TO"),
		Port:         os.Getenv("PORT"),
		MaxBodySize:  1024 * 1024, // 1MB
		AllowedHosts: []string{},
	}

	// If no environment variables, use defaults
	if cfg.SMTPHost == "" {
		cfg.SMTPHost = "mail-server"
	}
	if cfg.SMTPPort == "" {
		cfg.SMTPPort = "25"
	}
	if cfg.DefaultFrom == "" {
		cfg.DefaultFrom = "noreply@example.com"
	}
	if cfg.DefaultTo == "" {
		cfg.DefaultTo = "contact@example.com"
	}
	if cfg.Port == "" {
		cfg.Port = "3002"
	}

	// Load allowed hosts from environment variable
	if allowedHosts := os.Getenv("ALLOWED_HOSTS"); allowedHosts != "" {
		cfg.AllowedHosts = append(cfg.AllowedHosts, allowedHosts)
	}

	return cfg, nil
}
