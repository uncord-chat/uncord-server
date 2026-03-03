package audit

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"github.com/uncord-chat/uncord-protocol/models"
)

// ActionType identifies the kind of administrative action that was performed.
type ActionType string

// Action type constants for all auditable administrative mutations.
const (
	MemberKick         ActionType = "member.kick"
	MemberBan          ActionType = "member.ban"
	MemberUnban        ActionType = "member.unban"
	MemberTimeout      ActionType = "member.timeout"
	MemberTimeoutClear ActionType = "member.timeout_clear"
	MemberRoleAssign   ActionType = "member.role_assign"
	MemberRoleRemove   ActionType = "member.role_remove"

	RoleCreate ActionType = "role.create"
	RoleUpdate ActionType = "role.update"
	RoleDelete ActionType = "role.delete"

	ChannelCreate ActionType = "channel.create"
	ChannelUpdate ActionType = "channel.update"
	ChannelDelete ActionType = "channel.delete"

	CategoryCreate ActionType = "category.create"
	CategoryUpdate ActionType = "category.update"
	CategoryDelete ActionType = "category.delete"

	OverrideSet    ActionType = "override.set"
	OverrideDelete ActionType = "override.delete"

	MessageDelete ActionType = "message.delete"
	MessagePin    ActionType = "message.pin"
	MessageUnpin  ActionType = "message.unpin"

	InviteCreate ActionType = "invite.create"
	InviteDelete ActionType = "invite.delete"

	EmojiCreate ActionType = "emoji.create"
	EmojiUpdate ActionType = "emoji.update"
	EmojiDelete ActionType = "emoji.delete"

	ServerUpdate ActionType = "server.update"

	OnboardingUpdate ActionType = "onboarding.update"

	// Reserved for future feature groups.
	WebhookCreate ActionType = "webhook.create"
	WebhookUpdate ActionType = "webhook.update"
	WebhookDelete ActionType = "webhook.delete"
	AutomodCreate ActionType = "automod.create"
	AutomodUpdate ActionType = "automod.update"
	AutomodDelete ActionType = "automod.delete"
	ReportResolve ActionType = "report.resolve"
)

// Pagination defaults and limits for audit log queries.
const (
	DefaultLimit = 50
	MaxLimit     = 100
)

// ClampLimit constrains a requested page size to the valid range [1, MaxLimit], defaulting to DefaultLimit when the
// input is zero or negative.
func ClampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}

// Entry represents a single row in the audit_log table. ActorID is a pointer because the column is nullable; a NULL
// value indicates the acting user has since been deleted.
type Entry struct {
	ID         uuid.UUID
	ActorID    *uuid.UUID
	Action     ActionType
	TargetType *string
	TargetID   *uuid.UUID
	Changes    json.RawMessage
	Reason     *string
	CreatedAt  time.Time
}

// ToModel converts the internal entry to the protocol response type.
func (e *Entry) ToModel() models.AuditLogEntry {
	var actorID string
	if e.ActorID != nil {
		actorID = e.ActorID.String()
	}
	m := models.AuditLogEntry{
		ID:        e.ID.String(),
		ActorID:   actorID,
		Action:    string(e.Action),
		Changes:   e.Changes,
		Reason:    e.Reason,
		CreatedAt: e.CreatedAt.Format(time.RFC3339),
	}
	if e.TargetType != nil {
		v := *e.TargetType
		m.TargetType = &v
	}
	if e.TargetID != nil {
		v := e.TargetID.String()
		m.TargetID = &v
	}
	return m
}

// ListParams holds optional filters and cursor pagination parameters for listing audit log entries.
type ListParams struct {
	ActorID    *uuid.UUID
	ActionType *ActionType
	TargetID   *uuid.UUID
	Before     *uuid.UUID
	Limit      int
}

// Repository defines persistence operations for the audit log.
type Repository interface {
	Create(ctx context.Context, entry Entry) error
	List(ctx context.Context, params ListParams) ([]Entry, error)
}

var _ Repository = (*PGRepository)(nil)

// Logger provides a fire-and-forget interface for recording audit entries. Write failures are logged but never returned
// to the caller, so audit logging cannot disrupt the primary request flow.
type Logger struct {
	repo Repository
	log  zerolog.Logger
}

// NewLogger creates a Logger that writes entries through the given repository.
func NewLogger(repo Repository, logger zerolog.Logger) *Logger {
	return &Logger{repo: repo, log: logger}
}

// Record persists a single audit log entry. Errors are logged at the error level but not propagated. Callers are
// responsible for spawning a goroutine with context.Background() if the write should not block the HTTP response.
func (l *Logger) Record(ctx context.Context, entry Entry) {
	if err := l.repo.Create(ctx, entry); err != nil {
		event := l.log.Error().Err(err).Str("action", string(entry.Action))
		if entry.ActorID != nil {
			event = event.Str("actor_id", entry.ActorID.String())
		}
		event.Msg("failed to write audit log entry")
	}
}

// Ptr returns a pointer to the given string. It is a convenience for constructing Entry values with optional fields.
func Ptr(s string) *string {
	return &s
}

// MarshalChanges serialises an arbitrary value to JSON for the changes column. If marshalling fails it returns nil,
// which stores a SQL NULL rather than blocking the audit write.
func MarshalChanges(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
