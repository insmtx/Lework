package skilllinks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupLegacyGlobalSkillLinksOnceRemovesOnlyLegacyLinks(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".leros")
	homeDir := t.TempDir()
	legacySkillsDir := filepath.Join(stateDir, "skills")
	if err := os.MkdirAll(filepath.Join(legacySkillsDir, "legacy-skill"), 0o755); err != nil {
		t.Fatalf("create legacy Skill: %v", err)
	}

	claudeSkillsDir := filepath.Join(homeDir, ".claude", "skills")
	if err := os.MkdirAll(claudeSkillsDir, 0o755); err != nil {
		t.Fatalf("create Claude Skill directory: %v", err)
	}
	legacyLink := filepath.Join(claudeSkillsDir, "legacy-skill")
	if err := os.Symlink(filepath.Join(legacySkillsDir, "legacy-skill"), legacyLink); err != nil {
		t.Fatalf("create legacy link: %v", err)
	}
	if err := os.Mkdir(filepath.Join(claudeSkillsDir, "real-skill"), 0o755); err != nil {
		t.Fatalf("create real Skill directory: %v", err)
	}
	otherTarget := filepath.Join(t.TempDir(), "other-skill")
	if err := os.MkdirAll(otherTarget, 0o755); err != nil {
		t.Fatalf("create other Skill target: %v", err)
	}
	otherLink := filepath.Join(claudeSkillsDir, "other-skill")
	if err := os.Symlink(otherTarget, otherLink); err != nil {
		t.Fatalf("create other link: %v", err)
	}
	if err := os.Symlink(filepath.Join(legacySkillsDir, "legacy-skill"), filepath.Join(claudeSkillsDir, "different-name")); err != nil {
		t.Fatalf("create differently named link: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(legacySkillsDir, ".system", "system-skill"), 0o755); err != nil {
		t.Fatalf("create system Skill: %v", err)
	}
	if err := os.Symlink(filepath.Join(legacySkillsDir, ".system", "system-skill"), filepath.Join(claudeSkillsDir, "system-skill")); err != nil {
		t.Fatalf("create system link: %v", err)
	}

	report, err := cleanupLegacyGlobalSkillLinksOnce(stateDir, homeDir)
	if err != nil {
		t.Fatalf("cleanup legacy global Skill links: %v", err)
	}
	if report.Removed != 1 || report.AlreadyCompleted {
		t.Fatalf("cleanup report = %#v", report)
	}
	if _, err := os.Lstat(legacyLink); !os.IsNotExist(err) {
		t.Fatalf("legacy link still exists, err=%v", err)
	}
	for _, path := range []string{
		filepath.Join(claudeSkillsDir, "real-skill"),
		otherLink,
		filepath.Join(claudeSkillsDir, "different-name"),
		filepath.Join(claudeSkillsDir, "system-skill"),
	} {
		if _, err := os.Lstat(path); err != nil {
			t.Fatalf("non-legacy entry %s was removed: %v", path, err)
		}
	}
	if _, err := os.Stat(legacyGlobalSkillLinksCleanupMarkerPath(stateDir)); err != nil {
		t.Fatalf("cleanup marker: %v", err)
	}
}

func TestCleanupLegacyGlobalSkillLinksOnceUsesPersistentMarker(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), ".leros")
	homeDir := t.TempDir()
	legacySkillsDir := filepath.Join(stateDir, "skills", "later-skill")
	if err := os.MkdirAll(legacySkillsDir, 0o755); err != nil {
		t.Fatalf("create legacy Skill: %v", err)
	}

	if _, err := cleanupLegacyGlobalSkillLinksOnce(stateDir, homeDir); err != nil {
		t.Fatalf("first cleanup: %v", err)
	}
	claudeSkillsDir := filepath.Join(homeDir, ".claude", "skills")
	if err := os.MkdirAll(claudeSkillsDir, 0o755); err != nil {
		t.Fatalf("create Claude Skill directory: %v", err)
	}
	laterLink := filepath.Join(claudeSkillsDir, "later-skill")
	if err := os.Symlink(legacySkillsDir, laterLink); err != nil {
		t.Fatalf("create later legacy link: %v", err)
	}

	report, err := cleanupLegacyGlobalSkillLinksOnce(stateDir, homeDir)
	if err != nil {
		t.Fatalf("second cleanup: %v", err)
	}
	if !report.AlreadyCompleted || report.Removed != 0 {
		t.Fatalf("second cleanup report = %#v", report)
	}
	if _, err := os.Lstat(laterLink); err != nil {
		t.Fatalf("marker should prevent rescanning, err=%v", err)
	}
}
