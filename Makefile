.PHONY: build run test bench lint fmt clean docker docker-up docker-down

APP_NAME := goqueue
VERSION := $(shell git describe --tags --always 2>/dev/null || echo dev)
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

## build: Build the binary
build:
	go build $(LDFLAGS) -o bin/$(APP_NAME) ./cmd/goqueue

## run: Build and run the server
run: build
	./bin/$(APP_NAME) server

## test: Run all tests
test:
	go test -v -race ./...

## bench: Run benchmarks
bench:
	go test -bench=. -benchmem ./bench/

## lint: Run go vet
lint:
	go vet ./...

## fmt: Format code
fmt:
	gofmt -s -w .

## clean: Remove build artifacts
clean:
	rm -rf bin/

## docker: Build Docker image
docker:
	docker build -t $(APP_NAME):$(VERSION) .

## docker-up: Start with docker-compose
docker-up:
	docker compose up --build -d

## docker-down: Stop docker-compose
docker-down:
	docker compose down

## help: Show this help
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
