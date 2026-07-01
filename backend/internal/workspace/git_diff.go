package workspace

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/logs"
)

const (
	// envGitIndexFile is the environment variable used for a temporary Git index.
	envGitIndexFile = "GIT_INDEX_FILE"
)

// CapturePreRunTree writes the current working tree to a Git tree object using a
// temporary GIT_INDEX_FILE, and returns the tree SHA. This captures the state of
// the repo before the agent begins execution so that generated files can be
// identified later.
//
// It handles:
//   - Repos with HEAD: git read-tree HEAD into the temp index first.
//   - Repos without HEAD: git read-tree --empty into the temp index first.
//   - Then git add --all + git write-tree, all using the temp index.
//
// The real .git/index is never modified. The caller is responsible for
// handling the returned empty string (e.g. when .git does not exist).
func CapturePreRunTree(ctx context.Context, repoDir string) (string, error) {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", nil
	}
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return "", nil
	}

	tmpIndex, err := os.CreateTemp("", "leros-git-index-*.tmp")
	if err != nil {
		return "", fmt.Errorf("create temp git index: %w", err)
	}
	tmpIndexPath := tmpIndex.Name()
	tmpIndex.Close()
	defer os.Remove(tmpIndexPath)

	env := append(os.Environ(), envGitIndexFile+"="+tmpIndexPath)

	// Determine whether the repo has at least one commit.
	hasHead := func() bool {
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = repoDir
		return cmd.Run() == nil
	}()

	// Populate the temp index from the current HEAD (or an empty tree).
	if hasHead {
		readTree := exec.CommandContext(ctx, "git", "read-tree", "HEAD")
		readTree.Dir = repoDir
		readTree.Env = env
		if output, err := readTree.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git read-tree HEAD: %w: %s", err, strings.TrimSpace(string(output)))
		}
	} else {
		readTree := exec.CommandContext(ctx, "git", "read-tree", "--empty")
		readTree.Dir = repoDir
		readTree.Env = env
		if output, err := readTree.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git read-tree --empty: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}

	// Add all current files (staged, unstaged, untracked) using the temp index.
	addCmd := exec.CommandContext(ctx, "git", "add", "--all")
	addCmd.Dir = repoDir
	addCmd.Env = env
	if output, err := addCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add --all for pre-run tree: %w: %s", err, strings.TrimSpace(string(output)))
	}

	// Write the tree object.
	writeTreeCmd := exec.CommandContext(ctx, "git", "write-tree")
	writeTreeCmd.Dir = repoDir
	writeTreeCmd.Env = env
	output, err := writeTreeCmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git write-tree for pre-run snapshot: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

// CapturePostRunTree captures the current working tree as a Git tree object,
// using the same temporary-index approach as CapturePreRunTree.
func CapturePostRunTree(ctx context.Context, repoDir string) (string, error) {
	return CapturePreRunTree(ctx, repoDir)
}

// DiffArtifacts computes the files changed between two Git tree SHAs using git diff-tree.
// It returns a list of manifest artifact entries for new, modified, copied, renamed, and
// type-changed files. Deleted files, directories, and .gitignored files are excluded.
//
// Returns nil if either treeSHA is empty or trees are identical.
func DiffArtifacts(ctx context.Context, repoDir, preTreeSHA, postTreeSHA string) ([]ManifestArtifact, error) {
	preTreeSHA = strings.TrimSpace(preTreeSHA)
	postTreeSHA = strings.TrimSpace(postTreeSHA)
	if preTreeSHA == "" || postTreeSHA == "" {
		return nil, nil
	}
	if preTreeSHA == postTreeSHA {
		return nil, nil
	}

	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// git diff-tree -r --diff-filter=ACMRT lists Added, Copied, Modified, Renamed, Type-changed files.
	// -r recurses into trees. -z uses NUL terminators for safe filename parsing.
	cmd := exec.CommandContext(ctx, "git", "diff-tree", "-r", "--diff-filter=ACMRT", "--name-status", "-z", preTreeSHA, postTreeSHA)
	cmd.Dir = repoDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git diff-tree %s..%s: %w: %s", preTreeSHA, postTreeSHA, err, strings.TrimSpace(string(output)))
	}

	// Parse NUL-delimited output: pairs of (status, filename).
	parts := strings.Split(string(output), "\x00")
	if len(parts) < 2 {
		return nil, nil
	}

	var entries []ManifestArtifact
	for i := 0; i+1 < len(parts); i += 2 {
		status := strings.TrimSpace(parts[i])
		filePath := parts[i+1]

		if status == "" || filePath == "" {
			continue
		}

		entries = append(entries, ManifestArtifact{
			Path:    filePath,
			IsFinal: true,
			Source:  string(types.ArtifactSourceDiff),
		})
	}

	return entries, nil
}

// CapturePreRunTreeSafe is a best-effort wrapper that logs a warning on failure
// instead of returning an error. Callers that cannot abort on Git failures should
// use this.
func CapturePreRunTreeSafe(ctx context.Context, repoDir string) string {
	treeSHA, err := CapturePreRunTree(ctx, repoDir)
	if err != nil {
		logs.WarnContextf(ctx, "capture pre-run tree failed (continuing): %v", err)
		return ""
	}
	return treeSHA
}

// GitDiffReconcile runs when the manifest has no explicit final entries.
// It computes the diff between the pre-run and post-run Git trees and appends
// the resulting entries to the artifact manifest.
// This replaces the old baseline.jsonl-based ReconcileArtifacts.
//
// Failures during post-run capture or diff-tree are logged as warnings and do
// not prevent explicit declarations, finalization, or commit/push from proceeding.
func GitDiffReconcile(ctx context.Context, plan *TaskWorkspace, preTreeSHA string) error {
	if plan == nil || plan.RepoDir == "" || plan.ArtifactManifestPath == "" {
		return nil
	}
	if strings.TrimSpace(preTreeSHA) == "" {
		return nil
	}

	// First check if manifest already has final entries from explicit artifact_declare calls.
	hasFinal, err := manifestHasFinalEntries(plan.ArtifactManifestPath)
	if err != nil {
		return err
	}
	if hasFinal {
		// Explicit declarations take priority; skip Git diff fallback.
		return nil
	}

	// Capture post-run tree.
	postTreeSHA, err := CapturePostRunTree(ctx, plan.RepoDir)
	if err != nil {
		logs.WarnContextf(ctx, "git diff reconcile: capture post-run tree failed, skipping diff fallback: %v", err)
		return nil
	}
	if postTreeSHA == "" {
		return nil
	}

	entries, err := DiffArtifacts(ctx, plan.RepoDir, preTreeSHA, postTreeSHA)
	if err != nil {
		logs.WarnContextf(ctx, "git diff reconcile: diff-tree failed, skipping diff fallback: %v", err)
		return nil
	}
	if len(entries) == 0 {
		return nil
	}

	// Append diff entries to manifest.
	file, err := os.OpenFile(plan.ArtifactManifestPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open manifest for git diff fallback: %w", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal diff manifest entry: %w", err)
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("append diff manifest entry: %w", err)
		}
	}
	return writer.Flush()
}
