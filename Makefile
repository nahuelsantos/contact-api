.PHONY: build clean test run lint lint-fix docker-build docker-run help health-check

# Binary name
BINARY_NAME=mail-api
VERSION=1.0.0
BUILD_TIME=$(shell date +%FT%T%z)
DOCKER_TAG=latest

# Go related variables
GOBASE=$(shell pwd)
GOBIN=$(GOBASE)/bin
GOFILES=$(wildcard *.go)

# Main package path
MAIN_PACKAGE=.

# Default target
.DEFAULT_GOAL := help

## build: Build the application
build:
	@echo "Building $(BINARY_NAME)"
	go build -o $(GOBIN)/$(BINARY_NAME) $(MAIN_PACKAGE)

## clean: Clean build files
clean:
	@echo "Cleaning"
	go clean
	rm -f $(GOBIN)/$(BINARY_NAME)

## test: Run tests
test:
	@echo "Testing"
	go test -v ./...

## cover: Run tests with coverage
cover:
	@echo "Testing with coverage"
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out

## run: Run the application
run:
	@echo "Running $(BINARY_NAME)"
	go run $(MAIN_PACKAGE)

## lint: Run linter
lint:
	@echo "Linting"
	golangci-lint run ./...

## lint-fix: Run linter with fixing
lint-fix:
	@echo "Linting and fixing"
	golangci-lint run --fix ./...

## fmt: Format code
fmt:
	@echo "Formatting"
	go fmt ./...

## vet: Run go vet
vet:
	@echo "Vetting"
	go vet ./...

## docker-build: Build docker image
docker-build:
	@echo "Building Docker image"
	docker build -t $(BINARY_NAME):$(DOCKER_TAG) .

## docker-run: Run docker container
docker-run:
	@echo "Running Docker container"
	docker run -p 20001:20001 --env-file .env $(BINARY_NAME):$(DOCKER_TAG)

## mod-tidy: Tidy and verify go modules
mod-tidy:
	@echo "Tidying modules"
	go mod tidy
	go mod verify

## install-deps: Install development dependencies
install-deps:
	@echo "Installing dev dependencies"
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

## health-check: Run health check against a running instance
health-check:
	@echo "Running health check"
	./scripts/health_check.sh

## all: Clean, build, and test
all: clean build test

## help: Display help
help:
	@echo "Usage: make [target]"
	@grep -E '^##' Makefile | sed -e 's/## //g' | column -t -s ':' 