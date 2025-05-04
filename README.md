# Mail API

[![Go Version](https://img.shields.io/github/go-mod/go-version/nahuelsantos/mail-api?logo=go&logoColor=white&style=for-the-badge)](https://github.com/nahuelsantos/mail-api)
[![License](https://img.shields.io/github/license/nahuelsantos/mail-api?style=for-the-badge)](https://github.com/nahuelsantos/mail-api/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/nahuelsantos/mail-api?include_prereleases&style=for-the-badge)](https://github.com/nahuelsantos/mail-api/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/nahuelsantos/mail-api/go.yml?branch=main&style=for-the-badge)](https://github.com/nahuelsantos/mail-api/actions)
[![Last Commit](https://img.shields.io/github/last-commit/nahuelsantos/mail-api?style=for-the-badge)](https://github.com/nahuelsantos/mail-api/commits/main)
[![Open Issues](https://img.shields.io/github/issues/nahuelsantos/mail-api?style=for-the-badge)](https://github.com/nahuelsantos/mail-api/issues)
[![Pull Requests](https://img.shields.io/github/issues-pr/nahuelsantos/mail-api?style=for-the-badge)](https://github.com/nahuelsantos/mail-api/pulls)
[![Test Status](https://img.shields.io/badge/tests-passing-brightgreen?style=for-the-badge)](https://github.com/nahuelsantos/mail-api/actions)
[![CI/CD](https://img.shields.io/badge/CI/CD-Automated-43a047?logo=github-actions&logoColor=white&style=for-the-badge)](https://github.com/nahuelsantos/mail-api/actions)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white&style=for-the-badge)](https://github.com/nahuelsantos/mail-api/blob/main/Dockerfile)

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

## CI/CD Pipeline

The project includes an automated CI/CD pipeline using GitHub Actions:

### Pull Request Workflow
When a pull request is opened against the `main` branch:
- Linting is performed
- All tests are run with race condition detection
- Code coverage is calculated and reported

### Main Branch Workflow
When changes are merged to the `main` branch:
- A new version is automatically determined using semantic versioning (patch by default)
- A new GitHub release is created with release notes
- A Docker image is built and published to GitHub Container Registry (ghcr.io)
- The Docker image is tagged with:
  - The full semantic version (e.g., `v1.2.3`)
  - The major.minor version (e.g., `v1.2`)
  - `latest` tag

### Using the Docker Image

The Docker image is available at:
```
ghcr.io/nahuelsantos/mail-api:latest
```

To use a specific version:
```bash
docker pull ghcr.io/nahuelsantos/mail-api:v1.2.3
```

## Status Badges

The project uses [Shields.io](https://shields.io/) badges to provide quick insights:

- **Go Version**: Shows the Go version used in the project
- **License**: Displays the project's license type
- **Release**: Shows the latest release version
- **Build Status**: Indicates whether the CI pipeline is passing
- **Last Commit**: Shows when the last commit was made
- **Open Issues**: Displays the number of open issues
- **Pull Requests**: Shows the number of open pull requests
- **Test Status**: Shows whether the tests are passing
- **CI/CD**: Indicates that CI/CD is automated using GitHub Actions
- **Docker**: Indicates that the project is containerized with Docker

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