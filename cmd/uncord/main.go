package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/requestid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/api"
	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/bootstrap"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/postgres"
	"github.com/uncord-chat/uncord-server/internal/typesense"
	"github.com/uncord-chat/uncord-server/internal/user"
	"github.com/uncord-chat/uncord-server/internal/valkey"
)

func main() {
	log.Logger = zerolog.New(os.Stderr).With().Timestamp().Logger()

	if err := run(); err != nil {
		log.Fatal().Err(err).Msg("Server stopped")
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if cfg.IsDevelopment() {
		log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
			With().Timestamp().Logger()
	}

	log.Info().Str("env", cfg.ServerEnv).Msg("Starting Uncord Server")

	if cfg.CORSAllowOrigins == "*" {
		log.Warn().Msg("CORS_ALLOW_ORIGINS is set to a wildcard \"*\". This allows any origin to make requests to your server. Set an explicit origin (e.g. https://your-client.example.com) for production deployments.")
	}

	ctx := context.Background()

	// Connect PostgreSQL
	db, err := postgres.Connect(ctx, cfg.DatabaseURL, cfg.DatabaseMaxConn, cfg.DatabaseMinConn)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer db.Close()
	log.Info().Msg("PostgreSQL connected")

	// Run migrations
	if err := postgres.Migrate(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info().Msg("Database migrations complete")

	// Connect Valkey
	rdb, err := valkey.Connect(ctx, cfg.ValkeyURL)
	if err != nil {
		return fmt.Errorf("connect valkey: %w", err)
	}
	defer rdb.Close()
	log.Info().Msg("Valkey connected")

	// Check first-run and seed if needed
	firstRun, err := bootstrap.IsFirstRun(ctx, db)
	if err != nil {
		return fmt.Errorf("check first run: %w", err)
	}
	if firstRun {
		log.Info().Msg("First run detected, running initialization")
		if err := bootstrap.RunFirstInit(ctx, db, cfg); err != nil {
			return fmt.Errorf("first-run initialization: %w", err)
		}
		log.Info().Msg("First-run initialization complete")
	}

	// Typesense collection (best-effort)
	created, err := typesense.EnsureMessagesCollection(ctx, cfg.TypesenseURL, cfg.TypesenseAPIKey)
	if err != nil {
		log.Warn().Err(err).Msg("Typesense collection setup failed (non-fatal)")
	} else if created {
		log.Info().Msg("Typesense messages collection created")
	} else {
		log.Info().Msg("Typesense messages collection already exists")
	}

	// Initialize disposable email blocklist and prefetch asynchronously so the first registration request does not
	// block on a network call.
	blocklist := disposable.NewBlocklist(cfg.DisposableEmailBlocklistURL, cfg.DisposableEmailBlocklistEnabled)
	go blocklist.Prefetch(ctx)

	// Initialize permission engine
	permStore := permission.NewPGStore(db)
	permCache := permission.NewValkeyCache(rdb)
	permResolver := permission.NewResolver(permStore, permCache)
	permPublisher := permission.NewPublisher(rdb)

	// Start permission cache invalidation subscriber with reconnection.
	subCtx, subCancel := context.WithCancel(ctx)
	defer subCancel()
	permSub := permission.NewSubscriber(permCache, rdb)
	go func() {
		for {
			if err := permSub.Run(subCtx); err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				log.Error().Err(err).Msg("Permission cache subscriber stopped, restarting in 5s")
				select {
				case <-subCtx.Done():
					return
				case <-time.After(5 * time.Second):
				}
				continue
			}
			return
		}
	}()

	// Initialize user repository and auth service
	userRepo := user.NewPGRepository(db)
	authService := auth.NewService(userRepo, rdb, cfg, blocklist)

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:               "Uncord",
		DisableStartupMessage: true,
		// ErrorHandler catches errors returned by handlers that are not already mapped to structured API responses
		// (e.g. Fiber's built-in 404/405). errors.AsType is a generic helper added in Go 1.26.
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			status := fiber.StatusInternalServerError
			message := "An internal error occurred"
			apiCode := apierrors.InternalError
			if e, ok := errors.AsType[*fiber.Error](err); ok {
				status = e.Code
				message = e.Message
				apiCode = fiberStatusToAPICode(e.Code)
			} else {
				log.Error().Err(err).
					Str("method", c.Method()).
					Str("path", c.Path()).
					Msg("Unhandled error")
			}
			return c.Status(status).JSON(httputil.ErrorResponse{
				Error: httputil.ErrorBody{
					Code:    apiCode,
					Message: message,
				},
			})
		},
	})

	// Global middleware
	app.Use(requestid.New())
	app.Use(logger.New(logger.Config{
		Format:     "${time} ${locals:requestid} ${method} ${path} ${status} ${latency}\n",
		TimeFormat: time.RFC3339,
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins:  cfg.CORSAllowOrigins,
		AllowMethods:  "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:  "Origin,Content-Type,Accept,Authorization",
		ExposeHeaders: "X-Request-ID",
	}))

	// Global API rate limiter
	app.Use(limiter.New(limiter.Config{
		Max:        cfg.RateLimitAPIRequests,
		Expiration: time.Duration(cfg.RateLimitAPIWindowSeconds) * time.Second,
	}))

	// Register routes
	registerRoutes(app, cfg, db, rdb, authService, permResolver, permPublisher)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info().Msg("Shutting down server")
		subCancel()
		_ = app.Shutdown()
	}()

	// Listen
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Info().Str("addr", addr).Msg("Server listening")
	if err := app.Listen(addr); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func registerRoutes(
	app *fiber.App,
	cfg *config.Config,
	db *pgxpool.Pool,
	rdb *redis.Client,
	authService *auth.Service,
	permResolver *permission.Resolver,
	permPublisher *permission.Publisher,
) {
	health := api.NewHealthHandler(db, redisPinger{client: rdb})
	app.Get("/api/v1/health", health.Health)

	authHandler := api.NewAuthHandler(authService)

	// Auth routes with stricter rate limiting
	authGroup := app.Group("/api/v1/auth")
	authGroup.Use(limiter.New(limiter.Config{
		Max:        cfg.RateLimitAuthCount,
		Expiration: time.Duration(cfg.RateLimitAuthWindowSeconds) * time.Second,
	}))
	authGroup.Post("/register", authHandler.Register)
	authGroup.Post("/login", authHandler.Login)
	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/verify-email", authHandler.VerifyEmail)

	// Protected routes will use:
	//   auth.RequireAuth(cfg.JWTSecret, cfg.ServerURL) middleware group
	//   permission.RequirePermission(permResolver, perm) per-route
	_ = permResolver  // wired into protected route groups as they are built
	_ = permPublisher // used by handlers that mutate permissions
}

// redisPinger adapts *redis.Client to the api.Pinger interface.
type redisPinger struct{ client *redis.Client }

func (p redisPinger) Ping(ctx context.Context) error { return p.client.Ping(ctx).Err() }

// fiberStatusToAPICode maps an HTTP status code from Fiber's built-in errors (404, 405, etc.) to the closest protocol
// error code.
func fiberStatusToAPICode(status int) apierrors.Code {
	switch {
	case status == fiber.StatusNotFound:
		return apierrors.NotFound
	case status == fiber.StatusMethodNotAllowed:
		return apierrors.ValidationError
	case status == fiber.StatusTooManyRequests:
		return apierrors.RateLimited
	case status == fiber.StatusRequestEntityTooLarge:
		return apierrors.PayloadTooLarge
	case status == fiber.StatusServiceUnavailable:
		return apierrors.ServiceUnavailable
	case status >= 400 && status < 500:
		return apierrors.ValidationError
	default:
		return apierrors.InternalError
	}
}
