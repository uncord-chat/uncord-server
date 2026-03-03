package thread

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

const selectColumns = "id, channel_id, parent_message_id, name, archived, locked, created_at, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed thread repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
}

// Create inserts a new thread inside a transaction that validates the parent message exists, belongs to the given
// channel, and is not deleted. A unique constraint on parent_message_id ensures one thread per message.
func (r *PGRepository) Create(ctx context.Context, params CreateParams) (*Thread, error) {
	var t Thread
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var exists bool
		err := tx.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM messages WHERE id = $1 AND channel_id = $2 AND deleted_at IS NULL)",
			params.ParentMessageID, params.ChannelID,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check parent message: %w", err)
		}
		if !exists {
			return ErrMessageNotFound
		}

		row := tx.QueryRow(ctx,
			fmt.Sprintf(
				`INSERT INTO threads (channel_id, parent_message_id, name)
				 VALUES ($1, $2, $3)
				 RETURNING %s`, selectColumns),
			params.ChannelID, params.ParentMessageID, params.Name,
		)
		var scanErr error
		t, scanErr = scanThreadRow(row)
		if scanErr != nil {
			if postgres.IsUniqueViolation(scanErr) {
				return ErrAlreadyExists
			}
			return fmt.Errorf("insert thread: %w", scanErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetByID returns the thread matching the given ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Thread, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM threads WHERE id = $1", selectColumns), id,
	)
	t, err := scanThread(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query thread by id: %w", err)
	}
	return t, nil
}

// ListByChannel returns threads in a channel ordered newest first. When before is non-nil, only threads created before
// the referenced thread are returned (cursor-based pagination).
func (r *PGRepository) ListByChannel(ctx context.Context, channelID uuid.UUID, before *uuid.UUID, limit int) ([]Thread, error) {
	var rows pgx.Rows
	var err error
	if before != nil {
		rows, err = r.db.Query(ctx,
			fmt.Sprintf(`SELECT %s FROM threads WHERE channel_id = $1
				AND (created_at, id) < (SELECT created_at, id FROM threads WHERE id = $2)
				ORDER BY created_at DESC, id DESC LIMIT $3`, selectColumns),
			channelID, *before, limit,
		)
	} else {
		rows, err = r.db.Query(ctx,
			fmt.Sprintf("SELECT %s FROM threads WHERE channel_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2", selectColumns),
			channelID, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query threads: %w", err)
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		t, scanErr := scanThread(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		threads = append(threads, *t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate threads: %w", err)
	}
	return threads, nil
}

// Update applies the non-nil fields in params to the thread row and returns the updated thread. A locked thread rejects
// all changes unless the update explicitly sets locked to false. Nil pointer fields are left unchanged via COALESCE.
func (r *PGRepository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Thread, error) {
	if params.Name == nil && params.Archived == nil && params.Locked == nil {
		return r.GetByID(ctx, id)
	}

	var t *Thread
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		// Fetch the current state to enforce lock semantics.
		row := tx.QueryRow(ctx,
			fmt.Sprintf("SELECT %s FROM threads WHERE id = $1 FOR UPDATE", selectColumns), id,
		)
		current, scanErr := scanThread(row)
		if scanErr != nil {
			if errors.Is(scanErr, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return fmt.Errorf("fetch thread for update: %w", scanErr)
		}

		// A locked thread rejects changes unless the update explicitly unlocks it.
		unlocking := params.Locked != nil && !*params.Locked
		if current.Locked && !unlocking {
			return ErrLocked
		}

		const query = `UPDATE threads SET
			name     = COALESCE(@name, name),
			archived = COALESCE(@archived, archived),
			locked   = COALESCE(@locked, locked)
			WHERE id = @id RETURNING ` + selectColumns

		args := pgx.NamedArgs{
			"id":       id,
			"name":     params.Name,
			"archived": params.Archived,
			"locked":   params.Locked,
		}

		updateRow := tx.QueryRow(ctx, query, args)
		var updateErr error
		t, updateErr = scanThread(updateRow)
		if updateErr != nil {
			return fmt.Errorf("update thread: %w", updateErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

// scanThread scans a single row into a Thread struct and returns a pointer.
func scanThread(row pgx.Row) (*Thread, error) {
	var t Thread
	err := row.Scan(
		&t.ID, &t.ChannelID, &t.ParentMessageID, &t.Name,
		&t.Archived, &t.Locked, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan thread: %w", err)
	}
	return &t, nil
}

// scanThreadRow scans a single row into a Thread value (used inside transactions where a pointer allocation is avoided
// until the transaction succeeds).
func scanThreadRow(row pgx.Row) (Thread, error) {
	var t Thread
	err := row.Scan(
		&t.ID, &t.ChannelID, &t.ParentMessageID, &t.Name,
		&t.Archived, &t.Locked, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return t, fmt.Errorf("scan thread: %w", err)
	}
	return t, nil
}
