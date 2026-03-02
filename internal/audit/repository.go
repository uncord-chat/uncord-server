package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const selectColumns = `id, actor_id, action, target_type, target_id, changes, reason, created_at`

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed audit log repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
}

// Create inserts a single audit log entry. The database generates the primary key and timestamp when not supplied.
func (r *PGRepository) Create(ctx context.Context, entry Entry) error {
	_, err := r.db.Exec(ctx,
		`INSERT INTO audit_log (actor_id, action, target_type, target_id, changes, reason)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		entry.ActorID, string(entry.Action), entry.TargetType, entry.TargetID, entry.Changes, entry.Reason,
	)
	if err != nil {
		return fmt.Errorf("insert audit log entry: %w", err)
	}
	return nil
}

// List returns audit log entries ordered by (created_at DESC, id DESC) with optional filters and cursor pagination.
func (r *PGRepository) List(ctx context.Context, params ListParams) ([]Entry, error) {
	query := `SELECT ` + selectColumns + ` FROM audit_log WHERE true`
	args := make([]any, 0, 5)
	n := 0

	if params.ActorID != nil {
		n++
		query += fmt.Sprintf(` AND actor_id = $%d`, n)
		args = append(args, *params.ActorID)
	}
	if params.ActionType != nil {
		n++
		query += fmt.Sprintf(` AND action = $%d`, n)
		args = append(args, string(*params.ActionType))
	}
	if params.TargetID != nil {
		n++
		query += fmt.Sprintf(` AND target_id = $%d`, n)
		args = append(args, *params.TargetID)
	}
	if params.Before != nil {
		n++
		query += fmt.Sprintf(` AND (created_at, id) < (SELECT created_at, id FROM audit_log WHERE id = $%d)`, n)
		args = append(args, *params.Before)
	}

	limit := ClampLimit(params.Limit)
	n++
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, n)
	args = append(args, limit)

	rows, err := r.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit log: %w", err)
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit log: %w", err)
	}
	return entries, nil
}

// scanEntry reads a single audit log row into an Entry.
func scanEntry(row pgx.Row) (*Entry, error) {
	var (
		e       Entry
		action  string
		changes []byte
	)
	err := row.Scan(&e.ID, &e.ActorID, &action, &e.TargetType, &e.TargetID, &changes, &e.Reason, &e.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan audit log entry: %w", err)
	}
	e.Action = ActionType(action)
	if changes != nil {
		e.Changes = json.RawMessage(changes)
	}
	return &e, nil
}

// UUIDPtr returns a pointer to the given UUID. It is a convenience for constructing Entry values with optional fields.
func UUIDPtr(id uuid.UUID) *uuid.UUID {
	return &id
}
