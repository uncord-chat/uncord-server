.PHONY: build run test lint vet fmt tidy docker-up docker-down docker-build migrate-up migrate-down clean

# Build

build:
	go build -o bin/uncord ./cmd/uncord

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
	goose -dir internal/adapter/postgres/migrations postgres "$(DATABASE_URL)" up

migrate-down:
	goose -dir internal/adapter/postgres/migrations postgres "$(DATABASE_URL)" down

migrate-status:
	goose -dir internal/adapter/postgres/migrations postgres "$(DATABASE_URL)" status

# Clean

clean:
	rm -rf bin/ coverage.out
