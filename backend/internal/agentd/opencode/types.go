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
	Directory string `json:"directory,omitempty"`
	// ProjectID groups sessions that belong to the same repository or
	// directory. OpenCode hashes the git root commit for repositories and the
	// canonical path for plain directories, so the same repo cloned on two
	// machines reports one id; uncommitted repositories all report "global".
	ProjectID string `json:"projectID,omitempty"`
	// Subpath is the path of Directory relative to the project root. It is
	// empty when the session was created at the root itself.
	Subpath   string    `json:"subpath,omitempty"`
	Title     string    `json:"title,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

// Project is one OpenCode project: a git repository root or a plain working
// directory. Callers treat ID as the grouping key, except for the "global"
// sentinel that OpenCode uses for repositories without commits.
type Project struct {
	ID        string      `json:"id"`
	Canonical string      `json:"canonical"`
	VCS       string      `json:"vcs,omitempty"`
	Time      ProjectTime `json:"time,omitempty"`
}

// ProjectTime holds the millisecond timestamps OpenCode records per project.
type ProjectTime struct {
	Created int64 `json:"created,omitempty"`
	Updated int64 `json:"updated,omitempty"`
	Active  int64 `json:"active,omitempty"`
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

// PermissionRequest is a pending approval from OpenCode. It mirrors the
// permission.asked payload so the browser can rebuild a card from a snapshot
// after an event gap.
type PermissionRequest struct {
	ID        string   `json:"id"`
	SessionID string   `json:"sessionID"`
	Action    string   `json:"action"`
	Resources []string `json:"resources,omitempty"`
	Message   string   `json:"message,omitempty"`
}

// MessagePage is one page of session messages in chronological order. Next
// and Previous are opaque OpenCode cursors; Next points at an older page and
// is empty when the beginning of the session has been reached.
type MessagePage struct {
	Messages []Message `json:"messages"`
	Previous string    `json:"previous,omitempty"`
	Next     string    `json:"next,omitempty"`
}

// MessageError is the structured failure attached to a failed assistant or
// idle turn. Message is provider-supplied and may be shown to operators, but
// callers must not treat it as a secret-free string.
type MessageError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
	Status  int    `json:"status,omitempty"`
}

// ContentPart is a single piece of assistant message content. Text parts carry
// Text; tool parts keep the tool name, call ID, and raw state (status, input,
// output, error) so the browser can render a tool card without the agent
// interpreting provider-specific shapes.
type ContentPart struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Tool   string          `json:"tool,omitempty"`
	CallID string          `json:"callID,omitempty"`
	Name   string          `json:"name,omitempty"`
	ID     string          `json:"id,omitempty"`
	State  json.RawMessage `json:"state,omitempty"`
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

// FileDiff is one changed file in a session turn diff. Patch is a unified diff;
// Status is "added", "deleted", or "modified".
type FileDiff struct {
	File      string `json:"file"`
	Patch     string `json:"patch"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Status    string `json:"status"`
}

// FileInfo is one direct child returned by the OpenCode file list. Directories
// end with a slash; the Type is "file" or "directory".
type FileInfo struct {
	Path string `json:"path"`
	Type string `json:"type"`
}

// FileContent is raw bytes read from a workspace file. Size is the total file
// size when known; Truncated reports that maxBytes cut the body short.
type FileContent struct {
	Data        []byte
	ContentType string
	Size        int64
	Truncated   bool
}

// InterruptResult reports whether an interrupt stopped an active run. An idle
// session returns false rather than an error.
type InterruptResult struct {
	Interrupted bool `json:"interrupted"`
}

// sessionWire mirrors the v2 Session.Info envelope fields this package reads.
type sessionWire struct {
	ID        string `json:"id"`
	ProjectID string `json:"projectID"`
	Subpath   string `json:"subpath"`
	Title     string `json:"title"`
	Location  struct {
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
	s.ProjectID = wire.ProjectID
	s.Subpath = wire.Subpath
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
	for i := range wire.Content {
		part := &wire.Content[i]
		if part.Type == "text" {
			m.Text += part.Text
		}
		// OpenCode v2 names tool parts with `name` and `id`; normalize to the
		// stable Tool/CallID fields so consumers do not depend on provider shape.
		if part.Tool == "" {
			part.Tool = part.Name
		}
		if part.CallID == "" {
			part.CallID = part.ID
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
