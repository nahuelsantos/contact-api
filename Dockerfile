FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o mail-api .

# Create a minimal production image
FROM alpine:3.18

WORKDIR /app

# Install necessary tools for diagnostics and health checks
RUN apk add --no-cache curl wget

# Copy the binary from the builder stage
COPY --from=builder /app/mail-api /app/mail-api

# Expose the application port
EXPOSE 20001

# Run the application
CMD ["/app/mail-api"] 