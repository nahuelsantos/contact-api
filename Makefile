# Contact API Makefile

.PHONY: help run test start stop

# Default target
help: ## Show this help message
	@echo "Contact API - Available commands:"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

run: ## Run the application locally
	@echo "Starting Contact API on port 3002..."
	go run cmd/contact-api/main.go

test: ## Run linting and tests
	@echo "Running linter..."
	golangci-lint run ./...
	@echo "Running tests..."
	go test -v -race ./...

start: ## Start with docker-compose
	@echo "Starting Contact API with Docker..."
	docker-compose -f docker-compose.dinky.yml up -d

stop: ## Stop docker-compose
	@echo "Stopping Contact API..."
	docker-compose -f docker-compose.dinky.yml down 