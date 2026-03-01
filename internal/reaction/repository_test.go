package reaction

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSentinelErrors(t *testing.T) {
	t.Parallel()

	if ErrNotFound.Error() != "reaction not found" {
		t.Errorf("ErrNotFound = %q, want %q", ErrNotFound.Error(), "reaction not found")
	}
	if ErrAlreadyReacted.Error() != "user has already reacted with this emoji" {
		t.Errorf("ErrAlreadyReacted = %q, want %q", ErrAlreadyReacted.Error(), "user has already reacted with this emoji")
	}

	// Sentinel errors must be distinguishable.
	if errors.Is(ErrNotFound, ErrAlreadyReacted) {
		t.Error("ErrNotFound and ErrAlreadyReacted must not be equal")
	}
}

func TestSummary(t *testing.T) {
	t.Parallel()

	customID := uuid.New()
	thumbsUp := "👍"

	tests := []struct {
		name    string
		summary Summary
		wantID  *uuid.UUID
		wantStr *string
	}{
		{
			name:    "unicode emoji summary",
			summary: Summary{EmojiUnicode: &thumbsUp, Count: 5},
			wantStr: &thumbsUp,
		},
		{
			name:    "custom emoji summary",
			summary: Summary{EmojiID: &customID, Count: 3},
			wantID:  &customID,
		},
		{
			name:    "zero count",
			summary: Summary{EmojiUnicode: &thumbsUp, Count: 0},
			wantStr: &thumbsUp,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.wantID != nil {
				if tt.summary.EmojiID == nil || *tt.summary.EmojiID != *tt.wantID {
					t.Errorf("EmojiID = %v, want %v", tt.summary.EmojiID, tt.wantID)
				}
			} else if tt.summary.EmojiID != nil {
				t.Errorf("EmojiID = %v, want nil", tt.summary.EmojiID)
			}
			if tt.wantStr != nil {
				if tt.summary.EmojiUnicode == nil || *tt.summary.EmojiUnicode != *tt.wantStr {
					t.Errorf("EmojiUnicode = %v, want %q", tt.summary.EmojiUnicode, *tt.wantStr)
				}
			} else if tt.summary.EmojiUnicode != nil {
				t.Errorf("EmojiUnicode = %v, want nil", tt.summary.EmojiUnicode)
			}
		})
	}
}

func TestReaction_EmojiMutualExclusivity(t *testing.T) {
	t.Parallel()

	customID := uuid.New()
	thumbsUp := "👍"

	tests := []struct {
		name         string
		emojiID      *uuid.UUID
		emojiUnicode *string
		wantCustom   bool
		wantUnicode  bool
	}{
		{
			name:         "unicode only",
			emojiUnicode: &thumbsUp,
			wantUnicode:  true,
		},
		{
			name:       "custom only",
			emojiID:    &customID,
			wantCustom: true,
		},
		{
			name: "neither set",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rxn := Reaction{
				ID:           uuid.New(),
				MessageID:    uuid.New(),
				UserID:       uuid.New(),
				EmojiID:      tt.emojiID,
				EmojiUnicode: tt.emojiUnicode,
			}
			hasCustom := rxn.EmojiID != nil
			hasUnicode := rxn.EmojiUnicode != nil
			if hasCustom != tt.wantCustom {
				t.Errorf("has custom emoji = %v, want %v", hasCustom, tt.wantCustom)
			}
			if hasUnicode != tt.wantUnicode {
				t.Errorf("has unicode emoji = %v, want %v", hasUnicode, tt.wantUnicode)
			}
		})
	}
}

