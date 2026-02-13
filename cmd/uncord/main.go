package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	apierrors "github.com/uncord-chat/uncord-protocol/errors"

	"github.com/uncord-chat/uncord-server/internal/api"
	"github.com/uncord-chat/uncord-server/internal/auth"
	"github.com/uncord-chat/uncord-server/internal/bootstrap"
	"github.com/uncord-chat/uncord-server/internal/category"
	"github.com/uncord-chat/uncord-server/internal/channel"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/disposable"
	"github.com/uncord-chat/uncord-server/internal/httputil"
	"github.com/uncord-chat/uncord-server/internal/permission"
	"github.com/uncord-chat/uncord-server/internal/postgres"
	servercfg "github.com/uncord-chat/uncord-server/internal/server"
	"github.com/uncord-chat/uncord-server/internal/typesense"
	"github.com/uncord-chat/uncord-server/internal/user"
	"github.com/uncord-chat/uncord-server/internal/valkey"

	"github.com/uncord-chat/uncord-protocol/permissions"
)

// Build metadata injected via ldflags at compile time.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// server holds the shared dependencies used by route handlers and middleware.
type server struct {
	cfg           *config.Config
	db            *pgxpool.Pool
	rdb           *redis.Client
	userRepo      user.Repository
	authService   *auth.Service
	serverRepo    servercfg.Repository
	channelRepo   channel.Repository
	categoryRepo  category.Repository
	permResolver  *permission.Resolver
	permPublisher *permission.Publisher
}

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

	log.Info().
		Str("version", version).
		Str("commit", commit).
		Str("built", date).
		Str("env", cfg.ServerEnv).
		Msg("Starting Uncord Server")

	if cfg.CORSAllowOrigins == "*" {
		log.Warn().Msg("CORS_ALLOW_ORIGINS is set to a wildcard. Set an explicit origin when in production.")
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
	if err := postgres.Migrate(cfg.DatabaseURL, log.Logger); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	log.Info().Msg("Database migrations complete")

	// Connect Valkey
	rdb, err := valkey.Connect(ctx, cfg.ValkeyURL)
	if err != nil {
		return fmt.Errorf("connect valkey: %w", err)
	}
	defer func() { _ = rdb.Close() }()
	log.Info().Msg("Valkey connected")

	// Check first-run and seed if needed
	firstRun, err := bootstrap.IsFirstRun(ctx, db)
	if err != nil {
		return fmt.Errorf("check first run: %w", err)
	}
	if firstRun {
		log.Info().Msg("First run detected, running initialization")
		if err := bootstrap.RunFirstInit(ctx, db, cfg, log.Logger); err != nil {
			return fmt.Errorf("first-run initialization: %w", err)
		}
		log.Info().Msg("First-run initialization complete")
	}

	// Typesense collection (best-effort)
	result, err := typesense.EnsureMessagesCollection(ctx, cfg.TypesenseURL, cfg.TypesenseAPIKey)
	if err != nil {
		log.Warn().Err(err).Msg("Typesense collection setup failed")
	} else {
		switch result {
		case typesense.ResultCreated:
			log.Info().Msg("Typesense messages collection created")
		case typesense.ResultRecreated:
			log.Warn().Msg("Typesense messages collection recreated due to schema change")
		case typesense.ResultUnchanged:
			log.Info().Msg("Typesense messages collection already exists")
		}
	}

	// Initialise disposable email blocklist with periodic refresh so newly added disposable domains are picked up
	// without requiring a server restart.
	blocklist := disposable.NewBlocklist(cfg.DisposableEmailBlocklistURL, cfg.DisposableEmailBlocklistEnabled, log.Logger)

	// Initialise permission engine
	permStore := permission.NewPGStore(db)
	permCache := permission.NewValkeyCache(rdb)
	permResolver := permission.NewResolver(permStore, permCache, log.Logger)
	permPublisher := permission.NewPublisher(rdb)

	// Start background services with a shared cancellable context.
	subCtx, subCancel := context.WithCancel(ctx)

	go func() {
		if err := blocklist.Run(subCtx, cfg.DisposableEmailBlocklistRefreshInterval); err != nil {
			log.Error().Err(err).Msg("Disposable email blocklist refresh stopped")
		}
	}()

	// Start permission cache invalidation subscriber with reconnection.
	defer subCancel()
	permSub := permission.NewSubscriber(permCache, rdb, log.Logger)
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

	// Initialise repositories and services
	userRepo := user.NewPGRepository(db, log.Logger)
	serverRepo := servercfg.NewPGRepository(db, log.Logger)
	channelRepo := channel.NewPGRepository(db, log.Logger)
	categoryRepo := category.NewPGRepository(db, log.Logger)
	authService, err := auth.NewService(userRepo, rdb, cfg, blocklist, log.Logger)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create auth service")
	}

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:   "Uncord",
		BodyLimit: cfg.BodyLimitBytes(),
		// ErrorHandler catches errors returned by handlers that are not already mapped to structured API responses
		// (e.g. Fiber's built-in 404/405). errors.AsType is a generic helper added in Go 1.26.
		ErrorHandler: func(c fiber.Ctx, err error) error {
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
	app.Use(httputil.RequestLogger(log.Logger))
	app.Use(cors.New(cors.Config{
		AllowOrigins:  strings.Split(cfg.CORSAllowOrigins, ","),
		AllowMethods:  []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:  []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders: []string{"X-Request-ID"},
	}))

	// Global API rate limiter
	app.Use(limiter.New(limiter.Config{
		Max:        cfg.RateLimitAPIRequests,
		Expiration: time.Duration(cfg.RateLimitAPIWindowSeconds) * time.Second,
	}))

	// Register routes
	srv := &server{
		cfg:           cfg,
		db:            db,
		rdb:           rdb,
		userRepo:      userRepo,
		serverRepo:    serverRepo,
		channelRepo:   channelRepo,
		categoryRepo:  categoryRepo,
		authService:   authService,
		permResolver:  permResolver,
		permPublisher: permPublisher,
	}
	srv.registerRoutes(app)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info().Msg("Shutting down server")
		subCancel()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			log.Error().Err(err).Msg("Server shutdown error")
		}
	}()

	// Listen
	addr := fmt.Sprintf(":%d", cfg.ServerPort)
	log.Info().Str("addr", addr).Msg("Server listening")

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	log.Debug().
		Uint64("alloc_mb", mem.Alloc/1024/1024).
		Uint64("sys_mb", mem.Sys/1024/1024).
		Uint64("heap_inuse_mb", mem.HeapInuse/1024/1024).
		Uint64("stack_inuse_mb", mem.StackInuse/1024/1024).
		Uint32("num_gc", mem.NumGC).
		Msg("Runtime memory stats")

	if err := app.Listen(addr, fiber.ListenConfig{DisableStartupMessage: true}); err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

