package dm

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/uncord-chat/uncord-server/internal/postgres"
)

// PGRepository implements Repository using PostgreSQL.
type PGRepository struct {
	db *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed DM repository.
func NewPGRepository(db *pgxpool.Pool) *PGRepository {
	return &PGRepository{db: db}
}

// CreateDM creates a 1:1 DM channel between two users. If a channel already exists between them, the existing channel
// is returned. The check and insert run in a single transaction to prevent races.
func (r *PGRepository) CreateDM(ctx context.Context, params CreateDMParams) (*Channel, error) {
	var ch Channel
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		// Check for an existing 1:1 DM between the two users.
		row := tx.QueryRow(ctx,
			`SELECT dc.id, dc.type, dc.name, dc.owner_id, dc.created_at, dc.updated_at
			 FROM dm_channels dc
			 WHERE dc.type = 'dm'
			   AND EXISTS (SELECT 1 FROM dm_participants WHERE dm_channel_id = dc.id AND user_id = $1)
			   AND EXISTS (SELECT 1 FROM dm_participants WHERE dm_channel_id = dc.id AND user_id = $2)
			 LIMIT 1`, params.CreatorID, params.RecipientID)

		err := row.Scan(&ch.ID, &ch.Type, &ch.Name, &ch.OwnerID, &ch.CreatedAt, &ch.UpdatedAt)
		if err == nil {
			return nil // Existing channel found.
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("check existing dm: %w", err)
		}

		// No existing channel; create a new one.
		err = tx.QueryRow(ctx,
			`INSERT INTO dm_channels (type) VALUES ('dm')
			 RETURNING id, type, name, owner_id, created_at, updated_at`,
		).Scan(&ch.ID, &ch.Type, &ch.Name, &ch.OwnerID, &ch.CreatedAt, &ch.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert dm channel: %w", err)
		}

		// Add both participants.
		for _, uid := range []uuid.UUID{params.CreatorID, params.RecipientID} {
			_, err := tx.Exec(ctx,
				`INSERT INTO dm_participants (dm_channel_id, user_id) VALUES ($1, $2)`, ch.ID, uid)
			if err != nil {
				return fmt.Errorf("insert participant: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// CreateGroupDM creates a new group DM channel with the specified participants. The owner is automatically included as
// a participant. Enforces the MaxGroupDMParticipants limit.
func (r *PGRepository) CreateGroupDM(ctx context.Context, params CreateGroupDMParams) (*Channel, error) {
	allParticipants := uniqueUUIDs(append(params.ParticipantIDs, params.OwnerID))
	if len(allParticipants) > MaxGroupDMParticipants {
		return nil, ErrGroupDMFull
	}

	var ch Channel
	err := postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO dm_channels (type, name, owner_id) VALUES ('group_dm', $1, $2)
			 RETURNING id, type, name, owner_id, created_at, updated_at`,
			params.Name, params.OwnerID,
		).Scan(&ch.ID, &ch.Type, &ch.Name, &ch.OwnerID, &ch.CreatedAt, &ch.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert group dm channel: %w", err)
		}

		for _, uid := range allParticipants {
			_, err := tx.Exec(ctx,
				`INSERT INTO dm_participants (dm_channel_id, user_id) VALUES ($1, $2)`, ch.ID, uid)
			if err != nil {
				return fmt.Errorf("insert participant: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ch, nil
}

// GetByID returns a DM channel by its ID.
func (r *PGRepository) GetByID(ctx context.Context, id uuid.UUID) (*Channel, error) {
	var ch Channel
	err := r.db.QueryRow(ctx,
		`SELECT id, type, name, owner_id, created_at, updated_at FROM dm_channels WHERE id = $1`, id,
	).Scan(&ch.ID, &ch.Type, &ch.Name, &ch.OwnerID, &ch.CreatedAt, &ch.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDMNotFound
		}
		return nil, fmt.Errorf("get dm channel: %w", err)
	}
	return &ch, nil
}

// ListForUser returns all DM channels the user participates in, ordered by most recent update.
func (r *PGRepository) ListForUser(ctx context.Context, userID uuid.UUID) ([]Channel, error) {
	rows, err := r.db.Query(ctx,
		`SELECT dc.id, dc.type, dc.name, dc.owner_id, dc.created_at, dc.updated_at
		 FROM dm_channels dc
		 JOIN dm_participants dp ON dp.dm_channel_id = dc.id
		 WHERE dp.user_id = $1
		 ORDER BY dc.updated_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list dm channels: %w", err)
	}
	defer rows.Close()

	var channels []Channel
	for rows.Next() {
		var ch Channel
		if err := rows.Scan(&ch.ID, &ch.Type, &ch.Name, &ch.OwnerID, &ch.CreatedAt, &ch.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan dm channel: %w", err)
		}
		channels = append(channels, ch)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list dm channels: %w", err)
	}
	return channels, nil
}

// AddParticipant adds a user to a group DM channel. Returns ErrAlreadyParticipant on duplicate and ErrGroupDMFull if
// the channel is at capacity.
func (r *PGRepository) AddParticipant(ctx context.Context, channelID, userID uuid.UUID) error {
	return postgres.WithTx(ctx, r.db, func(tx pgx.Tx) error {
		var count int
		err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM dm_participants WHERE dm_channel_id = $1`, channelID).Scan(&count)
		if err != nil {
			return fmt.Errorf("count participants: %w", err)
		}
		if count >= MaxGroupDMParticipants {
			return ErrGroupDMFull
		}

		_, err = tx.Exec(ctx,
			`INSERT INTO dm_participants (dm_channel_id, user_id) VALUES ($1, $2)`, channelID, userID)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return ErrAlreadyParticipant
			}
			return fmt.Errorf("insert participant: %w", err)
		}
		return nil
	})
}

// RemoveParticipant removes a user from a DM channel.
func (r *PGRepository) RemoveParticipant(ctx context.Context, channelID, userID uuid.UUID) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM dm_participants WHERE dm_channel_id = $1 AND user_id = $2`, channelID, userID)
	if err != nil {
		return fmt.Errorf("delete participant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotParticipant
	}
	return nil
}

// ListParticipants returns all participants of a DM channel.
func (r *PGRepository) ListParticipants(ctx context.Context, channelID uuid.UUID) ([]Participant, error) {
	rows, err := r.db.Query(ctx,
		`SELECT dm_channel_id, user_id, joined_at FROM dm_participants WHERE dm_channel_id = $1 ORDER BY joined_at`,
		channelID)
	if err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	defer rows.Close()

	var participants []Participant
	for rows.Next() {
		var p Participant
		if err := rows.Scan(&p.DMChannelID, &p.UserID, &p.JoinedAt); err != nil {
			return nil, fmt.Errorf("scan participant: %w", err)
		}
		participants = append(participants, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list participants: %w", err)
	}
	return participants, nil
}

// IsParticipant checks whether a user is a participant of a DM channel.
func (r *PGRepository) IsParticipant(ctx context.Context, channelID, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM dm_participants WHERE dm_channel_id = $1 AND user_id = $2)`,
		channelID, userID).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check participant: %w", err)
	}
	return exists, nil
}

// ListDMPeers returns the distinct user IDs of all users who share any DM channel with the given user.
func (r *PGRepository) ListDMPeers(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := r.db.Query(ctx,
		`SELECT DISTINCT user_id FROM dm_participants
		 WHERE dm_channel_id IN (SELECT dm_channel_id FROM dm_participants WHERE user_id = $1)
		   AND user_id != $1`, userID)
	if err != nil {
		return nil, fmt.Errorf("list dm peers: %w", err)
	}
	defer rows.Close()

	var peers []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan peer: %w", err)
		}
		peers = append(peers, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list dm peers: %w", err)
	}
	return peers, nil
}

// uniqueUUIDs deduplicates a UUID slice while preserving order.
func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(ids))
	result := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
