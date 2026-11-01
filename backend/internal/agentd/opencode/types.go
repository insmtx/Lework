// Package opencode is a thin HTTP/SSE client for a locally running
// `opencode serve` process (OpenCode V2 API).
//
// The agent runs one opencode process per task and reaches it over loopback,
// so this package wraps only the endpoints the agent daemon needs. Endpoint
// paths and payload shapes follow
// docs/design/2026-10-27-opencode-v2-runtime-migration-design.md as the I0
// starting contract; they are expected to be corrected against a real probe
// rather than papered over with compatibility shims.
package opencode

import (
	"encoding/json"
	"time"
)

// Session is a conversation bound to a working directory.
type Session struct {
	ID string `json:"id"`
	// Directory is the absolute working directory the session was created in.
	Directory string    `json:"directory"`
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Message is one turn in a session. Parts carry the ordered content of the
// turn, which is where the authoritative assistant text lives after a run.
type Message struct {
	ID    string        `json:"id"`
	Role  string        `json:"role"`
	Parts []ContentPart `json:"parts"`
}

// ContentPart is a single piece of message content. Only text parts are
// projected for I0; other types are kept with an empty Text so callers can
// still count them.
type ContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Event is one frame from the OpenCode event feed. Type is the event name and
// Data keeps the payload undecoded so the caller decides which fields matter.
type Event struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionID"`
	Data      json.RawMessage `json:"data"`
}
