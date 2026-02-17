package channel

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

const selectColumns = "id, category_id, name, type, topic, position, slowmode_seconds, nsfw, created_at, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed channel repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// List returns all channels ordered by position then creation time.
func (r *PGRepository) List(ctx context.Context) ([]Channel, error) {
	rows, err := r.db.Query(ctx,
		fmt.Sprintf("SELECT %s FROM channels ORDER BY position, created_at", selectColumns),
	)
	if err != nil {
		return nil, fmt.Errorf("query channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		ch, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		channels = append(channels, *ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate channels: %w", err)
	}
	return channels, nil
}

// GetByID returns the channel matching the given ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Channel, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM channels WHERE id = $1", selectColumns), id,
	)
	ch, err := scanChannel(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query channel by id: %w", err)
	}
	return ch, nil
}

// Create inserts a new channel inside a transaction that enforces the maximum count, validates the category reference,
// and auto-assigns a position.
func (r *PGRepository) Create(ctx context.Context, params CreateParams, maxChannels int) (*Channel, error) {
	var ch *Channel
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM channels").Scan(&count); err != nil {
			return fmt.Errorf("count channels: %w", err)
		}
		if count >= maxChannels {
			return ErrMaxChannelsReached
		}

		if params.CategoryID != nil {
			var exists bool
			err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM categories WHERE id = $1)", *params.CategoryID).Scan(&exists)
			if err != nil {
				return fmt.Errorf("check category exists: %w", err)
			}
			if !exists {
				return ErrCategoryNotFound
			}
		}

		row := tx.QueryRow(ctx,
			fmt.Sprintf(
				`INSERT INTO channels (name, type, category_id, topic, slowmode_seconds, nsfw, position)
				 VALUES ($1, $2, $3, $4, $5, $6, COALESCE((SELECT MAX(position) FROM channels), -1) + 1)
				 RETURNING %s`, selectColumns),
			params.Name, params.Type, params.CategoryID, params.Topic, params.SlowmodeSeconds, params.NSFW,
		)
		var err error
		ch, err = scanChannel(row)
		if err != nil {
			return fmt.Errorf("insert channel: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ch, nil
}

// Update applies the non-nil fields in params to the channel row and returns the updated channel. Nil pointer fields
// are left unchanged via COALESCE; the nullable category_id column uses a CASE expression so that SetCategoryNull can
// explicitly clear it. All values flow through pgx named parameter binding.
func (r *PGRepository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Channel, error) {
	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if params.Name == nil && !params.SetCategoryNull && params.CategoryID == nil &&
		params.Topic == nil && params.Position == nil &&
		params.SlowmodeSeconds == nil && params.NSFW == nil {
		return r.GetByID(ctx, id)
	}

	// Validate the category exists before updating.
	if params.CategoryID != nil {
		var exists bool
		err := r.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM categories WHERE id = $1)", *params.CategoryID).Scan(&exists)
		if err != nil {
			return nil, fmt.Errorf("check category exists: %w", err)
		}
		if !exists {
			return nil, ErrCategoryNotFound
		}
	}

	const query = `UPDATE channels SET
		name             = COALESCE(@name, name),
		category_id      = CASE WHEN @clear_category THEN NULL ELSE COALESCE(@category_id, category_id) END,
		topic            = COALESCE(@topic, topic),
		position         = COALESCE(@position, position),
		slowmode_seconds = COALESCE(@slowmode_seconds, slowmode_seconds),
		nsfw             = COALESCE(@nsfw, nsfw)
		WHERE id = @id RETURNING ` + selectColumns

	args := pgx.NamedArgs{
		"id":               id,
		"name":             params.Name,
		"clear_category":   params.SetCategoryNull,
		"category_id":      params.CategoryID,
		"topic":            params.Topic,
		"position":         params.Position,
		"slowmode_seconds": params.SlowmodeSeconds,
		"nsfw":             params.NSFW,
	}

	row := r.db.QueryRow(ctx, query, args)
	ch, err := scanChannel(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update channel: %w", err)
	}
	return ch, nil
}

// Delete removes the channel with the given ID. Database triggers automatically clean up permission overrides.
func (r *PGRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM channels WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete channel: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanChannel scans a single row into a Channel struct.
func scanChannel(row pgx.Row) (*Channel, error) {
	var ch Channel
	err := row.Scan(
		&ch.ID, &ch.CategoryID, &ch.Name, &ch.Type, &ch.Topic,
		&ch.Position, &ch.SlowmodeSeconds, &ch.NSFW, &ch.CreatedAt, &ch.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan channel: %w", err)
	}
	return &ch, nil
}
