.PHONY: build run test test-cover lint vet fmt fmt-check vulncheck check tidy docker-up docker-down docker-build docker-logs migrate-up migrate-down migrate-status clean

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE    ?= $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

# Build

build:
	go build -trimpath -ldflags="$(LDFLAGS)" -o bin/uncord ./cmd/uncord
	cp -r data bin/data

run: build
	./bin/uncord

# Quality

test:
	go test -race ./...

test-cover:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

lint:
	golangci-lint run ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	test -z "$$(gofmt -l .)"

vulncheck:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

check: fmt-check lint vet vulncheck test

# Dependencies

tidy:
	go mod tidy

# Docker

docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f uncord

# Database

migrate-up:
	goose -dir internal/postgres/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir internal/postgres/migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir internal/postgres/migrations postgres "$(DATABASE_URL)" status

# Clean

clean:
	rm -rf bin/ coverage.out