func TestCollectReactions_Empty(t *testing.T) {
	t.Parallel()

	rows := &fakeRows{reactions: nil}
	result, err := collectReactions(rows)
	if err != nil {
		t.Fatalf("collectReactions(empty) error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("collectReactions(empty) returned %d reactions, want 0", len(result))
	}
}

func TestCollectReactions_MultipleRows(t *testing.T) {
	t.Parallel()

	thumbsUp := "👍"
	fire := "🔥"
	customID := uuid.New()
	msgID := uuid.New()

	rows := &fakeRows{
		reactions: []Reaction{
			{ID: uuid.New(), MessageID: msgID, UserID: uuid.New(), EmojiUnicode: &thumbsUp, Username: "alice"},
			{ID: uuid.New(), MessageID: msgID, UserID: uuid.New(), EmojiID: &customID, Username: "bob"},
			{ID: uuid.New(), MessageID: msgID, UserID: uuid.New(), EmojiUnicode: &fire, Username: "charlie"},
		},
	}

	result, err := collectReactions(rows)
	if err != nil {
		t.Fatalf("collectReactions error = %v", err)
	}
	if len(result) != 3 {
		t.Fatalf("collectReactions returned %d reactions, want 3", len(result))
	}
	if result[0].Username != "alice" {
		t.Errorf("result[0].Username = %q, want %q", result[0].Username, "alice")
	}
	if result[1].EmojiID == nil || *result[1].EmojiID != customID {
		t.Errorf("result[1].EmojiID = %v, want %v", result[1].EmojiID, customID)
	}
	if result[2].EmojiUnicode == nil || *result[2].EmojiUnicode != fire {
		t.Errorf("result[2].EmojiUnicode = %v, want %q", result[2].EmojiUnicode, fire)
	}
}

func TestCollectReactions_ScanError(t *testing.T) {
	t.Parallel()

	scanErr := errors.New("column type mismatch")
	rows := &fakeRows{scanErr: scanErr}

	_, err := collectReactions(rows)
	if err == nil {
		t.Fatal("collectReactions with scan error returned nil error")
	}
	if !errors.Is(err, scanErr) {
		t.Errorf("error = %v, want wrapped %v", err, scanErr)
	}
}

func TestCollectReactions_IterationError(t *testing.T) {
	t.Parallel()

	iterErr := errors.New("connection lost")
	rows := &fakeRows{iterErr: iterErr}

	_, err := collectReactions(rows)
	if err == nil {
		t.Fatal("collectReactions with iteration error returned nil error")
	}
	if !errors.Is(err, iterErr) {
		t.Errorf("error = %v, want wrapped %v", err, iterErr)
	}
}

// fakeRows implements the subset of pgx.Rows used by collectReactions. Each call to Scan returns the next pre-loaded
// Reaction, or the configured scanErr. After all rows are consumed, Err returns iterErr.
type fakeRows struct {
	reactions []Reaction
	pos       int
	scanErr   error
	iterErr   error
}

func (r *fakeRows) Next() bool {
	if r.scanErr != nil && r.pos == 0 {
		// Return true once so collectReactions calls Scan, which will return the error.
		r.pos = -1
		return true
	}
	if r.pos < 0 {
		return false
	}
	return r.pos < len(r.reactions)
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	rxn := r.reactions[r.pos]
	r.pos++

	// collectReactions scans 7 columns: id, message_id, user_id, emoji_id, emoji_unicode, created_at, username.
	if len(dest) != 7 {
		return errors.New("unexpected number of scan destinations")
	}
	*dest[0].(*uuid.UUID) = rxn.ID
	*dest[1].(*uuid.UUID) = rxn.MessageID
	*dest[2].(*uuid.UUID) = rxn.UserID
	*dest[3].(**uuid.UUID) = rxn.EmojiID
	*dest[4].(**string) = rxn.EmojiUnicode
	*dest[5].(*time.Time) = rxn.CreatedAt
	*dest[6].(*string) = rxn.Username
	return nil
}

func (r *fakeRows) Err() error {
	return r.iterErr
}

func (r *fakeRows) Close()                                       {}
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

// fakeRepository implements Repository for testing code that depends on the reaction repository interface.
type fakeRepository struct {
	reactions []Reaction
	addErr    error
	removeErr error
	listErr   error
}

func (f *fakeRepository) Add(_ context.Context, messageID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) (*Reaction, error) {
	if f.addErr != nil {
		return nil, f.addErr
	}
	// Check for duplicate (simulating unique constraint).
	for _, rxn := range f.reactions {
		if rxn.MessageID == messageID && rxn.UserID == userID && ptrEq(rxn.EmojiID, emojiID) && strPtrEq(rxn.EmojiUnicode, emojiUnicode) {
			return nil, ErrAlreadyReacted
		}
	}
	rxn := Reaction{
		ID:           uuid.New(),
		MessageID:    messageID,
		UserID:       userID,
		EmojiID:      emojiID,
		EmojiUnicode: emojiUnicode,
	}
	f.reactions = append(f.reactions, rxn)
	return &rxn, nil
}

func (f *fakeRepository) Remove(_ context.Context, messageID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) error {
	if f.removeErr != nil {
		return f.removeErr
	}
	for i, rxn := range f.reactions {
		if rxn.MessageID == messageID && rxn.UserID == userID && ptrEq(rxn.EmojiID, emojiID) && strPtrEq(rxn.EmojiUnicode, emojiUnicode) {
			f.reactions = append(f.reactions[:i], f.reactions[i+1:]...)
			return nil
		}
	}
	return ErrNotFound
}

func (f *fakeRepository) ListByMessage(_ context.Context, messageID uuid.UUID) ([]Reaction, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var result []Reaction
	for _, rxn := range f.reactions {
		if rxn.MessageID == messageID {
			result = append(result, rxn)
		}
	}
	return result, nil
}

func (f *fakeRepository) ListByEmoji(_ context.Context, messageID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) ([]Reaction, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var result []Reaction
	for _, rxn := range f.reactions {
		if rxn.MessageID != messageID {
			continue
		}
		if emojiID != nil && ptrEq(rxn.EmojiID, emojiID) {
			result = append(result, rxn)
		} else if emojiUnicode != nil && strPtrEq(rxn.EmojiUnicode, emojiUnicode) {
			result = append(result, rxn)
		}
	}
	return result, nil
}

func (f *fakeRepository) SummariesByMessages(_ context.Context, messageIDs []uuid.UUID) (map[uuid.UUID][]Summary, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	idSet := make(map[uuid.UUID]bool, len(messageIDs))
	for _, id := range messageIDs {
		idSet[id] = true
	}
	result := make(map[uuid.UUID][]Summary)
	type emojiKey struct {
		msgID        uuid.UUID
		emojiID      uuid.UUID
		emojiUnicode string
	}
	counts := make(map[emojiKey]int)
	var order []emojiKey
	for _, rxn := range f.reactions {
		if !idSet[rxn.MessageID] {
			continue
		}
		var eid uuid.UUID
		var euni string
		if rxn.EmojiID != nil {
			eid = *rxn.EmojiID
		}
		if rxn.EmojiUnicode != nil {
			euni = *rxn.EmojiUnicode
		}
		key := emojiKey{msgID: rxn.MessageID, emojiID: eid, emojiUnicode: euni}
		if counts[key] == 0 {
			order = append(order, key)
		}
		counts[key]++
	}
	for _, key := range order {
		s := Summary{Count: counts[key]}
		if key.emojiID != (uuid.UUID{}) {
			id := key.emojiID
			s.EmojiID = &id
		}
		if key.emojiUnicode != "" {
			uni := key.emojiUnicode
			s.EmojiUnicode = &uni
		}
		result[key.msgID] = append(result[key.msgID], s)
	}
	return result, nil
}

func (f *fakeRepository) UserReactionsByMessages(_ context.Context, messageIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]map[string]bool, error) {
	if len(messageIDs) == 0 {
		return nil, nil
	}
	idSet := make(map[uuid.UUID]bool, len(messageIDs))
	for _, id := range messageIDs {
		idSet[id] = true
	}
	result := make(map[uuid.UUID]map[string]bool)
	for _, rxn := range f.reactions {
		if !idSet[rxn.MessageID] || rxn.UserID != userID {
			continue
		}
		if result[rxn.MessageID] == nil {
			result[rxn.MessageID] = make(map[string]bool)
		}
		if rxn.EmojiID != nil {
			result[rxn.MessageID]["custom:"+rxn.EmojiID.String()] = true
		} else if rxn.EmojiUnicode != nil {
			result[rxn.MessageID][*rxn.EmojiUnicode] = true
		}
	}
	return result, nil
}

