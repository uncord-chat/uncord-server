package server

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const selectColumns = "id, name, description, icon_key, banner_key, owner_id, created_at, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed server config repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
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

// Update applies the non-nil fields in params to the server config row and returns the updated config. Nil pointer
// fields are left unchanged via COALESCE; all values flow through pgx named parameter binding.
func (r *PGRepository) Update(ctx context.Context, params UpdateParams) (*Config, error) {
	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if params.Name == nil && params.Description == nil {
		return r.Get(ctx)
	}

	const query = `UPDATE server_config SET
		name        = COALESCE(@name, name),
		description = COALESCE(@description, description)
		RETURNING ` + selectColumns

	args := pgx.NamedArgs{
		"name":        params.Name,
		"description": params.Description,
	}

	row := r.db.QueryRow(ctx, query, args)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update server config: %w", err)
	}
	return cfg, nil
}

// SetIconKey sets the server icon storage key and returns the updated config.
func (r *PGRepository) SetIconKey(ctx context.Context, key string) (*Config, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE server_config SET icon_key = $1 RETURNING `+selectColumns, key)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("set icon key: %w", err)
	}
	return cfg, nil
}

// ClearIconKey removes the server icon storage key and returns the updated config.
func (r *PGRepository) ClearIconKey(ctx context.Context) (*Config, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE server_config SET icon_key = NULL RETURNING `+selectColumns)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("clear icon key: %w", err)
	}
	return cfg, nil
}

// SetBannerKey sets the server banner storage key and returns the updated config.
func (r *PGRepository) SetBannerKey(ctx context.Context, key string) (*Config, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE server_config SET banner_key = $1 RETURNING `+selectColumns, key)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("set banner key: %w", err)
	}
	return cfg, nil
}

// ClearBannerKey removes the server banner storage key and returns the updated config.
func (r *PGRepository) ClearBannerKey(ctx context.Context) (*Config, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE server_config SET banner_key = NULL RETURNING `+selectColumns)
	cfg, err := scanConfig(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("clear banner key: %w", err)
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
