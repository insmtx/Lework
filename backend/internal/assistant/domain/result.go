package domain

import (
	"encoding/json"
	"time"

	"github.com/insmtx/Leros/backend/agent"
)

// RunStatus is the final SingerOS business status.
type RunStatus string

const (
	RunStatusCompleted RunStatus = "completed"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCancelled RunStatus = "cancelled"
)

// RunResult is the finalized business result of one assistant run.
type RunResult struct {
	RunID       string                 `json:"run_id"`
	TraceID     string                 `json:"trace_id,omitempty"`
	Status      RunStatus              `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Usage       *agent.Usage           `json:"usage,omitempty"`
	ToolCalls   []agent.ToolCallRecord `json:"tool_calls,omitempty"`
	Artifacts   []ArtifactRecord       `json:"artifacts,omitempty"`
	Metadata    *RunMetadata           `json:"metadata,omitempty"`
	StartedAt   time.Time              `json:"started_at"`
	CompletedAt time.Time              `json:"completed_at,omitempty"`
}

// RunMetadata contains typed business metadata persisted with a run.
type RunMetadata struct {
	Runtime    string `json:"runtime,omitempty"`
	WorkDir    string `json:"work_dir,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	Phase      string `json:"phase,omitempty"`
	Resume     bool   `json:"resume,omitempty"`
}

// ArtifactRecord is the artifact summary archived at run completion.
type ArtifactRecord struct {
	ArtifactID   string `json:"artifact_id,omitempty"`
	Title        string `json:"title,omitempty"`
	Filename     string `json:"filename,omitempty"`
	OriginalName string `json:"original_name,omitempty"`
	Description  string `json:"description,omitempty"`
	MimeType     string `json:"mime_type,omitempty"`
	ArtifactType string `json:"artifact_type,omitempty"`
	FileSize     int64  `json:"file_size,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
	StorageKey   string `json:"storage_key,omitempty"`
	StorageURI   string `json:"storage_uri,omitempty"`
	Sha256       string `json:"sha256,omitempty"`
	Source       string `json:"source,omitempty"`
	Status       string `json:"status,omitempty"`
}

// TerminalPayload is the business terminal payload encoded into run terminal events.
type TerminalPayload struct {
	Status      string                 `json:"status"`
	Message     string                 `json:"message,omitempty"`
	Error       string                 `json:"error,omitempty"`
	Usage       *agent.Usage           `json:"usage,omitempty"`
	ToolCalls   []agent.ToolCallRecord `json:"tool_calls,omitempty"`
	Artifacts   []ArtifactRecord       `json:"artifacts,omitempty"`
	StartedAt   string                 `json:"started_at,omitempty"`
	CompletedAt string                 `json:"completed_at,omitempty"`
	Metadata    *RunMetadata           `json:"metadata,omitempty"`
	Events      []TerminalEventRecord  `json:"events,omitempty"`
}

// TerminalEventRecord is an archived non-terminal event reference.
type TerminalEventRecord struct {
	Seq       int64           `json:"seq"`
	LastSeq   int64           `json:"last_seq,omitempty"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}
