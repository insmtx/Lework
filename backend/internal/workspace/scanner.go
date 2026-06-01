package workspace

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// FileSnapshot records a single file's metadata at a point in time.
type FileSnapshot struct {
	Path         string `json:"path"`
	Size         int64  `json:"size"`
	MtimeUnixNano int64 `json:"mtime_unix_nano"`
}

// WriteBaseline scans the repo directory, applies ignore rules, and writes a baseline.jsonl.
func WriteBaseline(ctx context.Context, plan *TaskWorkspace) error {
	if plan == nil || plan.RepoDir == "" || plan.BaselinePath == "" {
		return nil
	}
	snapshots, err := scanRepoFiles(ctx, plan.RepoDir)
	if err != nil {
		return fmt.Errorf("scan repo for baseline: %w", err)
	}
	file, err := os.Create(plan.BaselinePath)
	if err != nil {
		return fmt.Errorf("create baseline file: %w", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, snap := range snapshots {
		line, err := json.Marshal(snap)
		if err != nil {
			return fmt.Errorf("marshal baseline line: %w", err)
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write baseline line: %w", err)
		}
	}
	return writer.Flush()
}

// ReconcileArtifacts compares the current repo state against the baseline, identifies new or
// modified files, and populates the manifest at plan.ArtifactManifestPath with any autodetected
// artifacts. It skips reconciliation when the manifest already contains final entries.
func ReconcileArtifacts(ctx context.Context, plan *TaskWorkspace) error {
	if plan == nil || plan.RepoDir == "" || plan.BaselinePath == "" || plan.ArtifactManifestPath == "" {
		return nil
	}
	hasFinal, err := manifestHasFinalEntries(plan.ArtifactManifestPath)
	if err != nil {
		return err
	}
	if hasFinal {
		return nil
	}
	baseline := make(map[string]FileSnapshot)
	if err := readBaseline(plan.BaselinePath, baseline); err != nil {
		return fmt.Errorf("read baseline for reconciliation: %w", err)
	}
	current, err := scanRepoFiles(ctx, plan.RepoDir)
	if err != nil {
		return fmt.Errorf("scan repo for reconciliation: %w", err)
	}
	candidates := diffFiles(baseline, current)
	if len(candidates) == 0 {
		return nil
	}

	file, err := os.OpenFile(plan.ArtifactManifestPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open manifest for reconciliation: %w", err)
	}
	defer file.Close()
	writer := bufio.NewWriter(file)
	for _, snap := range candidates {
		rel, err := filepath.Rel(plan.RepoDir, snap.Path)
		if err != nil {
			return fmt.Errorf("resolve relative path for %q: %w", snap.Path, err)
		}
		entry := ManifestArtifact{
			Path:    filepath.ToSlash(rel),
			IsFinal: true,
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal manifest entry: %w", err)
		}
		if _, err := writer.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("append manifest entry: %w", err)
		}
	}
	return writer.Flush()
}

// manifestHasFinalEntries checks whether the manifest already contains at least one final entry.
func manifestHasFinalEntries(manifestPath string) (bool, error) {
	file, err := os.Open(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("open manifest: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var entry ManifestArtifact
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.IsFinal {
			return true, nil
		}
	}
	return false, scanner.Err()
}

func readBaseline(path string, target map[string]FileSnapshot) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open baseline: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var snap FileSnapshot
		if err := json.Unmarshal([]byte(line), &snap); err != nil {
			continue
		}
		target[snap.Path] = snap
	}
	return scanner.Err()
}

// diffFiles returns files that are either new (not in baseline) or modified (size or mtime changed).
func diffFiles(baseline map[string]FileSnapshot, current []FileSnapshot) []FileSnapshot {
	candidates := make([]FileSnapshot, 0)
	for _, snap := range current {
		prev, exists := baseline[snap.Path]
		if !exists {
			candidates = append(candidates, snap)
			continue
		}
		if snap.Size != prev.Size || snap.MtimeUnixNano != prev.MtimeUnixNano {
			candidates = append(candidates, snap)
		}
	}
	return candidates
}

