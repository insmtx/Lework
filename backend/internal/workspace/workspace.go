// Package workspace 提供 Agent 任务运行所需的共享文件系统约定。
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	assistantdomain "github.com/insmtx/Leros/backend/internal/assistant/domain"
	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/pkg/leros"
	"github.com/ygpkg/yg-go/logs"
)

// TaskWorkspaceRequest 标识一次任务 turn 的工作区和运行目录请求。
type TaskWorkspaceRequest struct {
	OrgID            uint
	ProjectID        string
	TaskID           string
	RequestID        string
	RequestedWorkDir string
	CloneURL         string
}

// TaskWorkspace 描述一次任务 turn 会使用到的文件系统路径。
type TaskWorkspace struct {
	WorkspaceRoot        string
	ProjectRoot          string
	RepoDir              string
	TaskDir              string
	TurnDir              string
	TurnTmpDir           string
	TurnLogDir           string
	ArtifactManifestPath string
	BaselinePath         string
	EffectiveWorkDir     string
	CloneURL             string
}

// PrepareTaskWorkspace 创建并校验项目任务工作区。
func PrepareTaskWorkspace(ctx context.Context, req TaskWorkspaceRequest) (*TaskWorkspace, error) {
	plan, err := ResolveTaskWorkspace(req)
	if err != nil {
		return nil, err
	}
	if err := ensureGitRepo(ctx, plan); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(plan.TurnDir, 0o755); err != nil {
		return nil, fmt.Errorf("create turn dir: %w", err)
	}
	if err := os.MkdirAll(plan.TurnTmpDir, 0o755); err != nil {
		return nil, fmt.Errorf("create turn tmp dir: %w", err)
	}
	if err := os.MkdirAll(plan.TurnLogDir, 0o755); err != nil {
		return nil, fmt.Errorf("create turn log dir: %w", err)
	}
	if file, err := os.OpenFile(plan.ArtifactManifestPath, os.O_CREATE, 0o644); err != nil {
		return nil, fmt.Errorf("create artifact manifest: %w", err)
	} else {
		_ = file.Close()
	}
	if err := ensureGitignore(plan.RepoDir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(plan.EffectiveWorkDir, 0o755); err != nil {
		return nil, fmt.Errorf("create effective work dir: %w", err)
	}
	if err := ensureNoSymlinkEscape(plan.RepoDir, plan.EffectiveWorkDir); err != nil {
		return nil, fmt.Errorf("invalid runtime work_dir: %w", err)
	}
	return plan, nil
}

// PrepareTempWorkspace 创建没有项目上下文时使用的临时兜底工作区。
func PrepareTempWorkspace() (string, error) {
	dir, err := leros.TempDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create workspace temp dir: %w", err)
	}
	return dir, nil
}

// ResolveTaskWorkspace 只计算任务 turn 工作区路径，不创建任何目录。
func ResolveTaskWorkspace(req TaskWorkspaceRequest) (*TaskWorkspace, error) {
	if req.OrgID == 0 {
		return nil, fmt.Errorf("workspace org_id is required")
	}
	projectID := cleanPathID(req.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("workspace project_id is required")
	}
	taskID := cleanPathID(req.TaskID)
	if taskID == "" {
		return nil, fmt.Errorf("workspace task_id is required")
	}
	requestID := cleanPathID(req.RequestID)
	if requestID == "" {
		return nil, fmt.Errorf("workspace request_id is required")
	}

	root, err := leros.WorkspaceRoot()
	if err != nil {
		return nil, err
	}

	projectRoot := filepath.Join(root, "projects", fmt.Sprintf("%d", req.OrgID), projectID)
	repoDir := filepath.Join(projectRoot, "repo")
	taskDir := filepath.Join(repoDir, ".leros", "tasks", taskID)
	turnDir := filepath.Join(taskDir, "turns", requestID)
	effectiveWorkDir, err := resolveWorkDir(repoDir, req.RequestedWorkDir)
	if err != nil {
		return nil, err
	}
	return &TaskWorkspace{
		WorkspaceRoot:        root,
		ProjectRoot:          projectRoot,
		RepoDir:              repoDir,
		TaskDir:              taskDir,
		TurnDir:              turnDir,
		TurnTmpDir:           filepath.Join(turnDir, "tmp"),
		TurnLogDir:           filepath.Join(turnDir, "logs"),
		ArtifactManifestPath: filepath.Join(turnDir, "artifacts.jsonl"),
		BaselinePath:         filepath.Join(turnDir, "baseline.jsonl"),
		EffectiveWorkDir:     effectiveWorkDir,
		CloneURL:             req.CloneURL,
	}, nil
}

