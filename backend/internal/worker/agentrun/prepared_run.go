package agentrun

import (
	"github.com/insmtx/Leros/backend/agent"
	agentrundomain "github.com/insmtx/Leros/backend/internal/worker/agentrun/domain"
)

// PreparedRun holds the immutable original request and the fully-built execution context.
// Request is the caller's original snapshot — it is never modified during preparation.
type PreparedRun struct {
	Request   *agentrundomain.RunRequest
	Execution agent.ExecutionRequest
	Workspace WorkspacePreparation
	Baseline  ArtifactBaseline
}

// ArtifactBaseline captures artifact state before execution.
type ArtifactBaseline struct {
	Ref string
}
