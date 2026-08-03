package skilllinks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/insmtx/Leros/backend/pkg/leros"
)

const legacyGlobalSkillLinksCleanupVersion = "remove-global-cli-skill-links-v1"

var legacyGlobalCLISkillRelativeDirs = []string{
	filepath.Join(".claude", "skills"),
	filepath.Join(".agents", "skills"),
	filepath.Join(".config", "opencode", "skills"),
}

// LegacyGlobalSkillLinksCleanupReport records the one-time removal of links
// created by the retired global Skill projection.
type LegacyGlobalSkillLinksCleanupReport struct {
	AlreadyCompleted bool
	Removed          int
}

type legacyGlobalSkillLinksCleanupMarker struct {
	Version     string    `json:"version"`
	Removed     int       `json:"removed"`
	CompletedAt time.Time `json:"completed_at"`
}

// CleanupLegacyGlobalSkillLinksOnce removes only legacy global CLI Skill links.
// It never removes real files, real directories, or links to any other location.
func CleanupLegacyGlobalSkillLinksOnce() (*LegacyGlobalSkillLinksCleanupReport, error) {
	stateDir, err := leros.EnsureStateDir()
	if err != nil {
		return nil, err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home directory: %w", err)
	}
	return cleanupLegacyGlobalSkillLinksOnce(stateDir, homeDir)
}

func cleanupLegacyGlobalSkillLinksOnce(
	stateDir string,
	homeDir string,
) (*LegacyGlobalSkillLinksCleanupReport, error) {
	markerPath := legacyGlobalSkillLinksCleanupMarkerPath(stateDir)
	if _, err := os.Stat(markerPath); err == nil {
		return &LegacyGlobalSkillLinksCleanupReport{AlreadyCompleted: true}, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read legacy Skill link cleanup marker: %w", err)
	}

	legacySkillsDir := filepath.Join(stateDir, "skills")
	removed := 0
	for _, relativeDir := range legacyGlobalCLISkillRelativeDirs {
		count, err := removeLegacyLinksInCLISkillDir(
			filepath.Join(homeDir, relativeDir),
			legacySkillsDir,
		)
		if err != nil {
			return nil, err
		}
		removed += count
	}

	marker := legacyGlobalSkillLinksCleanupMarker{
		Version:     legacyGlobalSkillLinksCleanupVersion,
		Removed:     removed,
		CompletedAt: time.Now().UTC(),
	}
	if err := writeLegacyGlobalSkillLinksCleanupMarker(markerPath, marker); err != nil {
		return nil, err
	}
	return &LegacyGlobalSkillLinksCleanupReport{Removed: removed}, nil
}

func removeLegacyLinksInCLISkillDir(cliSkillDir string, legacySkillsDir string) (int, error) {
	info, err := os.Lstat(cliSkillDir)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("inspect CLI Skill directory %s: %w", cliSkillDir, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, nil
	}

	entries, err := os.ReadDir(cliSkillDir)
	if err != nil {
		return 0, fmt.Errorf("read CLI Skill directory %s: %w", cliSkillDir, err)
	}
	removed := 0
	for _, entry := range entries {
		entryPath := filepath.Join(cliSkillDir, entry.Name())
		entryInfo, err := os.Lstat(entryPath)
		if err != nil {
			return 0, fmt.Errorf("inspect CLI Skill entry %s: %w", entryPath, err)
		}
		if entryInfo.Mode()&os.ModeSymlink == 0 {
			continue
		}
		legacyTarget, err := isLegacyGlobalSkillLink(entryPath, entry.Name(), legacySkillsDir)
		if err != nil {
			return 0, err
		}
		if !legacyTarget {
			continue
		}
		if err := os.Remove(entryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("remove legacy CLI Skill link %s: %w", entryPath, err)
		}
		removed++
	}
	return removed, nil
}

func isLegacyGlobalSkillLink(linkPath string, skillName string, legacySkillsDir string) (bool, error) {
	target, err := os.Readlink(linkPath)
	if err != nil {
		return false, fmt.Errorf("read CLI Skill link %s: %w", linkPath, err)
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkPath), target)
	}
	target, err = filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false, fmt.Errorf("resolve CLI Skill link %s: %w", linkPath, err)
	}
	legacySkillsDir, err = filepath.Abs(filepath.Clean(legacySkillsDir))
	if err != nil {
		return false, fmt.Errorf("resolve legacy Skills directory: %w", err)
	}
	return target == filepath.Join(legacySkillsDir, skillName), nil
}

func legacyGlobalSkillLinksCleanupMarkerPath(stateDir string) string {
	return filepath.Join(stateDir, "migrations", legacyGlobalSkillLinksCleanupVersion+".json")
}

func writeLegacyGlobalSkillLinksCleanupMarker(
	markerPath string,
	marker legacyGlobalSkillLinksCleanupMarker,
) error {
	if err := os.MkdirAll(filepath.Dir(markerPath), 0o700); err != nil {
		return fmt.Errorf("create legacy Skill link cleanup marker directory: %w", err)
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("encode legacy Skill link cleanup marker: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(markerPath), ".legacy-skill-links-*")
	if err != nil {
		return fmt.Errorf("create legacy Skill link cleanup marker: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write legacy Skill link cleanup marker: %w", err)
	}
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return fmt.Errorf("set legacy Skill link cleanup marker permissions: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close legacy Skill link cleanup marker: %w", err)
	}
	if err := os.Rename(temporaryPath, markerPath); err != nil {
		return fmt.Errorf("publish legacy Skill link cleanup marker: %w", err)
	}
	return nil
}
