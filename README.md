# Mail API

[![Go Version](https://img.shields.io/github/go-mod/go-version/nahuelsantos/mail-api)](https://github.com/nahuelsantos/mail-api)
[![License](https://img.shields.io/github/license/nahuelsantos/mail-api)](https://github.com/nahuelsantos/mail-api/blob/main/LICENSE)
[![Build Status](https://img.shields.io/github/actions/workflow/status/nahuelsantos/mail-api/build.yml?branch=main)](https://github.com/nahuelsantos/mail-api/actions)
[![Code Coverage](https://img.shields.io/codecov/c/github/nahuelsantos/mail-api)](https://codecov.io/gh/nahuelsantos/mail-api)
[![Go Report Card](https://goreportcard.com/badge/github.com/nahuelsantos/mail-api)](https://goreportcard.com/report/github.com/nahuelsantos/mail-api)
[![Last Commit](https://img.shields.io/github/last-commit/nahuelsantos/mail-api)](https://github.com/nahuelsantos/mail-api/commits/main)
[![Open Issues](https://img.shields.io/github/issues/nahuelsantos/mail-api)](https://github.com/nahuelsantos/mail-api/issues)
[![Pull Requests](https://img.shields.io/github/issues-pr/nahuelsantos/mail-api)](https://github.com/nahuelsantos/mail-api/pulls)
[![API Status](https://img.shields.io/badge/API-Active-brightgreen)](https://github.com/nahuelsantos/mail-api)
[![Test Status](https://img.shields.io/badge/tests-passing-brightgreen)](https://github.com/nahuelsantos/mail-api/actions)
[![SMTP Support](https://img.shields.io/badge/SMTP-Enabled-blue)](https://github.com/nahuelsantos/mail-api)
[![HTML Emails](https://img.shields.io/badge/HTML_Emails-Supported-blue)](https://github.com/nahuelsantos/mail-api)
[![Docker](https://img.shields.io/badge/Docker-Ready-blue)](https://github.com/nahuelsantos/mail-api/blob/main/Dockerfile)

<div align="center">
  <a href="https://github.com/nahuelsantos/mail-api">
    <img src="https://img.shields.io/badge/Mail-API-blue?style=for-the-badge&logo=mail&logoColor=white" alt="Mail API" />
  </a>
  <a href="https://github.com/nahuelsantos/mail-api/actions">
    <img src="https://img.shields.io/badge/CI/CD-Automated-43a047?style=for-the-badge&logo=github-actions&logoColor=white" alt="CI/CD" />
  </a>
  <a href="https://github.com/nahuelsantos/mail-api/blob/main/Dockerfile">
    <img src="https://img.shields.io/badge/Docker-Ready-2496ED?style=for-the-badge&logo=docker&logoColor=white" alt="Docker" />
  </a>
</div>

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

## Badges

The project uses [Shields.io](https://shields.io/) for status badges in the README. Most badges update automatically based on repository activity. To set up the dynamic coverage badge:

1. Create a new [GitHub Gist](https://gist.github.com/)
2. Note the Gist ID from the URL
3. Create a GitHub token with `gist` scope
4. Add the token as a repository secret named `GIST_SECRET`
5. Update the `gistID` field in `.github/workflows/badges.yml` with your Gist ID

Custom static badges can be created using:

```
https://img.shields.io/badge/<LABEL>-<MESSAGE>-<COLOR>
```

Example:
```
https://img.shields.io/badge/API-Active-brightgreen
```

### Badge Maintenance

- **Dynamic badges** (build, coverage, issues, etc.) update automatically
- **Static badges** (API status, SMTP Support, etc.) should be updated manually as features change
- To update static badges, edit the URLs in the README.md file
- Common colors include: `brightgreen`, `green`, `yellowgreen`, `yellow`, `orange`, `red`, `blue`, `lightgrey`

### Additional Badge Options

Shields.io badges support additional parameters:

- Style: `?style=flat-square`, `?style=plastic`, `?style=for-the-badge`
- Logo: `?logo=go`, `?logo=docker`
- Logo Color: `?logoColor=white`
- Label Color: `?labelColor=abcdef`

Example with styling:
```
https://img.shields.io/badge/API-Active-brightgreen?style=for-the-badge&logo=go&logoColor=white
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

## Status Badges

The project uses [Shields.io](https://shields.io/) badges to provide quick insights:

- **Go Version**: Shows the Go version used in the project
- **License**: Displays the project's license type
- **Build Status**: Indicates whether the CI pipeline is passing
- **Code Coverage**: Shows the percentage of code covered by tests
- **Go Report Card**: Rates the code quality based on various Go best practices
- **Last Commit**: Shows when the last commit was made
- **Open Issues**: Displays the number of open issues
- **Pull Requests**: Shows the number of open pull requests
- **API Status**: Indicates whether the API service is active
- **Test Status**: Shows whether the tests are passing
- **SMTP Support**: Indicates that SMTP is enabled for sending emails
- **HTML Emails**: Shows that HTML email format is supported
- **Docker**: Indicates that the project is containerized with Docker
