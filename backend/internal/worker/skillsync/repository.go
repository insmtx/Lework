package skillsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/pkg/messaging"
)

const managedIgnoreRules = `# Leros worker-managed paths
.system/
runs/
.skill-install-*/
.skill-backup-*/
**/__pycache__/
**/.cache/
*.tmp
.DS_Store
`

// Change is one top-level organization Skill changed from the committed baseline.
type Change struct {
	Code string
	Type messaging.SkillChangeType
}

// Repository manages the dedicated Git baseline for worker organization Skills.
type Repository struct {
	root string
}

// NewRepository creates a repository manager for one explicit Skill root.
func NewRepository(root string) (*Repository, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, fmt.Errorf("Skill repository root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve Skill repository root: %w", err)
	}
	return &Repository{root: absolute}, nil
}

// Root returns the managed Skill repository root.
func (r *Repository) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

// Ensure initializes the dedicated repository and commits existing content as a baseline.
func (r *Repository) Ensure(ctx context.Context) error {
	if r == nil || r.root == "" {
		return fmt.Errorf("Skill repository is not initialized")
	}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		return fmt.Errorf("create Skill repository: %w", err)
	}
	if _, err := os.Stat(filepath.Join(r.root, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := r.git(ctx, "init"); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("inspect Skill repository: %w", err)
	}
	ignoreChanged, err := r.ensureIgnoreRules()
	if err != nil {
		return err
	}
	if !r.hasHEAD(ctx) {
		if _, err := r.git(ctx, "add", "-A"); err != nil {
			return err
		}
		if _, err := r.gitWithIdentity(ctx, "commit", "-m", "chore(skill): 初始化 Worker Skill 基线"); err != nil {
			return err
		}
		return nil
	}
	if ignoreChanged {
		return r.commitPaths(ctx, []string{".gitignore"}, "chore(skill): 更新 Worker Skill 忽略规则")
	}
	return nil
}

// CommitInstalled commits server-installed Skills and the install manifest as baseline.
func (r *Repository) CommitInstalled(ctx context.Context, codes []string) error {
	if err := r.Ensure(ctx); err != nil {
		return err
	}
	paths := make([]string, 0, len(codes)+1)
	for _, code := range codes {
		code, err := validSkillCode(code)
		if err != nil {
			return err
		}
		paths = append(paths, code)
	}
	paths = append(paths, ".seed-manifest")
	sort.Strings(paths)
	return r.commitPaths(ctx, paths, "chore(skill): 更新 Server Skill 基线")
}

// Changes returns publishable changes and separately reports local deletions.
func (r *Repository) Changes(ctx context.Context) ([]Change, []string, error) {
	if err := r.Ensure(ctx); err != nil {
		return nil, nil, err
	}
	output, err := r.git(ctx, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--", ".")
	if err != nil {
		return nil, nil, err
	}
	changedCodes := make(map[string]struct{})
	entries := bytes.Split(output, []byte{0})
	for index := 0; index < len(entries); index++ {
		entry := entries[index]
		if len(entry) < 4 {
			continue
		}
		status := string(entry[:2])
		path := string(entry[3:])
		if status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C' {
			index++
		}
		code, ok := topLevelSkillCode(path)
		if ok {
			changedCodes[code] = struct{}{}
		}
	}
	codes := make([]string, 0, len(changedCodes))
	for code := range changedCodes {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	changes := make([]Change, 0, len(codes))
	deleted := make([]string, 0)
	for _, code := range codes {
		if _, err := os.Stat(filepath.Join(r.root, code, "SKILL.md")); errors.Is(err, os.ErrNotExist) {
			deleted = append(deleted, code)
			continue
		} else if err != nil {
			return nil, nil, fmt.Errorf("inspect Skill %q: %w", code, err)
		}
		changeType := messaging.SkillChangeUpdated
		if !r.trackedSkill(ctx, code) {
			changeType = messaging.SkillChangeCreated
		}
		changes = append(changes, Change{Code: code, Type: changeType})
	}
	return changes, deleted, nil
}

// Restore precisely returns one Skill to the committed baseline.
func (r *Repository) Restore(ctx context.Context, code string) error {
	code, err := validSkillCode(code)
	if err != nil {
		return err
	}
	if r.trackedSkill(ctx, code) {
		if _, err := r.git(ctx, "restore", "--source=HEAD", "--staged", "--worktree", "--", code); err != nil {
			return err
		}
	}
	if _, err := r.git(ctx, "clean", "-fdx", "--", code); err != nil {
		return err
	}
	return nil
}

func (r *Repository) commitPaths(ctx context.Context, paths []string, message string) error {
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := r.git(ctx, args...); err != nil {
		return err
	}
	diffArgs := append([]string{"diff", "--cached", "--quiet", "--"}, paths...)
	cmd := exec.CommandContext(ctx, "git", diffArgs...)
	cmd.Dir = r.root
	if err := cmd.Run(); err == nil {
		return nil
	} else if exitErr, ok := err.(*exec.ExitError); !ok || exitErr.ExitCode() != 1 {
		return fmt.Errorf("git %s: %w", strings.Join(diffArgs, " "), err)
	}
	commitArgs := append([]string{"commit", "--only", "-m", message, "--"}, paths...)
	_, err := r.gitWithIdentity(ctx, commitArgs...)
	return err
}

func (r *Repository) ensureIgnoreRules() (bool, error) {
	path := filepath.Join(r.root, ".gitignore")
	raw, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("read Skill .gitignore: %w", err)
	}
	if bytes.Contains(raw, []byte(managedIgnoreRules)) {
		return false, nil
	}
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		raw = append(raw, '\n')
	}
	raw = append(raw, []byte(managedIgnoreRules)...)
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		return false, fmt.Errorf("write Skill .gitignore: %w", err)
	}
	return true, nil
}

func (r *Repository) hasHEAD(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--verify", "HEAD")
	cmd.Dir = r.root
	return cmd.Run() == nil
}

func (r *Repository) trackedSkill(ctx context.Context, code string) bool {
	cmd := exec.CommandContext(ctx, "git", "cat-file", "-e", "HEAD:"+code+"/SKILL.md")
	cmd.Dir = r.root
	return cmd.Run() == nil
}

func (r *Repository) git(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (r *Repository) gitWithIdentity(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.root
	cmd.Env = identity.GitAuthorEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func topLevelSkillCode(path string) (string, bool) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return "", false
	}
	code := strings.SplitN(path, "/", 2)[0]
	if code == ".system" || code == "runs" || strings.HasPrefix(code, ".") {
		return "", false
	}
	code, err := validSkillCode(code)
	return code, err == nil
}

func validSkillCode(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." || filepath.Base(value) != value ||
		strings.ContainsAny(value, `/\`) || strings.HasPrefix(value, ".") ||
		value == "runs" || value == ".system" {
		return "", fmt.Errorf("invalid organization Skill code %q", value)
	}
	return value, nil
}