// FromAgentRequest 从标准化运行请求中的 workspace 上下文解析工作区路径。
func FromAgentRequest(req *assistantdomain.RunRequest) (*TaskWorkspace, bool, error) {
	if req == nil {
		return nil, false, nil
	}
	projectID := strings.TrimSpace(req.Workspace.ProjectID)
	taskID := strings.TrimSpace(req.Workspace.TaskID)
	if taskID == "" {
		taskID = strings.TrimSpace(req.TaskID)
	}
	requestID := strings.TrimSpace(req.Workspace.RequestID)
	if projectID == "" || taskID == "" || requestID == "" {
		return nil, false, nil
	}
	if req.Workspace.OrgID == 0 {
		return nil, false, nil
	}
	plan, err := ResolveTaskWorkspace(TaskWorkspaceRequest{
		OrgID:            req.Workspace.OrgID,
		ProjectID:        projectID,
		TaskID:           taskID,
		RequestID:        requestID,
		RequestedWorkDir: req.Runtime.WorkDir,
	})
	if err != nil {
		return nil, false, err
	}
	return plan, true, nil
}

// StorageKey returns a repo-relative path suitable for persistence and Gitea API access.
func (p *TaskWorkspace) StorageKey(relativePath string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("workspace plan is required")
	}
	absolute, err := SafeJoin(p.RepoDir, relativePath)
	if err != nil {
		return "", err
	}
	key, err := filepath.Rel(p.RepoDir, absolute)
	if err != nil {
		return "", fmt.Errorf("build storage key: %w", err)
	}
	return filepath.ToSlash(key), nil
}

// SafeJoin 解析相对路径，并确保最终路径仍在指定根目录内。
func SafeJoin(root string, child string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve root: %w", err)
	}
	child = strings.TrimSpace(child)
	if child == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(child) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(filepath.FromSlash(child))
	if clean == "." || clean == string(filepath.Separator) || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("path escapes output directory")
	}
	if isRuntimePath(clean) {
		return "", fmt.Errorf("runtime paths are not valid artifacts")
	}
	joined, err := filepath.Abs(filepath.Join(rootAbs, clean))
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	if err := ensureInside(rootAbs, joined); err != nil {
		return "", err
	}
	if err := ensureNoSymlinkEscape(rootAbs, joined); err != nil {
		return "", err
	}
	return joined, nil
}

func resolveWorkDir(repoDir string, requested string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return filepath.Abs(repoDir)
	}
	repoAbs, err := filepath.Abs(repoDir)
	if err != nil {
		return "", fmt.Errorf("resolve repo dir: %w", err)
	}
	var candidate string
	if filepath.IsAbs(requested) {
		candidate = requested
	} else {
		clean := filepath.Clean(filepath.FromSlash(requested))
		if clean == "." {
			candidate = repoAbs
		} else if strings.HasPrefix(clean, "..") {
			return "", fmt.Errorf("runtime work_dir escapes project workspace")
		} else if isRuntimePath(clean) {
			return "", fmt.Errorf("runtime work_dir cannot point to runtime paths")
		} else {
			candidate = filepath.Join(repoAbs, clean)
		}
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve runtime work_dir: %w", err)
	}
	if err := ensureInside(repoAbs, candidateAbs); err != nil {
		return "", fmt.Errorf("invalid runtime work_dir: %w", err)
	}
	return candidateAbs, nil
}

func isRuntimePath(path string) bool {
	path = filepath.ToSlash(filepath.Clean(path))
	if path == ".git" || strings.HasPrefix(path, ".git/") {
		return true
	}
	if path == ".leros" || strings.HasPrefix(path, ".leros/") {
		// .leros/memory/ 目录不在运行时保护范围内，允许访问项目记忆文件
		if path == ".leros/memory" || strings.HasPrefix(path, ".leros/memory/") {
			return false
		}
		return true
	}
	return false
}

func ensureInside(root string, candidate string) error {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return fmt.Errorf("compare paths: %w", err)
	}
	if rel == "." {
		return nil
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return fmt.Errorf("path escapes project workspace")
	}
	return nil
}

func ensureNoSymlinkEscape(root string, candidate string) error {
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return fmt.Errorf("resolve root symlinks: %w", err)
	}
	candidateReal, err := resolveSymlinksUpToExisting(candidate)
	if err != nil {
		return fmt.Errorf("resolve path symlinks: %w", err)
	}
	rootReal, err = filepath.Abs(rootReal)
	if err != nil {
		return fmt.Errorf("resolve real root: %w", err)
	}
	candidateReal, err = filepath.Abs(candidateReal)
	if err != nil {
		return fmt.Errorf("resolve real path: %w", err)
	}
	return ensureInside(rootReal, candidateReal)
}

