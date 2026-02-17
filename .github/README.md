<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/uncord-chat/.github/main/profile/logo-banner-dark.png">
    <source media="(prefers-color-scheme: light)" srcset="https://raw.githubusercontent.com/uncord-chat/.github/main/profile/logo-banner-light.png">
    <img alt="Uncord" src="https://raw.githubusercontent.com/uncord-chat/.github/main/profile/logo-banner-light.png" width="1280">
  </picture>
</p>

The Go server for the [Uncord](https://github.com/uncord-chat) project, deployed as a single binary behind Docker Compose. Handles the REST API, WebSocket gateway, permission engine, and media processing.

### Tech stack

| Component | Technology |
|-----------|------------|
| Language | Go 1.26 |
| HTTP Framework | Fiber v3 |
| WebSocket | Fiber contrib/websocket (fasthttp/websocket) |
| Database | PostgreSQL 18 |
| Cache / Pub-Sub | Valkey 9 |
| Search | Typesense 30 |
| Voice/Video | Pion WebRTC SFU (planned) |

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

#### Email testing

Docker Compose includes [Mailpit](https://mailpit.axigen.com/), a local SMTP server that catches all outgoing email. When `SERVER_ENV=development` the server automatically routes SMTP through Mailpit with no manual configuration required. View caught emails at [http://localhost:8025](http://localhost:8025).

### Project structure

```
cmd/uncord/             Entry point
internal/
  api/                  HTTP handlers
  attachment/           File attachment storage and metadata
  auth/                 JWT, Argon2id hashing, refresh tokens, MFA
  bootstrap/            First-run database seeding
  category/             Channel category CRUD
  channel/              Channel CRUD
  config/               Environment variable loading and validation
  disposable/           Disposable email domain blocklist
  email/                SMTP client for transactional email
  gateway/              WebSocket hub, pub/sub fan-out, sessions
  httputil/             Shared JSON response helpers
  invite/               Invite code creation and redemption
  media/                Storage providers and thumbnail generation
  member/               Server membership, bans, timeouts
  message/              Message CRUD and history
  onboarding/           New member onboarding flow
  page/                 Browser-facing HTML pages
  permission/           4-step permission resolver with caching
  postgres/             Connection pool, embedded migrations
  presence/             Online/typing presence tracking
  role/                 Role CRUD and assignment
  search/               Typesense message search
  server/               Server configuration CRUD
  typesense/            Search collection management
  user/                 User CRUD and account deletion
  valkey/               Valkey/Redis connection
```

### Related repositories

| Repository | Description |
|-----------|-------------|
| [uncord-client](https://github.com/uncord-chat/uncord-client) | React Native client for Windows, macOS, Linux, iOS, and Android |
| [uncord-protocol](https://github.com/uncord-chat/uncord-protocol) | Shared types, permission constants, event definitions, and OpenAPI spec |
| [uncord-docs](https://github.com/uncord-chat/uncord-docs) | User and admin documentation |

### License

AGPL-3.0