func ptrEq(a, b *uuid.UUID) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func strPtrEq(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// TestFakeRepository_Add verifies the in-memory fake correctly simulates insert and duplicate detection.
func TestFakeRepository_Add(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgID := uuid.New()
	userID := uuid.New()
	thumbsUp := "👍"

	rxn, err := repo.Add(ctx, msgID, userID, nil, &thumbsUp)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if rxn.MessageID != msgID {
		t.Errorf("MessageID = %v, want %v", rxn.MessageID, msgID)
	}
	if rxn.UserID != userID {
		t.Errorf("UserID = %v, want %v", rxn.UserID, userID)
	}

	// Duplicate reaction returns ErrAlreadyReacted.
	_, err = repo.Add(ctx, msgID, userID, nil, &thumbsUp)
	if !errors.Is(err, ErrAlreadyReacted) {
		t.Errorf("duplicate Add() error = %v, want %v", err, ErrAlreadyReacted)
	}
}

func TestFakeRepository_AddCustomEmoji(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgID := uuid.New()
	userID := uuid.New()
	emojiID := uuid.New()

	rxn, err := repo.Add(ctx, msgID, userID, &emojiID, nil)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if rxn.EmojiID == nil || *rxn.EmojiID != emojiID {
		t.Errorf("EmojiID = %v, want %v", rxn.EmojiID, emojiID)
	}
}

func TestFakeRepository_AddInjectedError(t *testing.T) {
	t.Parallel()

	injected := errors.New("database unavailable")
	repo := &fakeRepository{addErr: injected}

	_, err := repo.Add(context.Background(), uuid.New(), uuid.New(), nil, strPtr("👍"))
	if !errors.Is(err, injected) {
		t.Errorf("Add() error = %v, want %v", err, injected)
	}
}

func TestFakeRepository_Remove(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgID := uuid.New()
	userID := uuid.New()
	thumbsUp := "👍"

	_, err := repo.Add(ctx, msgID, userID, nil, &thumbsUp)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := repo.Remove(ctx, msgID, userID, nil, &thumbsUp); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	// Removing again returns ErrNotFound.
	err = repo.Remove(ctx, msgID, userID, nil, &thumbsUp)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("second Remove() error = %v, want %v", err, ErrNotFound)
	}
}

