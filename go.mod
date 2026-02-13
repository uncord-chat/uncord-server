module github.com/uncord-chat/uncord-server

go 1.26.0

// NOTE: Remove this replace directive before tagging a release. It points to a local protocol repo for development
// convenience. Production builds must reference a tagged version of uncord-protocol.
// replace github.com/uncord-chat/uncord-protocol => ../uncord-protocol

require (
	github.com/alexedwards/argon2id v1.0.0
	github.com/alicebob/miniredis/v2 v2.36.1
	github.com/gofiber/fiber/v3 v3.0.0
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.8.0
	github.com/pressly/goose/v3 v3.26.0
	github.com/redis/go-redis/v9 v9.17.3
	github.com/rs/zerolog v1.34.0
	github.com/uncord-chat/uncord-protocol v0.2.3
)

require (
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/gofiber/schema v1.6.0 // indirect
	github.com/gofiber/utils/v2 v2.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.18.3 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/philhofer/fwd v1.2.0 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	github.com/tinylib/msgp v1.6.3 // indirect
	github.com/valyala/bytebufferpool v1.0.0 // indirect
	github.com/valyala/fasthttp v1.69.0 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.47.0 // indirect
	golang.org/x/net v0.49.0 // indirect
	golang.org/x/sync v0.19.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.33.0 // indirect
)
