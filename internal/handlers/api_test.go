package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nahuelsantos/mail-api/internal/config"
	"github.com/nahuelsantos/mail-api/internal/email"
)

// MockEmailService is a mock implementation of the email.EmailService interface
type MockEmailService struct {
	SendFunc func(req email.Request, cfg config.Config) error
}

func (m *MockEmailService) Send(req email.Request, cfg config.Config) error {
	if m.SendFunc != nil {
		return m.SendFunc(req, cfg)
	}
	return nil
}

// Save the original smtp dialer function
var originalDialer = email.DefaultSMTPDialer

// setupMockDialer sets up a mock SMTP dialer that returns a mock SMTP client
func setupMockDialer(shouldFail bool) {
	if shouldFail {
		email.DefaultSMTPDialer = func(addr string) (email.SMTPClient, error) {
			return nil, errors.New("mock dialer error")
		}
	} else {
		email.DefaultSMTPDialer = func(addr string) (email.SMTPClient, error) {
			return &email.MockSMTPClient{
				MailFunc: func(from string) error { return nil },
				RcptFunc: func(to string) error { return nil },
				DataFunc: func() (io.WriteCloser, error) {
					return &email.MockWriteCloser{}, nil
				},
				QuitFunc: func() error { return nil },
			}, nil
		}
	}
}

// restoreOriginalDialer restores the original SMTP dialer
func restoreOriginalDialer() {
	email.DefaultSMTPDialer = originalDialer
}

func TestHealthCheck(t *testing.T) {
	// Create a new config
	cfg := config.Config{}

	// Create an API instance
	api := New(cfg)

	// Create a request
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	// Call the function under test
	api.HealthCheck(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	// Check JSON response
	var response Response
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to unmarshal response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success=true, got success=%v", response.Success)
	}

	if response.Message != "Mail API service is running" {
		t.Errorf("Expected message='Mail API service is running', got message='%s'", response.Message)
	}
}

func TestEmailHandler(t *testing.T) {
	// Create a new config with max body size to avoid request body limit error
	cfg := config.Config{
		DefaultFrom: "test@example.com",
		DefaultTo:   "recipient@example.com",
		MaxBodySize: 1024 * 1024, // 1MB
	}

	testCases := []struct {
		name           string
		method         string
		requestBody    interface{}
		expectedStatus int
		setupMock      func()
	}{
		{
			name:           "Method not allowed",
			method:         http.MethodGet,
			expectedStatus: http.StatusMethodNotAllowed,
			setupMock: func() {
				// No mock needed for this test
			},
		},
		{
			name:           "Invalid request - missing body",
			method:         http.MethodPost,
			expectedStatus: http.StatusBadRequest,
			setupMock: func() {
				// No mock needed for this test
			},
		},
		{
			name:   "Invalid request - malformed JSON",
			method: http.MethodPost,
			requestBody: map[string]string{
				"invalid": "data",
			},
			expectedStatus: http.StatusBadRequest,
			setupMock: func() {
				// No mock needed for this test
			},
		},
		{
			name:   "Email error",
			method: http.MethodPost,
			requestBody: email.Request{
				Subject: "Test Subject",
				Body:    "Test Body",
				From:    "test@example.com",
				To:      "recipient@example.com",
			},
			expectedStatus: http.StatusInternalServerError,
			setupMock: func() {
				setupMockDialer(true) // Set up mock dialer to fail
			},
		},
		{
			name:   "Successful email",
			method: http.MethodPost,
			requestBody: email.Request{
				Subject: "Test Subject",
				Body:    "Test Body",
				From:    "test@example.com",
				To:      "recipient@example.com",
			},
			expectedStatus: http.StatusOK,
			setupMock: func() {
				setupMockDialer(false) // Set up mock dialer to succeed
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mock for this test case
			tc.setupMock()
			defer restoreOriginalDialer()

			// Create an API instance
			api := New(cfg)

			var reqBody io.Reader
			if tc.requestBody != nil {
				jsonBody, _ := json.Marshal(tc.requestBody)
				reqBody = bytes.NewBuffer(jsonBody)
			}

			// Create a request
			req := httptest.NewRequest(tc.method, "/api/email", reqBody)
			w := httptest.NewRecorder()

			// Call the function under test
			api.EmailHandler(w, req)

			// Check response
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tc.expectedStatus, w.Code)
			}

			// If we expect OK status, check JSON response
			if tc.expectedStatus == http.StatusOK {
				var response Response
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}

				if !response.Success {
					t.Errorf("Expected success=true, got success=%v", response.Success)
				}
			}
		})
	}
}

func TestContactHandler(t *testing.T) {
	// Create a new config with max body size to avoid request body limit error
	cfg := config.Config{
		DefaultFrom: "test@example.com",
		DefaultTo:   "recipient@example.com",
		MaxBodySize: 1024 * 1024, // 1MB
	}

	testCases := []struct {
		name           string
		method         string
		requestBody    interface{}
		expectedStatus int
		setupMock      func()
	}{
		{
			name:           "Method not allowed",
			method:         http.MethodGet,
			expectedStatus: http.StatusMethodNotAllowed,
			setupMock: func() {
				// No mock needed for this test
			},
		},
		{
			name:           "Invalid request - missing body",
			method:         http.MethodPost,
			expectedStatus: http.StatusBadRequest,
			setupMock: func() {
				// No mock needed for this test
			},
		},
		{
			name:   "Invalid request - malformed JSON",
			method: http.MethodPost,
			requestBody: map[string]string{
				"invalid": "data",
			},
			expectedStatus: http.StatusBadRequest,
			setupMock: func() {
				// No mock needed for this test
			},
		},
		{
			name:   "Email error",
			method: http.MethodPost,
			requestBody: ContactFormData{
				Name:    "Test User",
				Email:   "user@example.com",
				Subject: "Test Subject",
				Message: "Test Message",
			},
			expectedStatus: http.StatusInternalServerError,
			setupMock: func() {
				setupMockDialer(true) // Set up mock dialer to fail
			},
		},
		{
			name:   "Successful contact form",
			method: http.MethodPost,
			requestBody: ContactFormData{
				Name:    "Test User",
				Email:   "user@example.com",
				Subject: "Test Subject",
				Message: "Test Message",
			},
			expectedStatus: http.StatusOK,
			setupMock: func() {
				setupMockDialer(false) // Set up mock dialer to succeed
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Set up mock for this test case
			tc.setupMock()
			defer restoreOriginalDialer()

			// Create an API instance
			api := New(cfg)

			var reqBody io.Reader
			if tc.requestBody != nil {
				jsonBody, _ := json.Marshal(tc.requestBody)
				reqBody = bytes.NewBuffer(jsonBody)
			}

			// Create a request
			req := httptest.NewRequest(tc.method, "/api/contact", reqBody)
			w := httptest.NewRecorder()

			// Call the function under test
			api.ContactHandler(w, req)

			// Check response
			if w.Code != tc.expectedStatus {
				t.Errorf("Expected status code %d, got %d", tc.expectedStatus, w.Code)
			}

			// If we expect OK status, check JSON response
			if tc.expectedStatus == http.StatusOK {
				var response Response
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to unmarshal response: %v", err)
				}

				if !response.Success {
					t.Errorf("Expected success=true, got success=%v", response.Success)
				}
			}
		})
	}
}
