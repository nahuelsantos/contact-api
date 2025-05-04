package mail

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nahuelsantos/mail-api/internal/config"
	"github.com/nahuelsantos/mail-api/internal/handlers"
)

// Run starts the mail API server
func Run() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Create API handlers
	api := handlers.New(cfg)

	// Set up routes
	mux := http.NewServeMux()
	mux.HandleFunc("/health", api.HealthCheck)
	mux.HandleFunc("/send", api.EmailHandler)
	mux.HandleFunc("/contact", api.ContactHandler)

	// Get port from environment variable or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "20001"
	}

	// Create a server with timeouts
	server := &http.Server{
		Addr:         ":" + port,
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start the server in a goroutine
	go func() {
		log.Printf("Starting mail API server on port %s", port)
		log.Printf("Configured to use SMTP server at %s:%s", cfg.SMTPHost, cfg.SMTPPort)

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Set up graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exited gracefully")
}

//nolint:unused // This is used when running the package directly
func main() {
	// Only run when directly invoked
	Run()
}
