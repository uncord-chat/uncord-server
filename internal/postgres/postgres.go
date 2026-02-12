package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog/log"

	"github.com/uncord-chat/uncord-server/internal/postgres/migrations"
)

// gooseLogger adapts zerolog to the goose.Logger interface.
type gooseLogger struct{}

func (gooseLogger) Fatalf(format string, v ...any) { log.Error().Msgf(format, v...) }
func (gooseLogger) Printf(format string, v ...any) { log.Info().Msgf(format, v...) }

// Connect creates a pgxpool.Pool from the given DSN with the specified connection limits.
func Connect(ctx context.Context, dsn string, maxConns, minConns int) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse postgres config: %w", err)
	}
	cfg.MaxConns = int32(maxConns)
	cfg.MinConns = int32(minConns)

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	return pool, nil
}

// Migrate runs all pending goose migrations using the embedded SQL files.
func Migrate(dsn string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open sql connection for migrations: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{})

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}
