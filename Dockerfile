FROM golang:1.23-alpine AS builder

# Build arguments
ARG BUILD_DATE
ARG VCS_REF
ARG VERSION

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application with version info
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-X main.version=${VERSION} -X main.commit=${VCS_REF} -X main.buildDate=${BUILD_DATE}" -o mail-api .

# Create a minimal production image
FROM alpine:3.18

# Build-time metadata as defined at http://label-schema.org
LABEL org.label-schema.build-date=${BUILD_DATE} \
    org.label-schema.name="Mail API" \
    org.label-schema.description="Simple mail API for contact forms" \
    org.label-schema.vcs-ref=${VCS_REF} \
    org.label-schema.vcs-url="https://github.com/nahuelsantos/mail-api" \
    org.label-schema.version=${VERSION} \
    org.label-schema.schema-version="1.0"

WORKDIR /app

# Install necessary tools for diagnostics and health checks
RUN apk add --no-cache curl wget

# Copy the binary from the builder stage
COPY --from=builder /app/mail-api /app/mail-api

# Expose the application port
EXPOSE 20001

# Use a non-root user to run the app (better security)
RUN addgroup -S appgroup && adduser -S appuser -G appgroup
USER appuser

# Run the application
CMD ["/app/mail-api"] 