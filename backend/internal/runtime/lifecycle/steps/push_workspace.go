package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ygpkg/yg-go/logs"
)

type PushWorkspaceStep struct{}

func (PushWorkspaceStep) Name() string {
	return "push_workspace"
}

func (s PushWorkspaceStep) Run(ctx context.Context, state *State) error {
	if state == nil || state.Err != nil || state.Request == nil {
		return nil
	}
	repoDir := strings.TrimSpace(state.Request.Workspace.RepoDir)
	if repoDir == "" {
		return nil
	}
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return nil
	}

	addCmd := exec.CommandContext(ctx, "git", "add", ".")
	addCmd.Dir = repoDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(output)))
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "task: agent run artifacts")
	commitCmd.Dir = repoDir
	commitCmd.CombinedOutput()

	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git push failed: %v: %s", err, strings.TrimSpace(string(output)))
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
