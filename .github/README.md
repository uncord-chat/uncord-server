<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/uncord-chat/.github/main/profile/logo-banner-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/uncord-chat/.github/main/profile/logo-banner-light.png">
    <img alt="Uncord" src="https://raw.githubusercontent.com/uncord-chat/.github/main/profile/logo-banner-light.png" width="1280">
  </picture>
</p>

The Go server for the [Uncord](https://github.com/uncord-chat) project, deployed as a single binary behind Docker Compose. Handles the REST API, WebSocket gateway, permission engine, media processing, and plugin system.

### Tech stack

| Component | Technology |
|-----------|------------|
| Language | Go |
| HTTP Framework | Fiber v3 |
| WebSocket | gorilla/websocket |
| Database | PostgreSQL 18 |
| Cache / Pub-Sub | Valkey 9 |
| Search | Typesense |
| Voice/Video | Pion (WebRTC SFU) |

### Quick start

```bash
git clone https://github.com/uncord-chat/uncord-server.git
cd uncord-server
cp .env.example .env    # edit with your settings
docker compose up
```

### Development

```bash
make build              # compile to bin/uncord
make run                # build and run
make test               # run tests with race detector
make lint               # run golangci-lint
make fmt                # format source files
```

See the [Makefile](../Makefile) for the full list of targets.

### Project structure

```
cmd/uncord/             Entry point
internal/
  api/                  HTTP handlers and response helpers
  bootstrap/            First-run initialization
  config/               Environment configuration
  domain/               Core entities and interfaces
  permission/           Permission engine
  postgres/             Database pool, migrations
  typesense/            Search integration
  valkey/               Cache and pub-sub
```

### Related repositories

| Repository | Description |
|-----------|-------------|
| [uncord-client](https://github.com/uncord-chat/uncord-client) | React Native client for Windows, macOS, Linux, iOS, and Android |
| [uncord-protocol](https://github.com/uncord-chat/uncord-protocol) | Shared types, permission constants, event definitions, and OpenAPI spec |
| [uncord-docs](https://github.com/uncord-chat/uncord-docs) | User and admin documentation |

### License

AGPL-3.0
