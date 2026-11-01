// Package opencode is a thin HTTP/SSE client for a locally running
// `opencode serve` process (OpenCode V2 API).
//
// The agent runs one opencode process per task and reaches it over loopback,
// so this package wraps only the endpoints the agent daemon needs. Endpoint
// paths and payload shapes were probed against opencode v2.0.32 and are
// recorded in docs/design/2026-11-01-i0-findings.md.
package opencode

import (
	"encoding/json"
	"time"
)

// Session is a conversation bound to a working directory.
//
// The wire shape nests the directory under "location" and timestamps under
// "time" as Unix milliseconds, so UnmarshalJSON flattens both into the fields
// below. Only the fields the agent needs are projected.
type Session struct {
	ID string `json:"id"`
	// Directory is the absolute working directory the session was created in.
	Directory string    `json:"directory,omitempty"`
	Title     string    `json:"title,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Message is one turn in a session. Role is the wire "type" discriminator:
// "user", "assistant", "idle", or one of the metadata turn types. Text is the
// user prompt for user turns and the concatenated text parts for assistant
// turns, which is the authoritative reply text after a run.
type Message struct {
	ID      string        `json:"id"`
	Role    string        `json:"role"`
	Text    string        `json:"text,omitempty"`
	Parts   []ContentPart `json:"parts,omitempty"`
	Finish  string        `json:"finish,omitempty"`
	Outcome string        `json:"outcome,omitempty"`
	Error   *MessageError `json:"error,omitempty"`
}

// MessageError is the structured failure attached to a failed assistant or
// idle turn. Message is provider-supplied and may be shown to operators, but
// callers must not treat it as a secret-free string.
type MessageError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
}

// ContentPart is a single piece of assistant message content. Only text parts
// are projected for I0; other types (reasoning, tool) are kept with an empty
// Text so callers can still count them.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Event is one frame from the OpenCode event feed. Type is the event name and
// Data keeps the payload undecoded so the caller decides which fields matter.
// SessionID is lifted out of data.sessionID because v2 does not put it at the
// top level; session-less events leave it empty.
type Event struct {
	ID        string          `json:"id,omitempty"`
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
}

// sessionWire mirrors the v2 Session.Info envelope fields this package reads.
type sessionWire struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Location struct {
		Directory string `json:"directory"`
	} `json:"location"`
	Time struct {
		Updated float64 `json:"updated"`
	} `json:"time"`
}

// UnmarshalJSON accepts the nested v2 shape and flattens it for callers.
func (s *Session) UnmarshalJSON(data []byte) error {
	var wire sessionWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	s.ID = wire.ID
	s.Directory = wire.Location.Directory
	s.Title = wire.Title
	if wire.Time.Updated > 0 {
		s.UpdatedAt = time.UnixMilli(int64(wire.Time.Updated)).UTC()
	}
	return nil
}

// messageWire mirrors the union message shape: user turns carry "text",
// assistant turns carry "content", and idle turns carry "outcome".
type messageWire struct {
	ID      string        `json:"id"`
	Type    string        `json:"type"`
	Text    string        `json:"text"`
	Content []ContentPart `json:"content"`
	Finish  string        `json:"finish"`
	Outcome string        `json:"outcome"`
	Error   *MessageError `json:"error"`
}

// UnmarshalJSON projects the union into the flat Message shape.
func (m *Message) UnmarshalJSON(data []byte) error {
	var wire messageWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	m.ID = wire.ID
	m.Role = wire.Type
	m.Text = wire.Text
	m.Parts = wire.Content
	m.Finish = wire.Finish
	m.Outcome = wire.Outcome
	m.Error = wire.Error
	for _, part := range wire.Content {
		if part.Type == "text" {
			m.Text += part.Text
		}
	}
	return nil
}

// eventWire mirrors the v2 event envelope. The event ID is used for
// correlation; it is not a replay cursor (see the I0 findings).
type eventWire struct {
	ID   string          `json:"id"`
	Type string          `json:"type"`
	Data json.RawMessage `json:"data"`
}

// UnmarshalJSON lifts data.sessionID to the top level.
func (e *Event) UnmarshalJSON(data []byte) error {
	var wire eventWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	e.ID = wire.ID
	e.Type = wire.Type
	e.Data = wire.Data
	if len(wire.Data) > 0 {
		var meta struct {
			SessionID string `json:"sessionID"`
		}
		if err := json.Unmarshal(wire.Data, &meta); err == nil {
			e.SessionID = meta.SessionID
		}
	}
	return nil
}
