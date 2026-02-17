package role

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

// selectColumns lists the columns returned by queries that produce a *Role. Every method that scans into a Role must
// select these columns in this exact order. See scanRole.
const selectColumns = "id, name, colour, position, hoist, permissions, is_everyone, created_at, updated_at"

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db  *pgxpool.Pool
	log zerolog.Logger
}

// NewPGRepository creates a new PostgreSQL-backed role repository.
func NewPGRepository(db *pgxpool.Pool, logger zerolog.Logger) *PGRepository {
	return &PGRepository{db: db, log: logger}
}

// List returns all roles ordered by position.
func (r *PGRepository) List(ctx context.Context) ([]Role, error) {
	rows, err := r.db.Query(ctx,
		fmt.Sprintf("SELECT %s FROM roles ORDER BY position", selectColumns),
	)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()

	var roles []Role
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		roles = append(roles, *role)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate roles: %w", err)
	}
	return roles, nil
}

// GetByID returns the role matching the given ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Role, error) {
	row := r.db.QueryRow(ctx,
		fmt.Sprintf("SELECT %s FROM roles WHERE id = $1", selectColumns), id,
	)
	role, err := scanRole(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("query role by id: %w", err)
	}
	return role, nil
}

// Create inserts a new role inside a transaction that enforces the maximum count and auto-assigns a position.
func (r *PGRepository) Create(ctx context.Context, params CreateParams, maxRoles int) (*Role, error) {
	var role *Role
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var count int
		if err := tx.QueryRow(ctx, "SELECT COUNT(*) FROM roles").Scan(&count); err != nil {
			return fmt.Errorf("count roles: %w", err)
		}
		if count >= maxRoles {
			return ErrMaxRolesReached
		}

		row := tx.QueryRow(ctx,
			fmt.Sprintf(
				`INSERT INTO roles (name, colour, hoist, permissions, position)
				 VALUES ($1, $2, $3, $4, COALESCE((SELECT MAX(position) FROM roles), -1) + 1)
				 RETURNING %s`, selectColumns),
			params.Name, params.Colour, params.Hoist, params.Permissions,
		)
		var err error
		role, err = scanRole(row)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrAlreadyExists
			}
			return fmt.Errorf("insert role: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return role, nil
}

// Update applies the non-nil fields in params to the role row and returns the updated role.
//
// Safety: the query is built dynamically, but every SET clause and named arg key is a hardcoded string literal. No
// caller-supplied value enters the SQL structure; all values flow through pgx named parameter binding.
func (r *PGRepository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*Role, error) {
	var setClauses []string
	namedArgs := pgx.NamedArgs{"id": id}

	if params.Name != nil {
		setClauses = append(setClauses, "name = @name")
		namedArgs["name"] = *params.Name
	}
	if params.Colour != nil {
		setClauses = append(setClauses, "colour = @colour")
		namedArgs["colour"] = *params.Colour
	}
	if params.Position != nil {
		setClauses = append(setClauses, "position = @position")
		namedArgs["position"] = *params.Position
	}
	if params.Permissions != nil {
		setClauses = append(setClauses, "permissions = @permissions")
		namedArgs["permissions"] = *params.Permissions
	}
	if params.Hoist != nil {
		setClauses = append(setClauses, "hoist = @hoist")
		namedArgs["hoist"] = *params.Hoist
	}

	// No fields to update. Return the current row without issuing an UPDATE so the database trigger does not bump
	// updated_at. A no-op PATCH should not alter the modification timestamp.
	if len(setClauses) == 0 {
		return r.GetByID(ctx, id)
	}

	query := "UPDATE roles SET " + strings.Join(setClauses, ", ") +
		" WHERE id = @id RETURNING " + selectColumns

	row := r.db.QueryRow(ctx, query, namedArgs)
	role, err := scanRole(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		if postgres.IsUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("update role: %w", err)
	}
	return role, nil
}

// Delete removes the role with the given ID. The @everyone role cannot be deleted.
func (r *PGRepository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.db.Exec(ctx, "DELETE FROM roles WHERE id = $1 AND NOT is_everyone", id)
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// Distinguish between "not found" and "@everyone cannot be deleted" by checking if the role exists.
		var isEveryone bool
		err := r.db.QueryRow(ctx, "SELECT is_everyone FROM roles WHERE id = $1", id).Scan(&isEveryone)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("check role existence: %w", err)
		}
		return ErrEveryoneImmutable
	}
	return nil
}

// HighestPosition returns the lowest position number among the user's explicitly assigned roles (lower position =
// higher rank). The @everyone role is excluded because every member holds it, so including it would make all users
// appear to hold position 0 and defeat hierarchy enforcement. If the user holds no explicit roles, math.MaxInt is
// returned, indicating the user has the lowest possible rank.
func (r *PGRepository) HighestPosition(ctx context.Context, userID uuid.UUID) (int, error) {
	var pos *int
	err := r.db.QueryRow(ctx,
		`SELECT MIN(r.position) FROM roles r
		 JOIN member_roles mr ON r.id = mr.role_id
		 WHERE mr.user_id = $1 AND r.is_everyone = false`,
		userID,
	).Scan(&pos)
	if err != nil {
		return 0, fmt.Errorf("query highest role position: %w", err)
	}
	if pos == nil {
		return math.MaxInt, nil
	}
	return *pos, nil
}

// scanRole scans a single row into a *Role. The row must contain the columns listed in selectColumns.
func scanRole(row pgx.Row) (*Role, error) {
	var role Role
	err := row.Scan(
		&role.ID, &role.Name, &role.Colour, &role.Position, &role.Hoist,
		&role.Permissions, &role.IsEveryone, &role.CreatedAt, &role.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan role: %w", err)
	}
	return &role, nil
}
