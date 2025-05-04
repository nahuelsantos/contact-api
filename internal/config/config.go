package config

import (
	"os"
)

// Config holds the mail server configuration
type Config struct {
	SMTPHost     string   `json:"smtp_host"`
	SMTPPort     string   `json:"smtp_port"`
	DefaultFrom  string   `json:"default_from"`
	DefaultTo    string   `json:"default_to"`
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
		cfg.DefaultFrom = "noreply@dinky.local"
	}
	if cfg.DefaultTo == "" {
		cfg.DefaultTo = "noreply@dinky.local"
	}
	// Load allowed hosts from environment variable
	if allowedHosts := os.Getenv("ALLOWED_HOSTS"); allowedHosts != "" {
		cfg.AllowedHosts = append(cfg.AllowedHosts, allowedHosts)
	}

	return cfg, nil
}
