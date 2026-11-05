// Package main is the contact-api entrypoint.
package main

import (
	"errors"
	"log/slog"
	"os"

	"github.com/nahuelsantos/contact-api/internal/config"
)

const defaultConfigPath = "/etc/contact-api/forms.toml"

func main() {
	path := os.Getenv("CONFIG_FILE")
	if path == "" {
		path = defaultConfigPath
	}

	_, err := config.Load(path, os.Getenv, func(string) bool { return true })
	if err != nil {
		for _, e := range flatten(err) {
			slog.Error("config problem", "error", e)
		}
		os.Exit(1)
	}

	slog.Info("config ok")
}

// flatten splits an errors.Join result into its individual problems.
func flatten(err error) []error {
	var j interface{ Unwrap() []error }
	if errors.As(err, &j) {
		return j.Unwrap()
	}

	return []error{err}
}
