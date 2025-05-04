package email

import (
	"io"
	"net/smtp"
)

// SMTPClient defines the interface for SMTP operations
// This allows us to mock the SMTP client for testing
type SMTPClient interface {
	Mail(from string) error
	Rcpt(to string) error
	Data() (io.WriteCloser, error)
	Quit() error
	Close() error
	Auth(auth smtp.Auth) error
}