func (s *server) registerRoutes(app *fiber.App) {
	health := api.NewHealthHandler(s.db, redisPinger{client: s.rdb})
	app.Get("/api/v1/health", health.Health)

	authHandler := api.NewAuthHandler(s.authService)

	// Auth routes with stricter rate limiting
	authGroup := app.Group("/api/v1/auth")
	authGroup.Use(limiter.New(limiter.Config{
		Max:        s.cfg.RateLimitAuthCount,
		Expiration: time.Duration(s.cfg.RateLimitAuthWindowSeconds) * time.Second,
	}))
	authGroup.Post("/register", authHandler.Register)
	authGroup.Post("/login", authHandler.Login)
	authGroup.Post("/refresh", authHandler.Refresh)
	authGroup.Post("/verify-email", authHandler.VerifyEmail)

	// User profile routes (authenticated, no permission checks)
	userHandler := api.NewUserHandler(s.userRepo)
	userGroup := app.Group("/api/v1/users", auth.RequireAuth(s.cfg.JWTSecret, s.cfg.ServerURL))
	userGroup.Get("/@me", userHandler.GetMe)
	userGroup.Patch("/@me", userHandler.UpdateMe)

	// Server config routes (authenticated, PATCH requires ManageServer)
	serverHandler := api.NewServerHandler(s.serverRepo)
	serverGroup := app.Group("/api/v1/server", auth.RequireAuth(s.cfg.JWTSecret, s.cfg.ServerURL))
	serverGroup.Get("/", serverHandler.Get)
	serverGroup.Patch("/", permission.RequireServerPermission(s.permResolver, permissions.ManageServer), serverHandler.Update)

	// Channel routes
	channelHandler := api.NewChannelHandler(s.channelRepo, s.permResolver, s.cfg.MaxChannels)
	serverGroup.Get("/channels", channelHandler.ListChannels)
	serverGroup.Post("/channels",
		permission.RequireServerPermission(s.permResolver, permissions.ManageChannels),
		channelHandler.CreateChannel)

	channelGroup := app.Group("/api/v1/channels",
		auth.RequireAuth(s.cfg.JWTSecret, s.cfg.ServerURL))
	channelGroup.Get("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ViewChannels),
		channelHandler.GetChannel)
	channelGroup.Patch("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ManageChannels),
		channelHandler.UpdateChannel)
	channelGroup.Delete("/:channelID",
		permission.RequirePermission(s.permResolver, permissions.ManageChannels),
		channelHandler.DeleteChannel)

	// Category routes
	categoryHandler := api.NewCategoryHandler(s.categoryRepo, s.cfg.MaxCategories)
	serverGroup.Get("/categories", categoryHandler.ListCategories)
	serverGroup.Post("/categories",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.CreateCategory)

	categoryGroup := app.Group("/api/v1/categories",
		auth.RequireAuth(s.cfg.JWTSecret, s.cfg.ServerURL))
	categoryGroup.Patch("/:categoryID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.UpdateCategory)
	categoryGroup.Delete("/:categoryID",
		permission.RequireServerPermission(s.permResolver, permissions.ManageCategories),
		categoryHandler.DeleteCategory)

	_ = s.permPublisher // used by handlers that mutate permissions
}

// redisPinger adapts *redis.Client to the api.Pinger interface.
type redisPinger struct{ client *redis.Client }

func (p redisPinger) Ping(ctx context.Context) error { return p.client.Ping(ctx).Err() }

// fiberStatusToAPICode maps an HTTP status code from Fiber's built-in errors (404, 405, etc.) to the closest protocol
// error code.
func fiberStatusToAPICode(status int) apierrors.Code {
	switch status {
	case fiber.StatusNotFound:
		return apierrors.NotFound
	case fiber.StatusMethodNotAllowed:
		return apierrors.ValidationError
	case fiber.StatusTooManyRequests:
		return apierrors.RateLimited
	case fiber.StatusRequestEntityTooLarge:
		return apierrors.PayloadTooLarge
	case fiber.StatusServiceUnavailable:
		return apierrors.ServiceUnavailable
	default:
		if status >= 400 && status < 500 {
			return apierrors.ValidationError
		}
		return apierrors.InternalError
	}
}
