package steps

import (
	"context"
	"os"

	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
)

// ArtifactBaselineStep captures a file snapshot of the repo directory before agent execution.
// The baseline is written to the turn directory so that a later ArtifactReconcileStep can
// detect files created or modified during the run.
type ArtifactBaselineStep struct{}

func (ArtifactBaselineStep) Name() string {
	return "artifact_baseline"
}

func (ArtifactBaselineStep) Run(ctx context.Context, state *State) error {
	if state == nil || state.Request == nil {
		return nil
	}
	plan, ok, err := agentworkspace.FromAgentRequest(state.Request)
	if err != nil || !ok {
		return err
	}
	if _, err := os.Stat(plan.RepoDir); os.IsNotExist(err) {
		return nil
	}
	return agentworkspace.WriteBaseline(ctx, plan)
}