// scanRepoFiles walks repoDir and returns file snapshots for every file not excluded by ignore rules.
func scanRepoFiles(ctx context.Context, repoDir string) ([]FileSnapshot, error) {
	checker, err := newIgnoreChecker(repoDir)
	if err != nil {
		return nil, err
	}
	snapshots := make([]FileSnapshot, 0)
	err = filepath.WalkDir(repoDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if d.IsDir() {
			if checker.shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		ignored, err := checker.isIgnored(path)
		if err != nil || ignored {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		snapshots = append(snapshots, FileSnapshot{
			Path:           path,
			Size:           info.Size(),
			MtimeUnixNano: info.ModTime().UnixNano(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return snapshots, nil
}

// ignoreChecker applies gitignore rules and Leros built-in excludes.
type ignoreChecker struct {
	repoDir   string
	gitDir    string
	lerosDir  string
	cache     map[string]bool
	useGit    bool
}

func newIgnoreChecker(repoDir string) (*ignoreChecker, error) {
	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}
	gitDir := filepath.Join(absRepo, ".git")
	lerosDir := filepath.Join(absRepo, ".leros")
	_, gitErr := os.Stat(gitDir)
	useGit := gitErr == nil
	return &ignoreChecker{
		repoDir:  absRepo,
		gitDir:   gitDir,
		lerosDir: lerosDir,
		cache:    make(map[string]bool),
		useGit:   useGit,
	}, nil
}

// shouldSkipDir returns true when a directory should be excluded from scanning entirely.
func (c *ignoreChecker) shouldSkipDir(absPath string) bool {
	if absPath == c.gitDir || absPath == c.lerosDir {
		return true
	}
	if c.isBuiltinExcludedDir(absPath) {
		return true
	}
	return false
}

func (c *ignoreChecker) isBuiltinExcludedDir(absPath string) bool {
	name := filepath.Base(absPath)
	switch strings.ToLower(name) {
	case "tmp", "temp", "logs", "log", ".cache", "node_modules", "vendor", "dist", "build", "target":
		return true
	}
	return false
}

func (c *ignoreChecker) isIgnored(absPath string) (bool, error) {
	if cached, ok := c.cache[absPath]; ok {
		return cached, nil
	}
	// Built-in filename exclusions
	if c.isBuiltinExcludedFile(absPath) {
		c.cache[absPath] = true
		return true, nil
	}
	// Git check-ignore via stdin
	if c.useGit {
		ignored, err := c.gitCheckIgnore(absPath)
		if err == nil {
			c.cache[absPath] = ignored
			return ignored, nil
		}
	}
	c.cache[absPath] = false
	return false, nil
}

func (c *ignoreChecker) isBuiltinExcludedFile(absPath string) bool {
	base := filepath.Base(absPath)
	if base == ".DS_Store" || base == "Thumbs.db" || base == ".gitignore" {
		return true
	}
	ext := filepath.Ext(base)
	if ext == ".swp" || ext == ".swo" || ext == ".log" {
		return true
	}
	return false
}

func (c *ignoreChecker) gitCheckIgnore(absPath string) (bool, error) {
	rel, err := filepath.Rel(c.repoDir, absPath)
	if err != nil {
		return false, err
	}
	rel = filepath.ToSlash(rel)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "check-ignore", "--stdin", "--no-index")
	cmd.Dir = c.repoDir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false, err
	}
	go func() {
		defer stdin.Close()
		fmt.Fprintln(stdin, rel)
	}()
	output, err := cmd.CombinedOutput()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Exit code 1: path is NOT ignored
			// Exit code 128: git error
			if exitErr.ExitCode() == 1 {
				return false, nil
			}
		}
		return false, err
	}
	return len(bytes.TrimSpace(output)) > 0, nil
}
