# Mail API

A simple REST API for sending emails via SMTP.

## Project Structure

```
mail-api/
├── cmd/
│   └── mail-api/        # Application entrypoint
├── internal/
│   ├── config/          # Configuration handling
│   ├── email/           # Email sending logic
│   └── handlers/        # HTTP handlers
├── scripts/             # Utility scripts
├── .github/             # GitHub workflows
├── main.go              # Main entrypoint
├── go.mod               # Go module definition
├── Makefile             # Build automation
├── Dockerfile           # Docker build configuration
└── README.md            # This file
```

## Configuration

The API can be configured using environment variables:

- `SMTP_HOST`: SMTP server hostname (default: "mail-server")
- `SMTP_PORT`: SMTP server port (default: "25")
- `DEFAULT_FROM`: Default sender email (default: "noreply@dinky.local")
- `DEFAULT_TO`: Default recipient email for contact forms (default: "noreply@dinky.local")
- `ALLOWED_HOSTS`: Comma-separated list of allowed hosts
- `PORT`: HTTP server port (default: "20001")

## API Endpoints

### Send Email

```
POST /send
Content-Type: application/json

{
  "from": "sender@example.com",  # Optional, uses DEFAULT_FROM if empty
  "to": "recipient@example.com",
  "subject": "Email Subject",
  "body": "Email content goes here",
  "html": false                  # Set to true for HTML emails
}
```

### Contact Form

```
POST /contact
Content-Type: application/json

{
  "name": "John Doe",
  "email": "john@example.com",
  "subject": "Inquiry about your services",
  "message": "Hello, I would like to learn more about your services..."
}
```

This endpoint is designed to handle contact form submissions from websites. It sends an email to the `DEFAULT_TO` address with the contact form information formatted as an HTML email.

### Health Check

```
GET /health
```

## Development

### Prerequisites

- Go 1.23 or higher
- golangci-lint (for linting)

### Setup

Clone the repository and install development dependencies:

```bash
# Clone the repository
git clone https://github.com/yourusername/mail-api.git
cd mail-api

# Install development dependencies
make install-deps
```

### Available Make Commands

The project includes a Makefile with common commands:

```bash
# Build the application
make build

# Run tests
make test

# Run tests with coverage
make cover

# Run linter
make lint

# Format code
make fmt

# Run the application
make run

# Check running instance health
make health-check

# Build Docker image
make docker-build

# Run Docker container
make docker-run

# See all available commands
make help
```

## Linting

The project uses golangci-lint for code quality. Configuration is in `.golangci.yml`.

```bash
# Run linter
make lint

# Run linter with auto-fix
make lint-fix
```

## Building and Running

### Locally

```bash
# Download dependencies
go mod download

# Build the application
make build

# Run the server
make run
```

### Docker

```bash
# Build the Docker image
make docker-build

# Run the container
make docker-run
```

## Environment Variables

Copy `.env.example` to `.env` and adjust the values as needed:

```bash
cp .env.example .env
```
