package steps

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/ygpkg/yg-go/logs"
)

type PushWorkspaceStep struct{}

func (PushWorkspaceStep) Name() string {
	return "push_workspace"
}

func (s PushWorkspaceStep) Run(ctx context.Context, state *State) error {
	if state == nil || state.Request == nil {
		return nil
	}
	if state.Err != nil {
		logs.ErrorContextf(ctx, "push_workspace skipped: previous error: %v", state.Err)
		return nil
	}
	repoDir := strings.TrimSpace(state.Request.Workspace.RepoDir)
	if repoDir == "" {
		logs.ErrorContextf(ctx, "push_workspace skipped: repo dir is empty")
		return nil
	}
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		logs.ErrorContextf(ctx, "push_workspace skipped: .git directory not found: %s", gitDir)
		return nil
	}

	addCmd := exec.CommandContext(ctx, "git", "add", ".")
	addCmd.Dir = repoDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(output)))
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "task: agent run artifacts")
	commitCmd.Dir = repoDir
	commitCmd.Env = identity.GitAuthorEnv()
	if output, err := commitCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git commit artifacts: %v: %s", err, strings.TrimSpace(string(output)))
		return nil
	}

	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git push failed: %v: %s", err, strings.TrimSpace(string(output)))
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(output)))
	}
	logs.InfoContextf(ctx, "push_workspace completed: repo_dir=%s", repoDir)
	return nil
}
