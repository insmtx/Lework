// Package workspace 提供 Agent 任务运行所需的共享文件系统约定。
package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/insmtx/Leros/backend/internal/agent"
	"github.com/insmtx/Leros/backend/pkg/leros"
)

// TaskWorkspaceRequest 标识一次任务 turn 的工作区和运行目录请求。
type TaskWorkspaceRequest struct {
	OrgID            uint
	ProjectID        string
	TaskID           string
	RequestID        string
	RequestedWorkDir string
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
}

// PrepareTaskWorkspace 创建并校验项目任务工作区。
func PrepareTaskWorkspace(ctx context.Context, req TaskWorkspaceRequest) (*TaskWorkspace, error) {
	plan, err := ResolveTaskWorkspace(req)
	if err != nil {
		return nil, err
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
	if err := ensureGitRepo(ctx, plan.RepoDir); err != nil {
		return nil, err
	}
	if err := ensureGitignore(plan.RepoDir); err != nil {
		return nil, err
	}
	if err := ensureLerosExcluded(plan.RepoDir); err != nil {
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
	}, nil
}

// FromAgentRequest 从标准化运行请求中的 workspace 上下文解析工作区路径。
func FromAgentRequest(req *agent.RequestContext) (*TaskWorkspace, bool, error) {
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

// StorageKey 返回适合持久化的 workspace root 相对路径。
func (p *TaskWorkspace) StorageKey(relativePath string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("workspace plan is required")
	}
	absolute, err := SafeJoin(p.RepoDir, relativePath)
	if err != nil {
		return "", err
	}
	key, err := filepath.Rel(p.WorkspaceRoot, absolute)
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
	return path == ".git" || strings.HasPrefix(path, ".git/") ||
		path == ".leros" || strings.HasPrefix(path, ".leros/")
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
	candidateReal, err := filepath.EvalSymlinks(candidate)
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

func ensureGitRepo(ctx context.Context, repoDir string) error {
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		return fmt.Errorf("create repo dir: %w", err)
	}
	gitDir := filepath.Join(repoDir, ".git")
	if info, err := os.Stat(gitDir); err == nil && info.IsDir() {
		return nil
	}
	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git init workspace: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// defaultGitignore 定义项目仓库初始化时创建的默认 .gitignore 内容。
const defaultGitignore = `# Leros runtime
.leros/

# Dependency directories
node_modules/
vendor/

# Build/cache outputs
dist/
build/
target/
.cache/
.cache*/
tmp/
temp/
logs/
log/

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

func ensureLerosExcluded(repoDir string) error {
	infoDir := filepath.Join(repoDir, ".git", "info")
	if err := os.MkdirAll(infoDir, 0o755); err != nil {
		return fmt.Errorf("create git info dir: %w", err)
	}
	excludePath := filepath.Join(infoDir, "exclude")
	current, err := os.ReadFile(excludePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read git exclude: %w", err)
	}
	if strings.Contains(string(current), ".leros/") {
		return nil
	}
	next := string(current)
	if next != "" && !strings.HasSuffix(next, "\n") {
		next += "\n"
	}
	next += ".leros/\n"
	return os.WriteFile(excludePath, []byte(next), 0o644)
}

func cleanPathID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `/\`)
	if value == "." || value == ".." || strings.Contains(value, "/") || strings.Contains(value, `\`) {
		return ""
	}
	return value
}
