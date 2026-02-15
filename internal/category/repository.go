package category

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

const selectColumns = "id, name, position, created_at, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed category repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// List returns all categories ordered by position.
func (r *PGRepository) List(ctx context.Context) ([]Category, error) {
	rows, err := r.db.Query(ctx,
		fmt.Sprintf("SELECT %s FROM categories ORDER BY position", selectColumns),
	)
	if err != nil {
		return nil, fmt.Errorf("query categories: %w", err)
	}
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		cat, err := scanCategory(rows)
		if err != nil {
			return nil, err
		}
		categories = append(categories, *cat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate categories: %w", err)
	}
	return categories, nil
}

// GetByID returns the category matching the given ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Category, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM categories WHERE id = $1", selectColumns), id,
	)
	cat, err := scanCategory(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query category by id: %w", err)
	}
	return cat, nil
}

// Create inserts a new category inside a transaction that enforces the maximum count and auto-assigns a position.
func (r *PGRepository) Create(ctx context.Context, params CreateParams, maxCategories int) (*Category, error) {
	var cat *Category
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM categories").Scan(&count); err != nil {
			return fmt.Errorf("count categories: %w", err)
		}
		if count >= maxCategories {
			return ErrMaxCategoriesReached
		}

		row := tx.QueryRow(ctx,
			fmt.Sprintf(
				`INSERT INTO categories (name, position)
				 VALUES ($1, COALESCE((SELECT MAX(position) FROM categories), -1) + 1)
				 RETURNING %s`, selectColumns),
			params.Name,
		)
		var err error
		cat, err = scanCategory(row)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrAlreadyExists
			}
			return fmt.Errorf("insert category: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return cat, nil
}

// Update applies the non-nil fields in params to the category row and returns the updated category.
func (r *PGRepository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Category, error) {
	setClauses := make([]string, 0, 2)
	args := make([]any, 0, 3)
	argPos := 1

	if params.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argPos))
		args = append(args, *params.Name)
		argPos++
	}
	if params.Position != nil {
		setClauses = append(setClauses, fmt.Sprintf("position = $%d", argPos))
		args = append(args, *params.Position)
		argPos++
	}

	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if len(setClauses) == 0 {
		return r.GetByID(ctx, id)
	}

	query := fmt.Sprintf(
		"UPDATE categories SET %s WHERE id = $%d RETURNING %s",
		strings.Join(setClauses, ", "), argPos, selectColumns,
	)
	args = append(args, id)

	row := r.db.QueryRow(ctx, query, args...)
	cat, err := scanCategory(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if postgres.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("update category: %w", err)
	}
	return cat, nil
}

// Delete removes the category with the given ID. The FK constraint on channels.category_id is ON DELETE SET NULL, so
// channels in this category are automatically uncategorized.
func (r *PGRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM categories WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete category: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// scanCategory scans a single row into a Category struct.
func scanCategory(row pgx.Row) (*Category, error) {
	var cat Category
	err := row.Scan(&cat.ID, &cat.Name, &cat.Position, &cat.CreatedAt, &cat.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan category: %w", err)
	}
	return &cat, nil
}
