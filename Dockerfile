# Build stage
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod edit -dropreplace=github.com/uncord-chat/uncord-protocol && \
    go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=$(git rev-parse --short HEAD) -X main.date=$(date -u '+%Y-%m-%dT%H:%M:%SZ')" \
    -o /bin/uncord ./cmd/uncord

# Runtime stage
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget

RUN addgroup -S uncord && adduser -S uncord -G uncord

COPY --from=build /bin/uncord /usr/local/bin/uncord

USER uncord

EXPOSE 8080 9090

ENTRYPOINT ["uncord"]
