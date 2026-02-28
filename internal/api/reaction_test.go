package api

import (
	"testing"

	"github.com/google/uuid"

	"github.com/uncord-chat/uncord-server/internal/reaction"
)

func TestParseEmojiParam(t *testing.T) {
	customID := uuid.New()

	tests := []struct {
		name         string
		param        string
		wantCustom   bool
		wantCustomID uuid.UUID
		wantUnicode  string
		wantErr      bool
	}{
		{
			name:        "unicode emoji",
			param:       "👍",
			wantUnicode: "👍",
		},
		{
			name:         "custom emoji",
			param:        "custom:" + customID.String(),
			wantCustom:   true,
			wantCustomID: customID,
		},
		{
			name:        "url-decoded unicode",
			param:       "🎉",
			wantUnicode: "🎉",
		},
		{
			name:    "empty string",
			param:   "",
			wantErr: true,
		},
		{
			name:    "invalid custom emoji ID",
			param:   "custom:not-a-uuid",
			wantErr: true,
		},
		{
			name:        "plain text treated as unicode",
			param:       "fire",
			wantUnicode: "fire",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			emojiID, emojiUnicode, err := parseEmojiParam(tt.param)

			if (err != nil) != tt.wantErr {
				t.Fatalf("parseEmojiParam(%q) error = %v, wantErr %v", tt.param, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if tt.wantCustom {
				if emojiID == nil {
					t.Fatalf("parseEmojiParam(%q) emojiID = nil, want %s", tt.param, tt.wantCustomID)
				}
				if *emojiID != tt.wantCustomID {
					t.Errorf("parseEmojiParam(%q) emojiID = %s, want %s", tt.param, *emojiID, tt.wantCustomID)
				}
				if emojiUnicode != nil {
					t.Errorf("parseEmojiParam(%q) emojiUnicode = %q, want nil", tt.param, *emojiUnicode)
				}
			} else {
				if emojiID != nil {
					t.Errorf("parseEmojiParam(%q) emojiID = %s, want nil", tt.param, *emojiID)
				}
				if emojiUnicode == nil {
					t.Fatalf("parseEmojiParam(%q) emojiUnicode = nil, want %q", tt.param, tt.wantUnicode)
				}
				if *emojiUnicode != tt.wantUnicode {
					t.Errorf("parseEmojiParam(%q) emojiUnicode = %q, want %q", tt.param, *emojiUnicode, tt.wantUnicode)
				}
			}
		})
	}
}

func TestGroupReactions(t *testing.T) {
	userA := uuid.New()
	userB := uuid.New()
	currentUser := userA

	thumbsUp := "👍"
	fire := "🔥"
	customID := uuid.New()

	rxns := []reaction.Reaction{
		{ID: uuid.New(), UserID: userA, EmojiUnicode: &thumbsUp},
		{ID: uuid.New(), UserID: userB, EmojiUnicode: &thumbsUp},
		{ID: uuid.New(), UserID: userA, EmojiUnicode: &fire},
		{ID: uuid.New(), UserID: userB, EmojiID: &customID},
	}

	result := groupReactions(rxns, currentUser)

	if len(result) != 3 {
		t.Fatalf("groupReactions returned %d summaries, want 3", len(result))
	}

	// First group: thumbsup (2 reactions, me=true)
	if result[0].Count != 2 {
		t.Errorf("result[0].Count = %d, want 2", result[0].Count)
	}
	if !result[0].Me {
		t.Error("result[0].Me = false, want true")
	}
	if result[0].EmojiUnicode == nil || *result[0].EmojiUnicode != thumbsUp {
		t.Errorf("result[0].EmojiUnicode = %v, want %q", result[0].EmojiUnicode, thumbsUp)
	}

	// Second group: fire (1 reaction, me=true)
	if result[1].Count != 1 {
		t.Errorf("result[1].Count = %d, want 1", result[1].Count)
	}
	if !result[1].Me {
		t.Error("result[1].Me = false, want true")
	}

	// Third group: custom emoji (1 reaction, me=false)
	if result[2].Count != 1 {
		t.Errorf("result[2].Count = %d, want 1", result[2].Count)
	}
	if result[2].Me {
		t.Error("result[2].Me = true, want false")
	}
	if result[2].EmojiID == nil || *result[2].EmojiID != customID.String() {
		t.Errorf("result[2].EmojiID = %v, want %q", result[2].EmojiID, customID.String())
	}
}
