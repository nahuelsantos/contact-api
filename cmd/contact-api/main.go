// Package main is the contact-api entrypoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/guard"
	"github.com/nahuelsantos/contact-api/internal/handlers"
	"github.com/nahuelsantos/contact-api/internal/mail"
	"github.com/nahuelsantos/contact-api/internal/mail/resend"
	"github.com/nahuelsantos/contact-api/internal/mail/smtp"
	"github.com/nahuelsantos/contact-api/internal/observability"
	"github.com/nahuelsantos/contact-api/internal/render"
	"github.com/nahuelsantos/contact-api/internal/submit"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	defaultConfigPath = "/etc/contact-api/forms.toml"
	serviceName       = "contact-api"
	readTimeout       = 30 * time.Second
	// writeTimeout covers detached delivery: up to 25 s for the notification
	// chain plus 20 s for the auto-reply (internal/submit).
	writeTimeout    = 50 * time.Second
	idleTimeout     = 120 * time.Second
	shutdownTimeout = 10 * time.Second
)

// sha is the build revision, set with -X main.sha=<git sha>.
var sha = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		for _, e := range flatten(err) {
			slog.Error("startup failed", "error", e)
		}
		os.Exit(1)
	}
}

func run() error {
	path := os.Getenv("CONFIG_FILE")
	if path == "" {
		path = defaultConfigPath
	}

	renderer, err := render.New(os.Getenv("TEMPLATES_DIR"))
	if err != nil {
		return fmt.Errorf("templates: %w", err)
	}
	cfg, err := config.Load(path, os.Getenv, renderer.Has)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	providers, err := mail.Build(cfg.Providers, map[string]mail.Factory{
		"smtp": func(name string, c config.ProviderConfig) (mail.Provider, error) {
			return smtp.New(name, c), nil
		},
		"resend": func(name string, c config.ProviderConfig) (mail.Provider, error) {
			return resend.New(name, c.APIKey), nil
		},
	})
	if err != nil {
		return fmt.Errorf("providers: %w", err)
	}

	shutdownTracing, err := observability.InitTracing(serviceName, sha)
	if err != nil {
		slog.Warn("tracing disabled", "error", err)
		shutdownTracing = func() {}
	}
	defer shutdownTracing()

	metrics := observability.NewMetrics(prometheus.DefaultRegisterer)
	metrics.Preinit(cfg)

	svc := &submit.Service{
		Config: cfg, Providers: providers, Renderer: renderer,
		Limiter:   guard.NewLimiter(time.Now),
		Turnstile: guard.NewTurnstile(nil, ""),
		Metrics:   metrics,
		Log:       slog.Default(),
	}

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      handlers.NewRouter(svc, cfg, handlers.BuildInfo{SHA: sha}),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	return serve(srv)
}

// serve runs srv until it fails or the process is asked to stop.
func serve(srv *http.Server) error {
	errCh := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", srv.Addr, "sha", sha)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

// flatten splits an errors.Join result into its individual problems.
func flatten(err error) []error {
	var out []error
	var walk func(error)
	walk = func(e error) {
		if j, ok := e.(interface{ Unwrap() []error }); ok { //nolint:errorlint // joined errors are not wrapped further
			for _, c := range j.Unwrap() {
				walk(c)
			}
			return
		}
		out = append(out, e)
	}
	walk(err)

	return out
}
