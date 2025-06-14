package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/nahuelsantos/contact-api/internal/config"
)

func TestMain(m *testing.M) {
	gin.SetMode(gin.TestMode)
	os.Exit(m.Run())
}

func setupTestAPI() *gin.Engine {
	cfg := config.Config{
		SMTPHost:    "localhost",
		SMTPPort:    "1025",
		DefaultFrom: "test@example.com",
		DefaultTo:   "contact@example.com",
	}

	api := New(cfg)
	r := gin.New()

	// Setup routes
	v1 := r.Group("/api/v1")
	{
		v1.POST("/contact/:website", api.ContactHandler)
		v1.GET("/contact/:website/health", api.WebsiteHealthCheck)
	}
	r.GET("/health", api.HealthCheck)

	return r
}

func TestHealthCheck(t *testing.T) {
	r := setupTestAPI()

	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), "GET", "/health", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	var response Response
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Errorf("Error unmarshaling response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success to be true, got %v", response.Success)
	}

	expectedMessage := "Contact API service is running"
	if response.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, response.Message)
	}
}

func TestWebsiteHealthCheck(t *testing.T) {
	r := setupTestAPI()

	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), "GET", "/api/v1/contact/main/health", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, got %d", http.StatusOK, w.Code)
	}

	var response Response
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Errorf("Error unmarshaling response: %v", err)
	}

	if !response.Success {
		t.Errorf("Expected success to be true, got %v", response.Success)
	}

	// Check that data contains website info
	data, ok := response.Data.(map[string]interface{})
	if !ok {
		t.Errorf("Expected data to be a map, got %T", response.Data)
	}

	if data["website"] != "main" {
		t.Errorf("Expected website to be 'main', got %v", data["website"])
	}
}

func TestContactHandler_InvalidJSON(t *testing.T) {
	r := setupTestAPI()

	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), "POST", "/api/v1/contact/main", bytes.NewBufferString("invalid json"))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
	}

	var response Response
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Errorf("Error unmarshaling response: %v", err)
	}

	if response.Success {
		t.Errorf("Expected success to be false, got %v", response.Success)
	}
}

func TestContactHandler_MissingRequiredFields(t *testing.T) {
	r := setupTestAPI()

	tests := []struct {
		name string
		data ContactFormData
	}{
		{
			name: "missing name",
			data: ContactFormData{
				Email:   "john@example.com",
				Subject: "Test Subject",
				Message: "Test message",
			},
		},
		{
			name: "missing email",
			data: ContactFormData{
				Name:    "John Doe",
				Subject: "Test Subject",
				Message: "Test message",
			},
		},
		{
			name: "missing subject",
			data: ContactFormData{
				Name:    "John Doe",
				Email:   "john@example.com",
				Message: "Test message",
			},
		},
		{
			name: "missing message",
			data: ContactFormData{
				Name:    "John Doe",
				Email:   "john@example.com",
				Subject: "Test Subject",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tt.data)
			if err != nil {
				t.Fatalf("Failed to marshal JSON: %v", err)
			}
			w := httptest.NewRecorder()
			req, err := http.NewRequestWithContext(context.Background(), "POST", "/api/v1/contact/main", bytes.NewBuffer(jsonData))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("Expected status code %d, got %d", http.StatusBadRequest, w.Code)
			}

			var response Response
			err = json.Unmarshal(w.Body.Bytes(), &response)
			if err != nil {
				t.Errorf("Error unmarshaling response: %v", err)
			}

			if response.Success {
				t.Errorf("Expected success to be false, got %v", response.Success)
			}
		})
	}
}

func TestContactHandler_ValidRequest(t *testing.T) {
	r := setupTestAPI()

	contactForm := ContactFormData{
		Name:    "John Doe",
		Email:   "john@example.com",
		Subject: "Test Subject",
		Message: "Test message",
	}

	jsonData, err := json.Marshal(contactForm)
	if err != nil {
		t.Fatalf("Failed to marshal JSON: %v", err)
	}
	w := httptest.NewRecorder()
	req, err := http.NewRequestWithContext(context.Background(), "POST", "/api/v1/contact/main", bytes.NewBuffer(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Since we don't have a working SMTP server, we expect a 500 error
	// But the request should be properly validated
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status code %d, got %d", http.StatusInternalServerError, w.Code)
	}

	var response Response
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Errorf("Error unmarshaling response: %v", err)
	}

	if response.Success {
		t.Errorf("Expected success to be false due to SMTP error, got %v", response.Success)
	}

	expectedMessage := "Failed to send your message. Please try again later."
	if response.Message != expectedMessage {
		t.Errorf("Expected message '%s', got '%s'", expectedMessage, response.Message)
	}
}
