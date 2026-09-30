package email

import (
	"fmt"
	"log"
	"net/smtp"
	"time"

	"github.com/nahuelsantos/contact-api/internal/config"
)

// Request represents an incoming request to send an email
type Request struct {
	From    string `json:"from"`
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
	HTML    bool   `json:"html"`
	// ReplyTo carries the visitor's address when From is the site's own sender,
	// so hitting reply still answers the person who filled in the form.
	ReplyTo string `json:"reply_to"`
}

// Service defines the operations for sending emails
type Service interface {
	Send(req Request, cfg config.Config) error
}

// SMTPDialer is a function type for creating SMTP clients
type SMTPDialer func(addr string) (SMTPClient, error)

// defaultSMTPDialFn is the standard implementation of SMTPDialer
func defaultSMTPDialFn(addr string) (SMTPClient, error) {
	client, err := smtp.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("dial smtp %s: %w", addr, err)
	}
	return client, nil
}

// DefaultSMTPDialer is the default dialer that can be replaced for testing
var DefaultSMTPDialer SMTPDialer = defaultSMTPDialFn

// ServiceImpl implements the Service interface
type ServiceImpl struct {
	smtpDialer SMTPDialer
}

// NewService creates a new email service with the given SMTP dialer
func NewService(dialer SMTPDialer) *ServiceImpl {
	if dialer == nil {
		dialer = DefaultSMTPDialer
	}
	return &ServiceImpl{smtpDialer: dialer}
}

// Send handles sending an email using the configured SMTP server
func (s *ServiceImpl) Send(req Request, cfg config.Config) error {
	// If From field is empty, use default
	if req.From == "" {
		req.From = cfg.DefaultFrom
	}

	// Headers, built in a fixed order: a Go map would shuffle them between sends,
	// and the first lines are what a strict reader trusts for the sender.
	var contentType string
	if req.HTML {
		contentType = "text/html; charset=UTF-8"
	} else {
		contentType = "text/plain; charset=UTF-8"
	}

	headers := [][2]string{
		{"From", req.From},
		{"To", req.To},
	}
	if req.ReplyTo != "" {
		headers = append(headers, [2]string{"Reply-To", req.ReplyTo})
	}
	headers = append(headers,
		[2]string{"Subject", req.Subject},
		[2]string{"Date", time.Now().Format(time.RFC1123Z)},
		[2]string{"MIME-Version", "1.0"},
		[2]string{"Content-Type", contentType},
	)

	// Compose the message
	message := ""
	for _, h := range headers {
		message += fmt.Sprintf("%s: %s\r\n", h[0], h[1])
	}
	message += "\r\n" + req.Body

	// Connect to the SMTP server
	addr := fmt.Sprintf("%s:%s", cfg.SMTPHost, cfg.SMTPPort)
	log.Printf("Attempting to send email via SMTP server: %s", addr)

	// Create client with TLS disabled
	client, err := s.smtpDialer(addr)
	if err != nil {
		log.Printf("SMTP connection error: %v", err)
		return fmt.Errorf("SMTP connection error: %w", err)
	}
	defer func() {
		if closeErr := client.Close(); closeErr != nil {
			log.Printf("Failed to close SMTP client: %v", closeErr)
		}
	}()

	// Set the sender and recipient. The envelope sender is req.From, which the
	// handler sets to the site's own address so the relay's SPF check passes.
	if err = client.Mail(req.From); err != nil {
		log.Printf("SMTP FROM error: %v", err)
		return fmt.Errorf("SMTP FROM error: %w", err)
	}
	if err = client.Rcpt(req.To); err != nil {
		log.Printf("SMTP RCPT error: %v", err)
		return fmt.Errorf("SMTP RCPT error: %w", err)
	}

	// Send the email body
	w, err := client.Data()
	if err != nil {
		log.Printf("SMTP DATA error: %v", err)
		return fmt.Errorf("SMTP DATA error: %w", err)
	}
	_, err = w.Write([]byte(message))
	if err != nil {
		log.Printf("SMTP write error: %v", err)
		return fmt.Errorf("SMTP write error: %w", err)
	}
	err = w.Close()
	if err != nil {
		log.Printf("SMTP close error: %v", err)
		return fmt.Errorf("SMTP close error: %w", err)
	}

	// Send the QUIT command and close the connection
	err = client.Quit()
	if err != nil {
		log.Printf("SMTP quit error: %v", err)
		return fmt.Errorf("SMTP quit error: %w", err)
	}

	log.Printf("Email sent successfully to %s", req.To)
	return nil
}

// Send is a package-level function that uses the default email service
// This is for backward compatibility
func Send(req Request, cfg config.Config) error {
	service := NewService(DefaultSMTPDialer)
	return service.Send(req, cfg)
}
