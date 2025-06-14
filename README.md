# Contact API

[![Release](https://img.shields.io/github/v/release/nahuelsantos/contact-api?include_prereleases&style=for-the-badge)](https://github.com/nahuelsantos/contact-api/releases)
[![Build Status](https://img.shields.io/github/actions/workflow/status/nahuelsantos/contact-api/release.yml?branch=main&style=for-the-badge)](https://github.com/nahuelsantos/contact-api/actions)
[![Docker](https://img.shields.io/badge/Docker-Ready-2496ED?logo=docker&logoColor=white&style=for-the-badge)](https://github.com/nahuelsantos/contact-api/blob/main/Dockerfile)

Simple REST API for handling contact form submissions from websites. Deploy it on your server and integrate it with any website's contact forms.

## Quick Start

**With Docker:**
```bash
docker run -d \
  -p 3002:3002 \
  -e SMTP_HOST=your-smtp-server \
  -e DEFAULT_TO=your-email@domain.com \
  ghcr.io/nahuelsantos/contact-api:latest
```

**With Docker Compose:**
```bash
git clone https://github.com/nahuelsantos/contact-api.git
cd contact-api
cp .env.example .env  # Configure your settings
make start
```

## Integration

Submit contact forms to: `POST /api/v1/contact/{website}`

**HTML Form Example:**
```html
<form id="contact-form">
  <input type="text" name="name" required>
  <input type="email" name="email" required>
  <input type="text" name="subject" required>
  <textarea name="message" required></textarea>
  <button type="submit">Send</button>
</form>

<script>
document.getElementById('contact-form').addEventListener('submit', async (e) => {
  e.preventDefault();
  
  const formData = new FormData(e.target);
  const data = Object.fromEntries(formData);
  
  const response = await fetch('http://your-api-domain/api/v1/contact/main', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(data)
  });
  
  const result = await response.json();
  alert(result.message);
});
</script>
```

## Configuration

Environment variables:
- `SMTP_HOST` - Your SMTP server
- `SMTP_PORT` - SMTP port (default: 25)
- `DEFAULT_TO` - Recipient email
- `DEFAULT_FROM` - Sender email
- `PORT` - API port (default: 3002)

## API Endpoints

- `POST /api/v1/contact/{website}` - Submit contact form
- `GET /health` - Health check
- `GET /swagger/index.html` - API documentation

## Development

```bash
make help    # Show available commands
make run     # Run locally
make test    # Run tests and linting
make start   # Start with Docker
make stop    # Stop Docker
```