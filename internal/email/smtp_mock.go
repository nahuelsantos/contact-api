package email

import (
	"io"
	"net/smtp"
)

// MockSMTPClient implements a mock SMTP client for testing
type MockSMTPClient struct {
	DialFunc  func(addr string) (SMTPClient, error)
	MailFunc  func(from string) error
	RcptFunc  func(to string) error
	DataFunc  func() (io.WriteCloser, error)
	QuitFunc  func() error
	CloseFunc func() error
	AuthFunc  func(auth smtp.Auth) error
}

// Dial is a mock implementation of net/smtp.Dial
func (m *MockSMTPClient) Dial(addr string) (SMTPClient, error) {
	if m.DialFunc != nil {
		return m.DialFunc(addr)
	}
	return m, nil
}

// Mail is a mock implementation of smtp.Client.Mail
func (m *MockSMTPClient) Mail(from string) error {
	if m.MailFunc != nil {
		return m.MailFunc(from)
	}
	return nil
}

// Rcpt is a mock implementation of smtp.Client.Rcpt
func (m *MockSMTPClient) Rcpt(to string) error {
	if m.RcptFunc != nil {
		return m.RcptFunc(to)
	}
	return nil
}

// Data is a mock implementation of smtp.Client.Data
func (m *MockSMTPClient) Data() (io.WriteCloser, error) {
	if m.DataFunc != nil {
		return m.DataFunc()
	}
	return &MockWriteCloser{}, nil
}

// Quit is a mock implementation of smtp.Client.Quit
func (m *MockSMTPClient) Quit() error {
	if m.QuitFunc != nil {
		return m.QuitFunc()
	}
	return nil
}

// Close is a mock implementation of smtp.Client.Close
func (m *MockSMTPClient) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

// Auth is a mock implementation of smtp.Client.Auth
func (m *MockSMTPClient) Auth(auth smtp.Auth) error {
	if m.AuthFunc != nil {
		return m.AuthFunc(auth)
	}
	return nil
}

// MockWriteCloser implements a mock for io.WriteCloser
type MockWriteCloser struct {
	WriteFunc func(p []byte) (n int, err error)
	CloseFunc func() error
	Data      []byte
}

// Write is a mock implementation of io.Writer.Write
func (m *MockWriteCloser) Write(p []byte) (n int, err error) {
	if m.WriteFunc != nil {
		return m.WriteFunc(p)
	}
	m.Data = append(m.Data, p...)
	return len(p), nil
}

// Close is a mock implementation of io.Closer.Close
func (m *MockWriteCloser) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}
