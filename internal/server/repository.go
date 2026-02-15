package server

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const selectColumns = "id, name, description, icon_key, banner_key, owner_id, created_at, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed server config repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Get returns the server configuration row.
func (r *PGRepository) Get(ctx context.Context) (*Config, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM server_config ORDER BY created_at LIMIT 1", selectColumns),
	)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query server config: %w", err)
	}
	return cfg, nil
}

// Update applies the non-nil fields in params to the server config row and returns the updated config.
func (r *PGRepository) Update(ctx context.Context, params UpdateParams) (*Config, error) {
	var setClauses []string
	namedArgs := pgx.NamedArgs{}

	if params.Name != nil {
		setClauses = append(setClauses, "name = @name")
		namedArgs["name"] = *params.Name
	}
	if params.Description != nil {
		setClauses = append(setClauses, "description = @description")
		namedArgs["description"] = *params.Description
	}
	if params.IconKey != nil {
		setClauses = append(setClauses, "icon_key = @icon_key")
		namedArgs["icon_key"] = *params.IconKey
	}
	if params.BannerKey != nil {
		setClauses = append(setClauses, "banner_key = @banner_key")
		namedArgs["banner_key"] = *params.BannerKey
	}

	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if len(setClauses) == 0 {
		return r.Get(ctx)
	}

	query := "UPDATE server_config SET " + strings.Join(setClauses, ", ") +
		" RETURNING " + selectColumns

	row := r.db.QueryRow(ctx, query, namedArgs)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update server config: %w", err)
	}
	return cfg, nil
}

// scanConfig scans a single row into a Config struct.
func scanConfig(row pgx.Row) (*Config, error) {
	var cfg Config
	err := row.Scan(
		&cfg.ID, &cfg.Name, &cfg.Description, &cfg.IconKey, &cfg.BannerKey,
		&cfg.OwnerID, &cfg.CreatedAt, &cfg.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan server config: %w", err)
	}
	return &cfg, nil
}