// resolveSymlinksUpToExisting 解析路径中的符号链接。
// 若路径本身不存在，则向上找到最近的存在父目录解析符号链接，
// 再将剩余不存在的路径段原样追加。用于上传等写入场景的路径校验。
func resolveSymlinksUpToExisting(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// 路径不存在，向上查找存在的父目录
	parent := filepath.Dir(path)
	if parent == path {
		// 已达到根目录，直接返回
		return path, nil
	}
	parentResolved, err := resolveSymlinksUpToExisting(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(parentResolved, filepath.Base(path)), nil
}

func ensureGitRepo(ctx context.Context, plan *TaskWorkspace) error {
	if err := os.MkdirAll(plan.RepoDir, 0o755); err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}
	entries, readErr := os.ReadDir(plan.RepoDir)
	if readErr == nil && len(entries) > 0 {
		gitDir := filepath.Join(plan.RepoDir, ".git")
		info, statErr := os.Stat(gitDir)
		if statErr != nil || !info.IsDir() {
			if err := os.RemoveAll(plan.RepoDir); err != nil {
				return fmt.Errorf("remove non-git repo dir: %w", err)
			}
		}
	}
	gitDir := filepath.Join(plan.RepoDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		if hasRemote(ctx, plan.RepoDir, "origin") {
			cmd := exec.CommandContext(ctx, "git", "pull", "origin", "main")
			cmd.Dir = plan.RepoDir
			if output, err := cmd.CombinedOutput(); err != nil {
				return fmt.Errorf("git pull: %w: %s", err, strings.TrimSpace(string(output)))
			}
			if err := os.MkdirAll(filepath.Join(plan.RepoDir, "artifacts"), 0o755); err != nil {
				return fmt.Errorf("create artifacts dir: %w", err)
			}
			return nil
		}
		if err := os.RemoveAll(plan.RepoDir); err != nil {
			return fmt.Errorf("remove broken repo: %w", err)
		}
	}

	if strings.TrimSpace(plan.CloneURL) == "" {
		cmd := exec.CommandContext(ctx, "git", "init", plan.RepoDir)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git init: %w: %s", err, strings.TrimSpace(string(output)))
		}
		if err := os.MkdirAll(filepath.Join(plan.RepoDir, "artifacts"), 0o755); err != nil {
			return fmt.Errorf("create artifacts dir: %w", err)
		}
		if err := os.MkdirAll(filepath.Join(plan.RepoDir, "assets"), 0o755); err != nil {
			return fmt.Errorf("create assets dir: %w", err)
		}
		return nil
	}

	parent := filepath.Dir(plan.RepoDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}
	cmd := exec.CommandContext(ctx, "git", "clone", plan.CloneURL, plan.RepoDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(plan.RepoDir)
		return fmt.Errorf("git clone: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := os.MkdirAll(filepath.Join(plan.RepoDir, "assets"), 0o755); err != nil {
		return fmt.Errorf("create assets dir: %w", err)
	}
	return nil
}

func hasRemote(ctx context.Context, repoDir string, remote string) bool {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", remote)
	cmd.Dir = repoDir
	return cmd.Run() == nil
}

// defaultGitignore 定义项目仓库初始化时创建的默认 .gitignore 内容。
const defaultGitignore = `# Leros runtime
.leros/tasks/

# Dependency directories
node_modules/
vendor/
dist/
build/
target/
.cache/
tmp/
temp/
logs/

# OS/editor noise
.DS_Store
Thumbs.db
*.swp
*.swo

# Runtime logs
*.log

# Environment/secrets
.env
.env.*
!.env.example
`

func ensureGitignore(repoDir string) error {
	gitignorePath := filepath.Join(repoDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); err == nil {
		// 文件已存在，不覆盖用户自定义的 .gitignore 规则
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat gitignore: %w", err)
	}
	if err := os.WriteFile(gitignorePath, []byte(defaultGitignore), 0o644); err != nil {
		return fmt.Errorf("write default gitignore: %w", err)
	}
	return nil
}

func cleanPathID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `/\`)
	if value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return ""
	}
	return value
}

func PushWorkspace(ctx context.Context, plan *TaskWorkspace) error {
	if plan == nil || plan.RepoDir == "" {
		logs.ErrorContextf(ctx, "PushWorkspace skipped: plan is nil or repo dir is empty")
		return nil
	}
	gitDir := filepath.Join(plan.RepoDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		logs.ErrorContextf(ctx, "PushWorkspace skipped: .git directory not found: %s", gitDir)
		return nil
	}

	addCmd := exec.CommandContext(ctx, "git", "add", ".")
	addCmd.Dir = plan.RepoDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w: %s", err, strings.TrimSpace(string(output)))
	}

	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", "task: agent run artifacts")
	commitCmd.Dir = plan.RepoDir
	commitCmd.Env = identity.GitAuthorEnv()
	if output, err := commitCmd.CombinedOutput(); err != nil {
		logs.ErrorContextf(ctx, "git commit artifacts: %v: %s", err, strings.TrimSpace(string(output)))
		return nil
	}

	pushCmd := exec.CommandContext(ctx, "git", "push", "origin", "main")
	pushCmd.Dir = plan.RepoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push: %w: %s", err, strings.TrimSpace(string(output)))
	}
	logs.InfoContextf(ctx, "PushWorkspace completed: repo_dir=%s", plan.RepoDir)
	return nil
}
