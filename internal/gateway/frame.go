package gateway

import (
	"encoding/json"
	"fmt"

	"github.com/uncord-chat/uncord-protocol/events"
	"github.com/uncord-chat/uncord-protocol/models"
)

// Frame is the wire-format structure for all WebSocket messages. Dispatch events (op 0) carry a sequence number and
// event type; control frames use only op and optionally d.
type Frame struct {
	Op   events.Opcode         `json:"op"`
	Seq  *int64                `json:"s,omitempty"`
	Type *events.DispatchEvent `json:"t,omitempty"`
	Data json.RawMessage       `json:"d,omitempty"`
}

// NewHelloFrame returns a serialised Hello frame with the given heartbeat interval in milliseconds.
func NewHelloFrame(heartbeatIntervalMS int) ([]byte, error) {
	data, err := json.Marshal(models.HelloData{HeartbeatInterval: heartbeatIntervalMS})
	if err != nil {
		return nil, fmt.Errorf("marshal hello data: %w", err)
	}
	return json.Marshal(Frame{
		Op:   events.OpcodeHello,
		Data: data,
	})
}

// NewHeartbeatACKFrame returns a serialised HeartbeatACK frame.
func NewHeartbeatACKFrame() ([]byte, error) {
	return json.Marshal(Frame{Op: events.OpcodeHeartbeatACK})
}

// NewDispatchFrame returns a serialised Dispatch frame with the given sequence number, event type, and raw data
// payload. The sequence number and event type are included in the frame envelope.
func NewDispatchFrame(seq int64, eventType events.DispatchEvent, data json.RawMessage) ([]byte, error) {
	return json.Marshal(Frame{
		Op:   events.OpcodeDispatch,
		Seq:  &seq,
		Type: &eventType,
		Data: data,
	})
}

// NewReconnectFrame returns a serialised Reconnect frame instructing the client to reconnect.
func NewReconnectFrame() ([]byte, error) {
	return json.Marshal(Frame{Op: events.OpcodeReconnect})
}

// NewInvalidSessionFrame returns a serialised InvalidSession frame. The resumable flag indicates whether the client
// should attempt to resume or must re-identify.
func NewInvalidSessionFrame(resumable bool) ([]byte, error) {
	data, err := json.Marshal(resumable)
	if err != nil {
		return nil, fmt.Errorf("marshal invalid session data: %w", err)
	}
	return json.Marshal(Frame{
		Op:   events.OpcodeInvalidSession,
		Data: data,
	})
}
