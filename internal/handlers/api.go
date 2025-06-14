package handlers

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/nahuelsantos/contact-api/internal/config"
	"github.com/nahuelsantos/contact-api/internal/email"
	"go.opentelemetry.io/otel"
)

// Response represents the API response
type Response struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ContactFormData represents a contact form submission
type ContactFormData struct {
	Name    string `json:"name" binding:"required" example:"John Doe"`
	Email   string `json:"email" binding:"required,email" example:"john@example.com"`
	Subject string `json:"subject" binding:"required" example:"Inquiry about services"`
	Message string `json:"message" binding:"required" example:"I would like to know more about your services"`
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

// ContactHandler processes contact form submissions
// @Summary Submit contact form
// @Description Submit a contact form for a specific website
// @Tags contact
// @Accept json
// @Produce json
// @Param website path string true "Website identifier" example:"main"
// @Param contact body ContactFormData true "Contact form data"
// @Success 200 {object} Response
// @Failure 400 {object} Response
// @Failure 500 {object} Response
// @Router /contact/{website} [post]
func (a *API) ContactHandler(c *gin.Context) {
	tracer := otel.Tracer("contact-api")
	_, span := tracer.Start(c.Request.Context(), "contact.submit")
	defer span.End()

	website := c.Param("website")

	var contactForm ContactFormData
	if err := c.ShouldBindJSON(&contactForm); err != nil {
		slog.Error("Invalid contact form data", "error", err, "website", website)
		c.JSON(http.StatusBadRequest, Response{
			Success: false,
			Message: "Invalid request format: " + err.Error(),
		})
		return
	}

	// Log contact form submission
	slog.Info("Contact form submission",
		"website", website,
		"email", contactForm.Email,
		"subject", contactForm.Subject,
	)

	// Construct email from contact form
	emailReq := email.Request{
		From:    contactForm.Email,
		To:      a.getRecipientForWebsite(website),
		Subject: fmt.Sprintf("[%s] Contact Form: %s", website, contactForm.Subject),
		Body:    a.formatContactEmail(contactForm, website),
		HTML:    true,
	}

	// Send the email
	err := email.Send(emailReq, a.Config)
	if err != nil {
		slog.Error("Failed to send contact form email",
			"error", err,
			"website", website,
			"email", contactForm.Email,
		)
		c.JSON(http.StatusInternalServerError, Response{
			Success: false,
			Message: "Failed to send your message. Please try again later.",
		})
		return
	}

	slog.Info("Contact form sent successfully",
		"website", website,
		"email", contactForm.Email,
	)

	c.JSON(http.StatusOK, Response{
		Success: true,
		Message: "Your message has been sent successfully! We will get back to you soon.",
	})
}

// WebsiteHealthCheck provides a health check for a specific website configuration
// @Summary Health check for website
// @Description Check if the contact form is properly configured for a website
// @Tags health
// @Produce json
// @Param website path string true "Website identifier" example:"main"
// @Success 200 {object} Response
// @Router /contact/{website}/health [get]
func (a *API) WebsiteHealthCheck(c *gin.Context) {
	website := c.Param("website")
	recipient := a.getRecipientForWebsite(website)

	c.JSON(http.StatusOK, Response{
		Success: true,
		Message: "Website contact form is configured",
		Data: map[string]string{
			"website":   website,
			"recipient": recipient,
			"smtp_host": a.Config.SMTPHost,
		},
	})
}

// HealthCheck provides a simple health check endpoint
// @Summary Health check
// @Description Check if the Contact API service is running
// @Tags health
// @Produce json
// @Success 200 {object} Response
// @Router /health [get]
func (a *API) HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Success: true,
		Message: "Contact API service is running",
	})
}

// getRecipientForWebsite returns the email recipient for a specific website
// This is where future website-specific configuration can be added
func (a *API) getRecipientForWebsite(_ string) string {
	// For now, use the default recipient for all websites
	// In the future, this could look up website-specific recipients from:
	// - Environment variables (e.g., RECIPIENT_WEBSITE_MAIN)
	// - Configuration file
	// - Database
	return a.Config.DefaultTo
}

// formatContactEmail formats the contact form data as an HTML email
func (a *API) formatContactEmail(form ContactFormData, website string) string {
	return fmt.Sprintf(`
<!DOCTYPE html>
<html>
<head>
    <style>
        body { font-family: Arial, sans-serif; line-height: 1.6; color: #333; }
        .container { max-width: 600px; margin: 0 auto; padding: 20px; }
        .header { background-color: #f8f9fa; padding: 20px; border-radius: 8px; margin-bottom: 20px; }
        .header h2 { margin: 0; color: #2c3e50; }
        .website-badge { background-color: #3498db; color: white; padding: 4px 8px; border-radius: 4px; font-size: 12px; }
        .content { background-color: #ffffff; padding: 20px; border: 1px solid #dee2e6; border-radius: 8px; }
        .field { margin-bottom: 15px; }
        .label { font-weight: bold; color: #495057; }
        .value { margin-top: 5px; }
        .message { background-color: #f8f9fa; padding: 15px; border-radius: 4px; margin-top: 10px; white-space: pre-line; }
    </style>
</head>
<body>
    <div class="container">
        <div class="header">
            <h2>New Contact Form Submission</h2>
            <span class="website-badge">%s</span>
        </div>
        <div class="content">
            <div class="field">
                <div class="label">Name:</div>
                <div class="value">%s</div>
            </div>
            <div class="field">
                <div class="label">Email:</div>
                <div class="value">%s</div>
            </div>
            <div class="field">
                <div class="label">Subject:</div>
                <div class="value">%s</div>
            </div>
            <div class="field">
                <div class="label">Message:</div>
                <div class="message">%s</div>
            </div>
        </div>
    </div>
</body>
</html>`, website, form.Name, form.Email, form.Subject, form.Message)
}
