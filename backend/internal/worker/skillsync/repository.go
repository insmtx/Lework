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
	"sync"

	"github.com/insmtx/Leros/backend/internal/worker/identity"
	"github.com/insmtx/Leros/backend/internal/worker/skillstate"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/ygpkg/yg-go/logs"
)

const managedIgnoreRules = `# Leros worker-managed paths
.system/
runs/
.skill-install-*/
.skill-backup-*/
.skill-task-backup-*/
.skill-task-txn-*/
**/__pycache__/
**/.cache/
*.tmp
.DS_Store
`

const taskSkillTransactionPrefix = ".skill-task-txn-"

// Change is one top-level organization Skill changed from the committed baseline.
type Change struct {
	Code string
	Type messaging.SkillChangeType
}

// Repository manages the dedicated Git baseline for worker organization Skills.
type Repository struct {
	root string
	lock *sync.Mutex
}

var repositoryLocks sync.Map // map[string]*sync.Mutex, keyed by absolute Skill root.
var fallbackRepositoryLock sync.Mutex

// SkillRepositoryLock returns the process-wide lock for one Skill root.
// Prepare and post-run synchronization share it so a reset cannot race with
// a project Skill installation.
func SkillRepositoryLock(root string) *sync.Mutex {
	root = strings.TrimSpace(root)
	if root == "" {
		return &fallbackRepositoryLock
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return &fallbackRepositoryLock
	}
	lockValue, _ := repositoryLocks.LoadOrStore(absolute, &sync.Mutex{})
	return lockValue.(*sync.Mutex)
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
	return &Repository{root: absolute, lock: SkillRepositoryLock(absolute)}, nil
}

// Root returns the managed Skill repository root.
func (r *Repository) Root() string {
	if r == nil {
		return ""
	}
	return r.root
}

func (r *Repository) repositoryLock() *sync.Mutex {
	if r != nil && r.lock != nil {
		return r.lock
	}
	return &fallbackRepositoryLock
}

// ImportTaskSkillDirs moves run-created directories into the persistent Skill
// repository. It deliberately does not validate Skill contents; validation and
// publication belong to Processor. Non-directory entries are left for the
// task-view cleanup so one unexpected entry cannot block post-run processing.
func (r *Repository) ImportTaskSkillDirs(ctx context.Context, sourceRoot string) {
	if r == nil || strings.TrimSpace(sourceRoot) == "" {
		return
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	r.importTaskSkillDirs(ctx, sourceRoot)
}

func (r *Repository) importTaskSkillDirs(ctx context.Context, sourceRoot string) {
	if err := r.recoverTaskSkillTransactions(ctx); err != nil {
		logs.WarnContextf(ctx, "recover interrupted task Skill moves failed: root=%s error=%v", r.root, err)
	}
	sourceRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		logs.WarnContextf(ctx, "resolve task Skill directory failed: root=%s error=%v", sourceRoot, err)
		return
	}
	if sameOrChildPath(sourceRoot, r.root) {
		logs.WarnContextf(ctx, "skip task Skill import from managed repository: source=%s root=%s", sourceRoot, r.root)
		return
	}
	sourceInfo, err := os.Lstat(sourceRoot)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		logs.WarnContextf(ctx, "inspect task Skill directory failed: root=%s error=%v", sourceRoot, err)
		return
	}
	if sourceInfo.Mode()&os.ModeSymlink != 0 || !sourceInfo.IsDir() {
		logs.WarnContextf(ctx, "skip non-directory task Skill root: root=%s", sourceRoot)
		return
	}
	entries, err := os.ReadDir(sourceRoot)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		logs.WarnContextf(ctx, "read task Skill directory failed: root=%s error=%v", sourceRoot, err)
		return
	}
	if err := os.MkdirAll(r.root, 0o755); err != nil {
		logs.WarnContextf(ctx, "create Worker Skill directory failed: root=%s error=%v", r.root, err)
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		source := filepath.Join(sourceRoot, name)
		info, statErr := os.Lstat(source)
		if statErr != nil {
			logs.WarnContextf(ctx, "inspect task Skill entry failed: path=%s error=%v", source, statErr)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			logs.WarnContextf(ctx, "skip non-directory task Skill entry: path=%s", source)
			continue
		}
		if _, err := validSkillCode(name); err != nil {
			logs.WarnContextf(ctx, "skip reserved or invalid task Skill directory: path=%s error=%v", source, err)
			continue
		}
		if err := r.replaceWithTaskSkill(ctx, source, filepath.Join(r.root, name)); err != nil {
			logs.WarnContextf(ctx, "move task Skill directory failed: source=%s target=%s error=%v", source, filepath.Join(r.root, name), err)
		}
	}
}

