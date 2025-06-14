package email

import (
	"errors"
	"io"
	"testing"

	"github.com/nahuelsantos/contact-api/internal/config"
)

func TestService_Send(t *testing.T) {
	tests := []struct {
		name          string
		request       Request
		config        config.Config
		mockDialer    func(addr string) (SMTPClient, error)
		expectedError bool
	}{
		{
			name: "Success",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				dataWriter := &MockWriteCloser{}
				return &MockSMTPClient{
					MailFunc: func(from string) error { return nil },
					RcptFunc: func(to string) error { return nil },
					DataFunc: func() (io.WriteCloser, error) { return dataWriter, nil },
					QuitFunc: func() error { return nil },
				}, nil
			},
			expectedError: false,
		},
		{
			name: "Connection Error",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				return nil, errors.New("connection error")
			},
			expectedError: true,
		},
		{
			name: "Mail From Error",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				return &MockSMTPClient{
					MailFunc: func(from string) error { return errors.New("mail from error") },
				}, nil
			},
			expectedError: true,
		},
		{
			name: "Rcpt To Error",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				return &MockSMTPClient{
					MailFunc: func(from string) error { return nil },
					RcptFunc: func(to string) error { return errors.New("rcpt to error") },
				}, nil
			},
			expectedError: true,
		},
		{
			name: "Data Error",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				return &MockSMTPClient{
					MailFunc: func(from string) error { return nil },
					RcptFunc: func(to string) error { return nil },
					DataFunc: func() (io.WriteCloser, error) { return nil, errors.New("data error") },
				}, nil
			},
			expectedError: true,
		},
		{
			name: "Write Error",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				writer := &MockWriteCloser{
					WriteFunc: func(p []byte) (n int, err error) { return 0, errors.New("write error") },
				}
				return &MockSMTPClient{
					MailFunc: func(from string) error { return nil },
					RcptFunc: func(to string) error { return nil },
					DataFunc: func() (io.WriteCloser, error) { return writer, nil },
				}, nil
			},
			expectedError: true,
		},
		{
			name: "HTML Content Type",
			request: Request{
				From:    "sender@example.com",
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "<h1>This is a test email</h1>",
				HTML:    true,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				writer := &MockWriteCloser{}
				return &MockSMTPClient{
					MailFunc: func(from string) error { return nil },
					RcptFunc: func(to string) error { return nil },
					DataFunc: func() (io.WriteCloser, error) { return writer, nil },
					QuitFunc: func() error { return nil },
				}, nil
			},
			expectedError: false,
		},
		{
			name: "Use Default From Address",
			request: Request{
				To:      "recipient@example.com",
				Subject: "Test Email",
				Body:    "This is a test email",
				HTML:    false,
			},
			config: config.Config{
				SMTPHost:    "mail-server",
				SMTPPort:    "25",
				DefaultFrom: "noreply@dinky.local",
			},
			mockDialer: func(addr string) (SMTPClient, error) {
				return &MockSMTPClient{
					MailFunc: func(from string) error {
						if from != "noreply@dinky.local" {
							return errors.New("wrong from address")
						}
						return nil
					},
					RcptFunc: func(to string) error { return nil },
					DataFunc: func() (io.WriteCloser, error) { return &MockWriteCloser{}, nil },
					QuitFunc: func() error { return nil },
				}, nil
			},
			expectedError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Create a new service with the mock dialer
			service := NewService(tc.mockDialer)

			// Call the function under test
			err := service.Send(tc.request, tc.config)

			// Check results
			if tc.expectedError && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !tc.expectedError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}

			// Test HTML content type is set properly
			if tc.request.HTML {
				// This is just a placeholder for future verification
				// We could extend the mock to capture the email content and check content type
				_ = "text/html" // Just to make the condition non-empty
			}
		})
	}
}

// Test the package-level Send function for backward compatibility
func TestSend(t *testing.T) {
	// Setup a simple mock
	var called bool

	// Save the original function
	originalDialer := DefaultSMTPDialer

	// Create a temporary replacement for testing
	mockDialer := func(addr string) (SMTPClient, error) {
		called = true
		return &MockSMTPClient{}, nil
	}

	// Replace the function temporarily
	DefaultSMTPDialer = mockDialer

	// Restore the original function after the test
	defer func() { DefaultSMTPDialer = originalDialer }()

	// Call the function
	err := Send(Request{
		To:      "test@example.com",
		Subject: "Test",
		Body:    "Test",
	}, config.Config{
		SMTPHost:    "testhost",
		SMTPPort:    "25",
		DefaultFrom: "from@example.com",
	})

	// Assertions
	if err != nil {
		t.Errorf("expected no error but got: %v", err)
	}
	if !called {
		t.Error("DefaultSMTPDialer was not called")
	}
}
