package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/uncord-chat/uncord-server/internal/api"
	"github.com/uncord-chat/uncord-server/internal/bootstrap"
	"github.com/uncord-chat/uncord-server/internal/config"
	"github.com/uncord-chat/uncord-server/internal/postgres"
	"github.com/uncord-chat/uncord-server/internal/typesense"
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
		log.Info().Msg("First run detected — running initialization")
		if err := bootstrap.RunFirstInit(ctx, db, cfg); err != nil {
			return fmt.Errorf("first-run initialization: %w", err)
		}
		log.Info().Msg("First-run initialization complete")
	}

	// Typesense collection (best-effort)
	created, err := typesense.EnsureMessagesCollection(cfg.TypesenseURL, cfg.TypesenseAPIKey)
	if err != nil {
		log.Warn().Err(err).Msg("Typesense collection setup failed (non-fatal)")
	} else if created {
		log.Info().Msg("Typesense messages collection created")
	} else {
		log.Info().Msg("Typesense messages collection already exists")
	}

	// Create Fiber app
	app := fiber.New(fiber.Config{
		AppName:               "Uncord",
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := errors.AsType[*fiber.Error](err); ok {
				code = e.Code
			}
			return c.Status(code).JSON(fiber.Map{
				"error": fiber.Map{
					"code":    "INTERNAL_ERROR",
					"message": err.Error(),
				},
			})
		},
	})

	// Register routes
	health := &api.HealthHandler{DB: db, Redis: rdb}
	app.Get("/api/v1/health", health.Health)

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Info().Msg("Shutting down server")
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
