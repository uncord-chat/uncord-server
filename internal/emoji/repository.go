package emoji

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

const selectColumns = `id, name, animated, storage_key, uploader_id, created_at, updated_at`

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed emoji repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// Create inserts a new custom emoji and returns the created record. Returns ErrNameTaken if the name conflicts with an
// existing emoji.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (*Emoji, error) {
	row := r.db.QueryRow(ctx,
		`INSERT INTO custom_emoji (name, animated, storage_key, uploader_id)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+selectColumns,
		params.Name, params.Animated, params.StorageKey, params.UploaderID,
	)
	e, err := scanEmoji(row)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, ErrNameTaken
		}
		return nil, fmt.Errorf("create emoji: %w", err)
	}
	return e, nil
}

// GetByID returns a custom emoji by its ID. Returns ErrNotFound when the ID does not exist.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Emoji, error) {
	row := r.db.QueryRow(ctx,
		"SELECT "+selectColumns+" FROM custom_emoji WHERE id = $1", id,
	)
	e, err := scanEmoji(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query emoji by id: %w", err)
	}
	return e, nil
}

// GetByName returns a custom emoji by its unique name. Returns ErrNotFound when the name does not exist.
func (r *PGRepository) GetByName(ctx context.Context, name string) (*Emoji, error) {
	row := r.db.QueryRow(ctx,
		"SELECT "+selectColumns+" FROM custom_emoji WHERE name = $1", name,
	)
	e, err := scanEmoji(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query emoji by name: %w", err)
	}
	return e, nil
}

// List returns all custom emoji ordered by name.
func (r *PGRepository) List(ctx context.Context) ([]Emoji, error) {
	rows, err := r.db.Query(ctx,
		"SELECT "+selectColumns+" FROM custom_emoji ORDER BY name ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("list emoji: %w", err)
	}
	defer rows.Close()

	var result []Emoji
	for rows.Next() {
		e, err := scanEmoji(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate emoji: %w", err)
	}
	return result, nil
}

// UpdateName changes the name of an existing emoji. Returns ErrNotFound if the ID does not exist, or ErrNameTaken if
// the new name conflicts with another emoji.
func (r *PGRepository) UpdateName(ctx context.Context, id uuid.UUID, name string) (*Emoji, error) {
	row := r.db.QueryRow(ctx,
		`UPDATE custom_emoji SET name = $2 WHERE id = $1 RETURNING `+selectColumns,
		id, name,
	)
	e, err := scanEmoji(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if postgres.IsUniqueViolation(err) {
			return nil, ErrNameTaken
		}
		return nil, fmt.Errorf("update emoji name: %w", err)
	}
	return e, nil
}

// Delete removes a custom emoji by ID. Returns ErrNotFound if the ID does not exist.
func (r *PGRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM custom_emoji WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete emoji: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// Count returns the total number of custom emoji.
func (r *PGRepository) Count(ctx context.Context) (int, error) {
	var count int
	err := r.db.QueryRow(ctx, "SELECT COUNT(*) FROM custom_emoji").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count emoji: %w", err)
	}
	return count, nil
}

func scanEmoji(row pgx.Row) (*Emoji, error) {
	var e Emoji
	err := row.Scan(&e.ID, &e.Name, &e.Animated, &e.StorageKey, &e.UploaderID, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &e, nil
}
