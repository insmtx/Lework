// Package protocol defines the wire protocol shared by the Lework control
// plane and the local lework-agent daemon over a WebSocket connection.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// MaxFrameBytes is the largest single frame either side accepts.
//
// The limit is memory protection, not a payload budget: it must still fit a
// legitimate session.messages page. A 200-message page is already ~1.3 MiB, so
// the old 1 MiB ceiling killed the connection and surfaced as a bogus
// "node_offline". 8 MiB matches the NATS max_payload used by the deployment
// configs, keeping the WebSocket and cross-instance forwarding ceilings aligned.
const MaxFrameBytes = 8 << 20

// FrameType identifies the kind of message carried by a Frame. It is a string
// so that unknown types can still be decoded and rejected by Validate instead
// of failing during JSON unmarshalling.
type FrameType string

// Frame types exchanged between the agent and the control plane.
const (
	// FrameHello is sent by the agent to register itself after connecting.
	FrameHello FrameType = "hello"
	// FrameWelcome is the control plane's acknowledgement of FrameHello.
	FrameWelcome FrameType = "welcome"
	// FrameHeartbeat keeps an otherwise idle connection alive.
	FrameHeartbeat FrameType = "heartbeat"
	// FrameRequest carries a control-plane method call to the agent.
	FrameRequest FrameType = "request"
	// FrameResponse carries the result or error of a FrameRequest.
	FrameResponse FrameType = "response"
	// FrameEvent carries an asynchronous agent-side notification.
	FrameEvent FrameType = "event"
	// FrameCancel asks the agent to abandon an in-flight request.
	FrameCancel FrameType = "cancel"
	// FramePairPending tells an unpaired agent which claim id and short code
	// identify this pending connection.
	FramePairPending FrameType = "pair.pending"
	// FramePairResult delivers the node id and one-time credential after the
	// claim has been bound to an account.
	FramePairResult FrameType = "pair.result"
)

// Frame is the single envelope exchanged over the control-plane WebSocket.
// Every message, control or data, is a Frame so that the transport only needs
// one read/write path.
type Frame struct {
	Type   FrameType       `json:"type"`
	ID     string          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *FrameError     `json:"error,omitempty"`
	Event  *Event          `json:"event,omitempty"`
}

// FrameError is a protocol-level failure returned in a FrameResponse. Code is
// stable and machine-readable; Message is for humans and must not contain
// secrets.
type FrameError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Event is an asynchronous notification about a node or session. Seq is
// monotonic per session so that receivers can detect gaps and resync.
type Event struct {
	NodeID    string          `json:"node_id"`
	SessionID string          `json:"session_id,omitempty"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data,omitempty"`
	Time      time.Time       `json:"time"`
}

// Validate checks that the frame carries the fields its type requires.
// It is used on both sides of the connection so malformed frames fail fast
// and visibly instead of being partially processed.
func (f Frame) Validate() error {
	switch f.Type {
	case FrameHello, FrameWelcome, FrameHeartbeat:
		return nil
	case FrameRequest:
		if f.ID == "" {
			return fmt.Errorf("request frame requires id")
		}
		if f.Method == "" {
			return fmt.Errorf("request frame requires method")
		}
		return nil
	case FrameResponse:
		if f.ID == "" {
			return fmt.Errorf("response frame requires id")
		}
		if len(f.Result) == 0 && f.Error == nil {
			return fmt.Errorf("response frame requires result or error")
		}
		return nil
	case FrameEvent:
		if f.Event == nil {
			return fmt.Errorf("event frame requires event")
		}
		if f.Event.Type == "" {
			return fmt.Errorf("event frame requires event type")
		}
		return nil
	case FrameCancel:
		if f.ID == "" {
			return fmt.Errorf("cancel frame requires id")
		}
		return nil
	case FramePairPending, FramePairResult:
		// 配对帧只要求载荷：pair.pending 带 claim_id/code，pair.result 带
		// node_id/credential；ID 用来对应 hello，不做强制。
		if len(f.Result) == 0 {
			return fmt.Errorf("%s frame requires result", f.Type)
		}
		return nil
	default:
		return fmt.Errorf("unknown frame type: %s", f.Type)
	}
}