func (r *Repository) replaceWithTaskSkill(ctx context.Context, source, target string) error {
	name, err := validSkillCode(filepath.Base(target))
	if err != nil {
		return err
	}
	target = filepath.Join(r.root, name)
	targetInfo, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return os.Rename(source, target)
	}
	if err != nil {
		return err
	}
	if targetInfo == nil {
		return fmt.Errorf("inspect target Skill %q returned no file information", name)
	}

	txn, err := os.MkdirTemp(r.root, taskSkillTransactionPrefix)
	if err != nil {
		return err
	}
	keepTxn := true
	defer func() {
		if keepTxn {
			return
		}
		if removeErr := os.RemoveAll(txn); removeErr != nil {
			logs.WarnContextf(ctx, "remove completed task Skill transaction failed: path=%s error=%v", txn, removeErr)
		}
	}()
	if err := os.WriteFile(filepath.Join(txn, "target"), []byte(name+"\n"), 0o600); err != nil {
		return err
	}
	backup := filepath.Join(txn, "backup")
	if err := os.Rename(target, backup); err != nil {
		return err
	}
	if err := os.Rename(source, target); err != nil {
		if recoverErr := recoverTaskSkillTransaction(txn); recoverErr != nil {
			logs.WarnContextf(ctx, "restore replaced Worker Skill after move failure failed: target=%s error=%v", target, recoverErr)
		}
		return err
	}
	keepTxn = false
	return nil
}

// Ensure initializes the dedicated repository and commits existing content as a baseline.
func (r *Repository) Ensure(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("Skill repository is not initialized")
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	return r.ensure(ctx)
}

func (r *Repository) ensure(ctx context.Context) error {
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
	if err := r.recoverTaskSkillTransactions(ctx); err != nil {
		logs.WarnContextf(ctx, "recover interrupted task Skill moves failed: root=%s error=%v", r.root, err)
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

func (r *Repository) recoverTaskSkillTransactions(ctx context.Context) error {
	if r == nil || strings.TrimSpace(r.root) == "" {
		return fmt.Errorf("Skill repository is not initialized")
	}
	entries, err := os.ReadDir(r.root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var recoveredErr error
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), taskSkillTransactionPrefix) {
			continue
		}
		txn := filepath.Join(r.root, entry.Name())
		if !entry.IsDir() {
			logs.WarnContextf(ctx, "remove invalid task Skill transaction entry: path=%s", txn)
			if removeErr := os.Remove(txn); removeErr != nil {
				recoveredErr = errors.Join(recoveredErr, removeErr)
			}
			continue
		}
		if err := recoverTaskSkillTransaction(txn); err != nil {
			recoveredErr = errors.Join(recoveredErr, err)
			logs.WarnContextf(ctx, "recover task Skill transaction failed: path=%s error=%v", txn, err)
		}
	}
	return recoveredErr
}

func recoverTaskSkillTransaction(txn string) error {
	raw, err := os.ReadFile(filepath.Join(txn, "target"))
	if err != nil {
		return err
	}
	name, err := validSkillCode(strings.TrimSpace(string(raw)))
	if err != nil {
		return err
	}
	target := filepath.Join(filepath.Dir(txn), name)
	backup := filepath.Join(txn, "backup")
	_, targetErr := os.Lstat(target)
	_, backupErr := os.Lstat(backup)
	switch {
	case targetErr == nil:
		// The new Skill reached its target. The old copy is only a backup now.
		return os.RemoveAll(txn)
	case !errors.Is(targetErr, os.ErrNotExist):
		return targetErr
	case backupErr == nil:
		if err := os.Rename(backup, target); err != nil {
			return err
		}
		return os.RemoveAll(txn)
	case errors.Is(backupErr, os.ErrNotExist):
		return os.RemoveAll(txn)
	default:
		return backupErr
	}
}

