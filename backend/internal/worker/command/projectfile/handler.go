// Package projectfile handles project file commands on the worker that owns the workspace.
package projectfile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/internal/workspace"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

// ReplyPublisher publishes request/reply results through core NATS.
type ReplyPublisher interface {
	Publish(subject string, data []byte) error
}

// Handler executes project file commands in the worker workspace.
type Handler struct {
	pub        ReplyPublisher
	httpClient *http.Client
}

// New creates a project file command handler.
func New(pub ReplyPublisher) (*Handler, error) {
	if pub == nil {
		return nil, fmt.Errorf("reply publisher is required")
	}
	return &Handler{
		pub:        pub,
		httpClient: &http.Client{Timeout: 5 * time.Minute},
	}, nil
}

// HandleFileCommand restores a stored project file version and replies to the requester.
func (h *Handler) HandleFileCommand(ctx context.Context, cmd messaging.WorkerCommand) error {
	if cmd.Body.CommandType != messaging.CommandTypeProjectFileRestore {
		return h.reply(cmd.Body.ReplyTo, messaging.ProjectFileRestoreResult{
			Success: false,
			Error:   fmt.Sprintf("unsupported file command: %s", cmd.Body.CommandType),
		})
	}
	payload, err := messaging.DecodeCommandPayload[messaging.ProjectFileRestoreCommandPayload](&cmd.Body)
	if err != nil {
		return h.replyFailure(cmd.Body.ReplyTo, err)
	}
	result, err := h.restore(ctx, cmd.Route.OrgID, cmd.Route.WorkerID, payload)
	if err != nil {
		return h.replyFailure(cmd.Body.ReplyTo, err)
	}
	return h.reply(cmd.Body.ReplyTo, result)
}

func (h *Handler) restore(
	ctx context.Context,
	orgID uint,
	workerID uint,
	payload messaging.ProjectFileRestoreCommandPayload,
) (messaging.ProjectFileRestoreResult, error) {
	projectID := strings.TrimSpace(payload.ProjectPublicID)
	if projectID == "" {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("project_public_id is required")
	}
	relativePath, err := workspace.NormalizeRelativePath(payload.RelativePath)
	if err != nil {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("validate relative path: %w", err)
	}
	downloadURL := strings.TrimSpace(payload.DownloadURL)
	if downloadURL == "" {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("download_url is required")
	}

	repoDir, err := workspace.ProjectRepoPath(orgID, workerID, projectID)
	if err != nil {
		return messaging.ProjectFileRestoreResult{}, err
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".git")); err != nil {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("project repository is unavailable: %w", err)
	}
	absolutePath, err := workspace.SafeJoin(repoDir, relativePath)
	if err != nil {
		return messaging.ProjectFileRestoreResult{}, fmt.Errorf("resolve restore path: %w", err)
	}
	if err := downloadAtomically(ctx, h.httpClient, downloadURL, absolutePath); err != nil {
		return messaging.ProjectFileRestoreResult{}, err
	}
	commitSHA, err := commitRestoredFile(ctx, repoDir, relativePath, payload)
	if err != nil {
		return messaging.ProjectFileRestoreResult{}, err
	}
	return messaging.ProjectFileRestoreResult{
		Success:      true,
		RelativePath: relativePath,
		CommitSHA:    commitSHA,
	}, nil
}

func downloadAtomically(ctx context.Context, client *http.Client, downloadURL string, path string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return fmt.Errorf("create restored file download request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("download restored project file: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("download restored project file: unexpected status %s", response.Status)
	}

	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create project file directory: %w", err)
	}
	temporary, err := os.CreateTemp(parent, ".leros-restore-*")
	if err != nil {
		return fmt.Errorf("create restored project file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	if _, err := io.Copy(temporary, response.Body); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("write restored project file: %w", err)
	}
	if err := temporary.Chmod(0o644); err != nil {
		_ = temporary.Close()
		return fmt.Errorf("set restored project file mode: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close restored project file: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace project file with restored version: %w", err)
	}
	removeTemporary = false
	return nil
}

func commitRestoredFile(
	ctx context.Context,
	repoDir string,
	relativePath string,
	payload messaging.ProjectFileRestoreCommandPayload,
) (string, error) {
	if err := runGit(ctx, repoDir, nil, "add", "--", relativePath); err != nil {
		return "", fmt.Errorf("stage restored project file: %w", err)
	}

	diff := exec.CommandContext(ctx, "git", "diff", "--cached", "--quiet", "--", relativePath)
	diff.Dir = repoDir
	if err := diff.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
			return "", fmt.Errorf("check restored project file diff: %w", err)
		}
		env := append(os.Environ(),
			"GIT_AUTHOR_NAME="+defaultValue(payload.AuthorName, "leros-project-file-restore"),
			"GIT_AUTHOR_EMAIL="+defaultValue(payload.AuthorEmail, "project-file-restore@leros.local"),
			"GIT_COMMITTER_NAME="+defaultValue(payload.AuthorName, "leros-project-file-restore"),
			"GIT_COMMITTER_EMAIL="+defaultValue(payload.AuthorEmail, "project-file-restore@leros.local"),
		)
		if err := runGit(ctx, repoDir, env, "commit", "-m", "restore: "+relativePath, "--", relativePath); err != nil {
			return "", fmt.Errorf("commit restored project file: %w", err)
		}
	}

	branch := defaultValue(payload.Branch, "main")
	remote := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	remote.Dir = repoDir
	if remote.Run() == nil {
		if err := runGit(ctx, repoDir, nil, "push", "origin", "HEAD:"+branch); err != nil {
			return "", fmt.Errorf("push restored project file: %w", err)
		}
	}
	sha, err := gitOutput(ctx, repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("resolve restored project commit: %w", err)
	}
	return sha, nil
}

func runGit(ctx context.Context, repoDir string, env []string, args ...string) error {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoDir
	if env != nil {
		command.Env = env
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func gitOutput(ctx context.Context, repoDir string, args ...string) (string, error) {
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = repoDir
	output, err := command.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return strings.TrimSpace(string(output)), nil
}

func defaultValue(value string, fallback string) string {
	if value = strings.TrimSpace(value); value != "" {
		return value
	}
	return fallback
}

func (h *Handler) replyFailure(replyTo string, cause error) error {
	return h.reply(replyTo, messaging.ProjectFileRestoreResult{Success: false, Error: cause.Error()})
}

func (h *Handler) reply(replyTo string, result messaging.ProjectFileRestoreResult) error {
	if strings.TrimSpace(replyTo) == "" {
		return fmt.Errorf("reply_to is required")
	}
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal project file command result: %w", err)
	}
	if err := h.pub.Publish(replyTo, data); err != nil {
		return fmt.Errorf("publish project file command result: %w", err)
	}
	return nil
}
