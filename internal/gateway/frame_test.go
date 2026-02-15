package gateway

import (
	"encoding/json"
	"testing"

	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
)

func TestNewHelloFrame(t *testing.T) {
	t.Parallel()

	raw, err := NewHelloFrame(45000)
	if err != nil {
		t.Fatalf("NewHelloFrame() error = %v", err)
	}

	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if f.Op != events.OpcodeHello {
		t.Errorf("Op = %d, want %d", f.Op, events.OpcodeHello)
	}
	if f.Seq != nil {
		t.Errorf("Seq = %v, want nil", f.Seq)
	}
	if f.Type != nil {
		t.Errorf("Type = %v, want nil", f.Type)
	}

	var data models.HelloData
	if err := json.Unmarshal(f.Data, &data); err != nil {
		t.Fatalf("unmarshal hello data: %v", err)
	}
	if data.HeartbeatInterval != 45000 {
		t.Errorf("HeartbeatInterval = %d, want 45000", data.HeartbeatInterval)
	}
}

func TestNewHeartbeatACKFrame(t *testing.T) {
	t.Parallel()

	raw, err := NewHeartbeatACKFrame()
	if err != nil {
		t.Fatalf("NewHeartbeatACKFrame() error = %v", err)
	}

	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if f.Op != events.OpcodeHeartbeatACK {
		t.Errorf("Op = %d, want %d", f.Op, events.OpcodeHeartbeatACK)
	}
	if f.Seq != nil {
		t.Errorf("Seq = %v, want nil", f.Seq)
	}
}

func TestNewDispatchFrame(t *testing.T) {
	t.Parallel()

	payload := json.RawMessage(`{"channel_id":"abc","content":"hello"}`)
	raw, err := NewDispatchFrame(42, events.MessageCreate, payload)
	if err != nil {
		t.Fatalf("NewDispatchFrame() error = %v", err)
	}

	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if f.Op != events.OpcodeDispatch {
		t.Errorf("Op = %d, want %d", f.Op, events.OpcodeDispatch)
	}
	if f.Seq == nil || *f.Seq != 42 {
		t.Errorf("Seq = %v, want 42", f.Seq)
	}
	if f.Type == nil || *f.Type != events.MessageCreate {
		t.Errorf("Type = %v, want %q", f.Type, events.MessageCreate)
	}

	var data struct {
		ChannelID string `json:"channel_id"`
		Content   string `json:"content"`
	}
	if err := json.Unmarshal(f.Data, &data); err != nil {
		t.Fatalf("unmarshal dispatch data: %v", err)
	}
	if data.ChannelID != "abc" {
		t.Errorf("ChannelID = %q, want %q", data.ChannelID, "abc")
	}
}

func TestNewReconnectFrame(t *testing.T) {
	t.Parallel()

	raw, err := NewReconnectFrame()
	if err != nil {
		t.Fatalf("NewReconnectFrame() error = %v", err)
	}

	var f Frame
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	if f.Op != events.OpcodeReconnect {
		t.Errorf("Op = %d, want %d", f.Op, events.OpcodeReconnect)
	}
}

func TestNewInvalidSessionFrame(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resumable bool
	}{
		{"resumable", true},
		{"not resumable", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			raw, err := NewInvalidSessionFrame(tt.resumable)
			if err != nil {
				t.Fatalf("NewInvalidSessionFrame(%v) error = %v", tt.resumable, err)
			}

			var f Frame
			if err := json.Unmarshal(raw, &f); err != nil {
				t.Fatalf("unmarshal frame: %v", err)
			}
			if f.Op != events.OpcodeInvalidSession {
				t.Errorf("Op = %d, want %d", f.Op, events.OpcodeInvalidSession)
			}

			var got bool
			if err := json.Unmarshal(f.Data, &got); err != nil {
				t.Fatalf("unmarshal data: %v", err)
			}
			if got != tt.resumable {
				t.Errorf("data = %v, want %v", got, tt.resumable)
			}
		})
	}
}
