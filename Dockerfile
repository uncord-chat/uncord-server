# Build stage
FROM golang:1.26-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./

# Docker builds must use the published protocol package, not a local filesystem replacement. Fail early with a clear
# message so the developer knows to fix go.mod before building.
RUN if grep -q '^replace.*uncord-protocol' go.mod; then \
      echo '' >&2; \
      echo 'ERROR: go.mod contains a replace directive for uncord-protocol.' >&2; \
      echo '' >&2; \
      echo 'Docker builds must use the published protocol package. Remove or comment' >&2; \
      echo 'out the replace directive in go.mod and ensure the require directive' >&2; \
      echo 'references a published version (e.g. v0.2.10), then retry the build.' >&2; \
      echo '' >&2; \
      exit 1; \
    fi

RUN go mod download

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
COPY data/ /data/uncord/

RUN chown -R uncord:uncord /data/uncord

USER uncord

EXPOSE 8080

ENTRYPOINT ["uncord"]
