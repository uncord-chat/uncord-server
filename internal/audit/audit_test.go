package audit

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// fakeRepo records Create calls and returns canned List results.
type fakeRepo struct {
	created []Entry
	listFn  func(context.Context, ListParams) ([]Entry, error)
	err     error
}

func (f *fakeRepo) Create(_ context.Context, entry Entry) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, entry)
	return nil
}

func (f *fakeRepo) List(ctx context.Context, params ListParams) ([]Entry, error) {
	if f.listFn != nil {
		return f.listFn(ctx, params)
	}
	return nil, nil
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{"zero uses default", 0, DefaultLimit},
		{"negative uses default", -5, DefaultLimit},
		{"within range", 25, 25},
		{"at max", MaxLimit, MaxLimit},
		{"above max clamped", 200, MaxLimit},
		{"one", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ClampLimit(tt.in)
			if got != tt.want {
				t.Errorf("ClampLimit(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestLoggerRecord_Success(t *testing.T) {
	repo := &fakeRepo{}
	logger := NewLogger(repo, zerolog.Nop())

	entry := Entry{
		ActorID:    UUIDPtr(uuid.New()),
		Action:     RoleCreate,
		TargetType: Ptr("role"),
		TargetID:   UUIDPtr(uuid.New()),
	}

	logger.Record(context.Background(), entry)

	if len(repo.created) != 1 {
		t.Fatalf("expected 1 created entry, got %d", len(repo.created))
	}
	if repo.created[0].Action != RoleCreate {
		t.Errorf("expected action %q, got %q", RoleCreate, repo.created[0].Action)
	}
}

func TestLoggerRecord_ErrorSwallowed(t *testing.T) {
	repo := &fakeRepo{err: errors.New("db unavailable")}
	logger := NewLogger(repo, zerolog.Nop())

	entry := Entry{
		ActorID: UUIDPtr(uuid.New()),
		Action:  RoleCreate,
	}

	// Must not panic despite the repository error.
	logger.Record(context.Background(), entry)
}

func TestEntryToModel(t *testing.T) {
	now := time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC)
	entryID := uuid.New()
	actorID := uuid.New()
	targetID := uuid.New()
	changes := json.RawMessage(`{"name":"admin"}`)
	reason := "promoted"

	entry := Entry{
		ID:         entryID,
		ActorID:    &actorID,
		Action:     RoleCreate,
		TargetType: Ptr("role"),
		TargetID:   &targetID,
		Changes:    changes,
		Reason:     &reason,
		CreatedAt:  now,
	}

	m := entry.ToModel()

	if m.ID != entryID.String() {
		t.Errorf("ID = %q, want %q", m.ID, entryID.String())
	}
	if m.ActorID != actorID.String() {
		t.Errorf("ActorID = %q, want %q", m.ActorID, actorID.String())
	}
	if m.Action != "role.create" {
		t.Errorf("Action = %q, want %q", m.Action, "role.create")
	}
	if m.TargetType == nil || *m.TargetType != "role" {
		t.Errorf("TargetType = %v, want %q", m.TargetType, "role")
	}
	if m.TargetID == nil || *m.TargetID != targetID.String() {
		t.Errorf("TargetID = %v, want %q", m.TargetID, targetID.String())
	}
	if string(m.Changes) != `{"name":"admin"}` {
		t.Errorf("Changes = %s, want %s", m.Changes, `{"name":"admin"}`)
	}
	if m.Reason == nil || *m.Reason != "promoted" {
		t.Errorf("Reason = %v, want %q", m.Reason, "promoted")
	}
	if m.CreatedAt != "2025-06-15T12:00:00Z" {
		t.Errorf("CreatedAt = %q, want %q", m.CreatedAt, "2025-06-15T12:00:00Z")
	}
}

func TestEntryToModel_NilOptionalFields(t *testing.T) {
	entry := Entry{
		ID:        uuid.New(),
		ActorID:   UUIDPtr(uuid.New()),
		Action:    MemberKick,
		CreatedAt: time.Now(),
	}

	m := entry.ToModel()

	if m.TargetType != nil {
		t.Errorf("TargetType = %v, want nil", m.TargetType)
	}
	if m.TargetID != nil {
		t.Errorf("TargetID = %v, want nil", m.TargetID)
	}
	if m.Reason != nil {
		t.Errorf("Reason = %v, want nil", m.Reason)
	}
}

func TestMarshalChanges(t *testing.T) {
	data := MarshalChanges(map[string]string{"role_id": "abc"})
	if data == nil {
		t.Fatal("expected non-nil result")
	}

	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["role_id"] != "abc" {
		t.Errorf("role_id = %q, want %q", m["role_id"], "abc")
	}
}

func TestMarshalChanges_Nil(t *testing.T) {
	// Channels cannot be marshalled to JSON and should return nil.
	data := MarshalChanges(make(chan int))
	if data != nil {
		t.Errorf("expected nil for unmarshallable value, got %s", data)
	}
}

func TestPtr(t *testing.T) {
	s := Ptr("role")
	if s == nil || *s != "role" {
		t.Errorf("Ptr(%q) = %v, want pointer to %q", "role", s, "role")
	}
}

func TestUUIDPtr(t *testing.T) {
	id := uuid.New()
	p := UUIDPtr(id)
	if p == nil || *p != id {
		t.Errorf("UUIDPtr(%v) = %v, want pointer to %v", id, p, id)
	}
}