func TestFakeRepository_RemoveNonExistent(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	err := repo.Remove(context.Background(), uuid.New(), uuid.New(), nil, strPtr("👍"))
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Remove() error = %v, want %v", err, ErrNotFound)
	}
}

func TestFakeRepository_ListByMessage(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgA := uuid.New()
	msgB := uuid.New()
	userID := uuid.New()
	thumbsUp := "👍"
	fire := "🔥"

	mustAdd(t, repo, msgA, userID, nil, &thumbsUp)
	mustAdd(t, repo, msgA, userID, nil, &fire)
	mustAdd(t, repo, msgB, userID, nil, &thumbsUp)

	result, err := repo.ListByMessage(ctx, msgA)
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListByMessage() returned %d, want 2", len(result))
	}
}

func TestFakeRepository_ListByEmoji(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgID := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	thumbsUp := "👍"
	fire := "🔥"

	mustAdd(t, repo, msgID, userA, nil, &thumbsUp)
	mustAdd(t, repo, msgID, userB, nil, &thumbsUp)
	mustAdd(t, repo, msgID, userA, nil, &fire)

	result, err := repo.ListByEmoji(ctx, msgID, nil, &thumbsUp)
	if err != nil {
		t.Fatalf("ListByEmoji() error = %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("ListByEmoji() returned %d, want 2", len(result))
	}
}

func TestFakeRepository_SummariesByMessages(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgA := uuid.New()
	msgB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	thumbsUp := "👍"
	fire := "🔥"
	customID := uuid.New()

	mustAdd(t, repo, msgA, userA, nil, &thumbsUp)
	mustAdd(t, repo, msgA, userB, nil, &thumbsUp)
	mustAdd(t, repo, msgA, userA, nil, &fire)
	mustAdd(t, repo, msgB, userA, &customID, nil)

	t.Run("multiple messages", func(t *testing.T) {
		result, err := repo.SummariesByMessages(ctx, []uuid.UUID{msgA, msgB})
		if err != nil {
			t.Fatalf("SummariesByMessages() error = %v", err)
		}
		if len(result[msgA]) != 2 {
			t.Errorf("msgA summaries = %d, want 2", len(result[msgA]))
		}
		if result[msgA][0].Count != 2 {
			t.Errorf("msgA thumbsUp count = %d, want 2", result[msgA][0].Count)
		}
		if result[msgA][1].Count != 1 {
			t.Errorf("msgA fire count = %d, want 1", result[msgA][1].Count)
		}
		if len(result[msgB]) != 1 {
			t.Errorf("msgB summaries = %d, want 1", len(result[msgB]))
		}
		if result[msgB][0].Count != 1 {
			t.Errorf("msgB custom count = %d, want 1", result[msgB][0].Count)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result, err := repo.SummariesByMessages(ctx, nil)
		if err != nil {
			t.Fatalf("SummariesByMessages(nil) error = %v", err)
		}
		if result != nil {
			t.Errorf("SummariesByMessages(nil) = %v, want nil", result)
		}
	})

	t.Run("no matching messages", func(t *testing.T) {
		result, err := repo.SummariesByMessages(ctx, []uuid.UUID{uuid.New()})
		if err != nil {
			t.Fatalf("SummariesByMessages(unknown) error = %v", err)
		}
		if len(result) != 0 {
			t.Errorf("SummariesByMessages(unknown) has %d entries, want 0", len(result))
		}
	})
}

func TestFakeRepository_UserReactionsByMessages(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgA := uuid.New()
	msgB := uuid.New()
	userA := uuid.New()
	userB := uuid.New()
	thumbsUp := "👍"
	customID := uuid.New()

	mustAdd(t, repo, msgA, userA, nil, &thumbsUp)
	mustAdd(t, repo, msgA, userB, nil, &thumbsUp)
	mustAdd(t, repo, msgB, userA, &customID, nil)

	t.Run("returns user reactions", func(t *testing.T) {
		result, err := repo.UserReactionsByMessages(ctx, []uuid.UUID{msgA, msgB}, userA)
		if err != nil {
			t.Fatalf("UserReactionsByMessages() error = %v", err)
		}
		if !result[msgA][thumbsUp] {
			t.Error("expected userA to have thumbsUp on msgA")
		}
		expectedKey := "custom:" + customID.String()
		if !result[msgB][expectedKey] {
			t.Error("expected userA to have custom emoji on msgB")
		}
	})

	t.Run("excludes other users reactions", func(t *testing.T) {
		result, err := repo.UserReactionsByMessages(ctx, []uuid.UUID{msgA, msgB}, userB)
		if err != nil {
			t.Fatalf("UserReactionsByMessages() error = %v", err)
		}
		if !result[msgA][thumbsUp] {
			t.Error("expected userB to have thumbsUp on msgA")
		}
		if result[msgB] != nil {
			t.Errorf("expected userB to have no reactions on msgB, got %v", result[msgB])
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result, err := repo.UserReactionsByMessages(ctx, nil, userA)
		if err != nil {
			t.Fatalf("UserReactionsByMessages(nil) error = %v", err)
		}
		if result != nil {
			t.Errorf("UserReactionsByMessages(nil) = %v, want nil", result)
		}
	})
}

func TestFakeRepository_DifferentEmojiTypesSameUser(t *testing.T) {
	t.Parallel()

	repo := &fakeRepository{}
	ctx := context.Background()
	msgID := uuid.New()
	userID := uuid.New()
	thumbsUp := "👍"
	customID := uuid.New()

	// A user can react with both a unicode and custom emoji on the same message.
	_, err := repo.Add(ctx, msgID, userID, nil, &thumbsUp)
	if err != nil {
		t.Fatalf("Add unicode error = %v", err)
	}
	_, err = repo.Add(ctx, msgID, userID, &customID, nil)
	if err != nil {
		t.Fatalf("Add custom error = %v", err)
	}

	result, err := repo.ListByMessage(ctx, msgID)
	if err != nil {
		t.Fatalf("ListByMessage() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("ListByMessage() returned %d, want 2", len(result))
	}
}

func strPtr(s string) *string { return &s }

// mustAdd is a test helper that adds a reaction and fails the test if an error occurs.
func mustAdd(t *testing.T, repo *fakeRepository, msgID, userID uuid.UUID, emojiID *uuid.UUID, emojiUnicode *string) {
	t.Helper()
	if _, err := repo.Add(context.Background(), msgID, userID, emojiID, emojiUnicode); err != nil {
		t.Fatalf("mustAdd: %v", err)
	}
}
