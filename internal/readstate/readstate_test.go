package readstate

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestToModel(t *testing.T) {
	t.Parallel()

	channelID := uuid.New()
	messageID := uuid.New()
	now := time.Now()

	t.Run("with last message", func(t *testing.T) {
		t.Parallel()
		rs := ReadState{
			UserID:        uuid.New(),
			ChannelID:     channelID,
			LastMessageID: &messageID,
			MentionCount:  0,
			UpdatedAt:     now,
		}

		m := rs.ToModel()

		if m.ChannelID != channelID.String() {
			t.Errorf("ChannelID = %q, want %q", m.ChannelID, channelID.String())
		}
		if m.LastMessageID == nil {
			t.Fatal("LastMessageID = nil, want non-nil")
		}
		if *m.LastMessageID != messageID.String() {
			t.Errorf("LastMessageID = %q, want %q", *m.LastMessageID, messageID.String())
		}
		if m.MentionCount != 0 {
			t.Errorf("MentionCount = %d, want 0", m.MentionCount)
		}
	})

	t.Run("without last message", func(t *testing.T) {
		t.Parallel()
		rs := ReadState{
			UserID:    uuid.New(),
			ChannelID: channelID,
			UpdatedAt: now,
		}

		m := rs.ToModel()

		if m.ChannelID != channelID.String() {
			t.Errorf("ChannelID = %q, want %q", m.ChannelID, channelID.String())
		}
		if m.LastMessageID != nil {
			t.Errorf("LastMessageID = %v, want nil", m.LastMessageID)
		}
	})
}