func sameOrChildPath(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	rel, err := filepath.Rel(root, path)
	return err == nil && (rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))))
}

// CommitInstalled commits server-installed Skills and the install manifest as baseline.
func (r *Repository) CommitInstalled(ctx context.Context, codes []string) error {
	if r == nil {
		return fmt.Errorf("Skill repository is not initialized")
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	return r.commitInstalled(ctx, codes)
}

// CommitInstalledLocked commits a baseline while the caller holds
// SkillRepositoryLock(r.Root()). It exists for the prepare phase, which keeps
// the shared lock while replacing the installed Skill and its manifest.
func (r *Repository) CommitInstalledLocked(ctx context.Context, codes []string) error {
	if r == nil {
		return fmt.Errorf("Skill repository is not initialized")
	}
	return r.commitInstalled(ctx, codes)
}

func (r *Repository) commitInstalled(ctx context.Context, codes []string) error {
	if err := r.ensure(ctx); err != nil {
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
	if r == nil {
		return nil, nil, fmt.Errorf("Skill repository is not initialized")
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	return r.changes(ctx)
}

func (r *Repository) changes(ctx context.Context) ([]Change, []string, error) {
	if err := r.ensure(ctx); err != nil {
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
	if r == nil {
		return fmt.Errorf("Skill repository is not initialized")
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	return r.restore(ctx, code)
}

// RestoreLocked restores one Skill while the caller holds
// SkillRepositoryLock(r.Root()).
func (r *Repository) RestoreLocked(ctx context.Context, code string) error {
	if r == nil {
		return fmt.Errorf("Skill repository is not initialized")
	}
	return r.restore(ctx, code)
}

func (r *Repository) restore(ctx context.Context, code string) error {
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

// RestoreAll discards Run-owned changes under the managed Skill root while
// preserving the Worker-synchronized .system cache. The repository lock must
// cover the full post-run sync so another Run cannot recreate files between
// restore and clean.
func (r *Repository) RestoreAll(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("Skill repository is not initialized")
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	return r.restoreAll(ctx)
}

func (r *Repository) restoreAll(ctx context.Context) error {
	if r == nil || strings.TrimSpace(r.root) == "" {
		return fmt.Errorf("Skill repository is not initialized")
	}
	var restoreErr, cleanErr error
	if _, err := r.git(ctx, "restore", "--source=HEAD", "--staged", "--worktree", "--", "."); err != nil {
		restoreErr = err
		logs.WarnContextf(ctx, "restore Worker Skill Git baseline failed: root=%s error=%v", r.root, err)
	}
	if _, err := r.git(ctx, "clean", "-fdx", "-e", "/.system/", "--", "."); err != nil {
		cleanErr = err
		logs.WarnContextf(ctx, "clean Worker Skill Git repository failed: root=%s error=%v", r.root, err)
	}
	return errors.Join(restoreErr, cleanErr)
}

// CommittedInstallManifest reads sync policy only from the trusted Git baseline.
func (r *Repository) CommittedInstallManifest(ctx context.Context) (*skillstate.Manifest, error) {
	if r == nil {
		return nil, fmt.Errorf("Skill repository is not initialized")
	}
	r.repositoryLock().Lock()
	defer r.repositoryLock().Unlock()
	return r.committedInstallManifest(ctx)
}

func (r *Repository) committedInstallManifest(ctx context.Context) (*skillstate.Manifest, error) {
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}
	paths, err := r.git(ctx, "ls-tree", "--name-only", "HEAD", "--", ".seed-manifest")
	if err != nil {
		return nil, fmt.Errorf("inspect committed Skill manifest: %w", err)
	}
	if strings.TrimSpace(string(paths)) == "" {
		return skillstate.Parse(nil), nil
	}
	raw, err := r.git(ctx, "show", "HEAD:.seed-manifest")
	if err != nil {
		return nil, fmt.Errorf("read committed Skill manifest: %w", err)
	}
	return skillstate.Parse(raw), nil
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
