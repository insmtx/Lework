package assistant

import (
	"github.com/insmtx/Leros/backend/agent"
	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
)

// PreparedRun binds the immutable business snapshot to its pure execution request.
type PreparedRun struct {
	Request   *assistantdomain.RunRequest
	Execution agent.ExecutionRequest
	Workspace WorkspacePreparation
	Baseline  ArtifactBaseline
}

// ArtifactBaseline captures artifact state before execution.
type ArtifactBaseline struct {
	Ref string
}
