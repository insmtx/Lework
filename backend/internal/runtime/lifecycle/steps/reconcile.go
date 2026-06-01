package steps

import (
	"context"
	"os"

	agentworkspace "github.com/insmtx/Leros/backend/internal/workspace"
)

// ArtifactReconcileStep compares the current repo state against the baseline and
// populates the artifact manifest with any auto-detected file changes. It skips
// reconciliation when the manifest already contains final entries (explicitly declared
// artifacts take priority).
type ArtifactReconcileStep struct{}

func (ArtifactReconcileStep) Name() string {
	return "artifact_reconcile"
}

func (ArtifactReconcileStep) Run(ctx context.Context, state *State) error {
	if state == nil || state.Request == nil || state.Err != nil {
		return nil
	}
	plan, ok, err := agentworkspace.FromAgentRequest(state.Request)
	if err != nil || !ok {
		return err
	}
	if _, err := os.Stat(plan.RepoDir); os.IsNotExist(err) {
		return nil
	}
	return agentworkspace.ReconcileArtifacts(ctx, plan)
}
