package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/nahuelsantos/mail-api/internal/config"
	"github.com/nahuelsantos/mail-api/internal/email"
)

// Response represents the API response
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ContactFormData represents a contact form submission
type ContactFormData struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Subject string `json:"subject"`
	Message string `json:"message"`
}

// API holds handler dependencies
type API struct {
	Config config.Config
}

// New creates a new API handler with dependencies
func New(cfg config.Config) *API {
	return &API{
		Config: cfg,
	}
}

// encodeResponse writes a JSON response and logs any encoding error
func encodeResponse(w http.ResponseWriter, resp Response) {
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

// ContactHandler processes contact form submissions
func (a *API) ContactHandler(w http.ResponseWriter, r *http.Request) {
	// Set response content type
	w.Header().Set("Content-Type", "application/json")

	// Check if method is POST
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		encodeResponse(w, Response{
			Success: false,
			Message: "Only POST method is allowed",
		})
		return
	}

	// Limit the size of the request body
	r.Body = http.MaxBytesReader(w, r.Body, a.Config.MaxBodySize)

	// Decode the request body
	var contactForm ContactFormData
	err := json.NewDecoder(r.Body).Decode(&contactForm)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Invalid request format: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if contactForm.Name == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Name is required",
		})
		return
	}

	if contactForm.Email == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Email is required",
		})
		return
	}

	if contactForm.Subject == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Subject is required",
		})
		return
	}

	if contactForm.Message == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Message is required",
		})
		return
	}

	// Construct email from contact form
	emailReq := email.Request{
		From:    contactForm.Email,
		To:      a.Config.DefaultTo, // Send to the site admin
		Subject: fmt.Sprintf("Contact Form: %s", contactForm.Subject),
		Body:    formatContactEmail(contactForm),
		HTML:    true,
	}

	// Send the email
	err = email.Send(emailReq, a.Config)
	if err != nil {
		log.Printf("Error sending contact form email: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		encodeResponse(w, Response{
			Success: false,
			Message: "Failed to send your message. Please try again later.",
		})
		return
	}

	// Return success response
	encodeResponse(w, Response{
		Success: true,
		Message: "Your message has been sent successfully! We will get back to you soon.",
	})
}

// formatContactEmail formats the contact form data as an HTML email
func formatContactEmail(form ContactFormData) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #f5f5f5; padding: 10px; border-radius: 5px; }
        .content { margin-top: 20px; }
        .field { margin-bottom: 10px; }
        .label { font-weight: bold; }
        .message { white-space: pre-line; margin-top: 15px; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h2>New Contact Form Submission</h2>
        </div>
        <div class="content">
            <div class="field">
                <span class="label">Name:</span> %s
            </div>
            <div class="field">
                <span class="label">Email:</span> %s
            </div>
            <div class="field">
                <span class="label">Subject:</span> %s
            </div>
            <div class="field">
                <span class="label">Message:</span>
                <div class="message">%s</div>
            </div>
        </div>
    </div>
</body>
</html>`, form.Name, form.Email, form.Subject, form.Message)
}

// EmailHandler processes requests to send emails
func (a *API) EmailHandler(w http.ResponseWriter, r *http.Request) {
	// Set response content type
	w.Header().Set("Content-Type", "application/json")

	// Check if method is POST
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		encodeResponse(w, Response{
			Success: false,
			Message: "Only POST method is allowed",
		})
		return
	}

	// Limit the size of the request body
	r.Body = http.MaxBytesReader(w, r.Body, a.Config.MaxBodySize)

	// Decode the request body
	var emailReq email.Request
	err := json.NewDecoder(r.Body).Decode(&emailReq)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Invalid request format: " + err.Error(),
		})
		return
	}

	// Validate required fields
	if emailReq.To == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Recipient (to) is required",
		})
		return
	}

	if emailReq.Subject == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Subject is required",
		})
		return
	}

	if emailReq.Body == "" {
		w.WriteHeader(http.StatusBadRequest)
		encodeResponse(w, Response{
			Success: false,
			Message: "Email body is required",
		})
		return
	}

	// Send the email
	err = email.Send(emailReq, a.Config)
	if err != nil {
		log.Printf("Error sending email: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		encodeResponse(w, Response{
			Success: false,
			Message: "Failed to send email: " + err.Error(),
		})
		return
	}

	// Return success response
	encodeResponse(w, Response{
		Success: true,
		Message: "Email sent successfully",
	})
}

// HealthCheck provides a simple health check endpoint
func (a *API) HealthCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	encodeResponse(w, Response{
		Success: true,
		Message: "Mail API service is running",
	})
}
