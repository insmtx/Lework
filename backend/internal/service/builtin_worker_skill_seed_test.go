package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func TestSyncWorkerSkillsCreatesSystemPlugin(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "worker-demo", "", "Worker body.")

	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if report.Scanned != 1 || report.Created != 1 || len(report.Failures) != 0 {
		t.Fatalf("first report = %#v", report)
	}

	// Verify plugin was created as system plugin with builtin_worker origin.
	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "worker-demo")
	if err != nil || plugin == nil {
		t.Fatalf("system plugin = %#v, %v", plugin, err)
	}
	if plugin.OwnerScope != types.OwnerScopeSystem || plugin.OrgID != 0 ||
		plugin.Origin != "builtin_worker" || plugin.CurrentRevision != 1 ||
		plugin.Status != types.PluginStatusActive {
		t.Fatalf("system plugin ownership = %#v", plugin)
	}

	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("revision = %#v, %v", revision, err)
	}
	if revision.PublishedByType != "system" || revision.PublishedByID != 0 {
		t.Fatalf("system revision audit = %#v", revision)
	}

	content, err := infradb.GetPluginRevisionContent(context.Background(), database, revision.ID)
	if err != nil || content == nil || len(content.FileIndex) != 2 {
		t.Fatalf("revision content = %#v, %v", content, err)
	}

	// Verify no marketplace item was created.
	assertBuiltinSkillRecordCounts(t, database, 1, 1, 1, 1)
	var marketplaceCount int64
	if err := database.Model(&types.PluginMarketplaceItem{}).Count(&marketplaceCount).Error; err != nil {
		t.Fatalf("count marketplace items: %v", err)
	}
	if marketplaceCount != 0 {
		t.Fatalf("marketplace items = %d, want 0", marketplaceCount)
	}
}

func TestSyncWorkerSkillsIdempotent(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "idempotent-skill", "", "Idempotent content.")

	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Created != 1 {
		t.Fatalf("first sync report = %#v, %v", report, err)
	}

	report, err = SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Unchanged != 1 {
		t.Fatalf("idempotent report = %#v, %v", report, err)
	}

	assertBuiltinSkillRecordCounts(t, database, 1, 1, 1, 1)
}

func TestSyncWorkerSkillsContentChange(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "changing-skill", "", "Version 1.")

	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Created != 1 {
		t.Fatalf("first sync report = %#v, %v", report, err)
	}

	writeBuiltinSkillTestFiles(t, sourceDir, "changing-skill", "", "Version 2.")
	report, err = SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Updated != 1 {
		t.Fatalf("update report = %#v, %v", report, err)
	}

	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "changing-skill")
	if err != nil || plugin == nil {
		t.Fatalf("plugin = %#v, %v", plugin, err)
	}
	if plugin.CurrentRevision != 2 {
		t.Fatalf("current revision = %d, want 2", plugin.CurrentRevision)
	}

	revision, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revision == nil || revision.Revision != 2 {
		t.Fatalf("revision = %#v, %v", revision, err)
	}

	assertBuiltinSkillRecordCounts(t, database, 1, 2, 2, 2)
}

func TestSyncWorkerSkillsArchiveOnDelete(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "to-delete", "", "Will be deleted.")

	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Created != 1 {
		t.Fatalf("first sync report = %#v, %v", report, err)
	}

	// Remove the skill directory.
	if err := os.RemoveAll(filepath.Join(sourceDir, "to-delete")); err != nil {
		t.Fatalf("remove source skill: %v", err)
	}

	report, err = SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("sync after delete: %v", err)
	}
	if report.Scanned != 0 {
		t.Fatalf("no skills should be scanned, got %d", report.Scanned)
	}

	// Plugin should be archived, revisions preserved.
	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "to-delete")
	if err != nil || plugin == nil {
		t.Fatalf("plugin = %#v, %v", plugin, err)
	}
	if plugin.Status != types.PluginStatusArchived {
		t.Fatalf("plugin status = %s, want %s", plugin.Status, types.PluginStatusArchived)
	}
	if plugin.CurrentRevision != 1 {
		t.Fatalf("current revision = %d, want 1", plugin.CurrentRevision)
	}

	// Revisions and contents should still exist.
	assertBuiltinSkillRecordCounts(t, database, 1, 1, 1, 1)
}

func TestSyncWorkerSkillsRestoreOnReappear(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "reappear", "", "Same content always.")

	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Created != 1 {
		t.Fatalf("first sync report = %#v, %v", report, err)
	}

	// Delete and sync to archive.
	if err := os.RemoveAll(filepath.Join(sourceDir, "reappear")); err != nil {
		t.Fatalf("remove skill dir: %v", err)
	}
	if _, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir); err != nil {
		t.Fatalf("sync after delete: %v", err)
	}

	// Recreate with same content.
	writeBuiltinSkillTestFiles(t, sourceDir, "reappear", "", "Same content always.")
	report, err = SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("sync after reappear: %v", err)
	}
	if report.Restored != 1 {
		t.Fatalf("restored report = %#v", report)
	}

	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "reappear")
	if err != nil || plugin == nil {
		t.Fatalf("plugin = %#v, %v", plugin, err)
	}
	if plugin.Status != types.PluginStatusActive {
		t.Fatalf("plugin status = %s, want %s", plugin.Status, types.PluginStatusActive)
	}
	if plugin.CurrentRevision != 1 {
		t.Fatalf("current revision = %d, want 1 (no new revision on restore)", plugin.CurrentRevision)
	}

	assertBuiltinSkillRecordCounts(t, database, 1, 1, 1, 1)
}

func TestSyncWorkerSkillsInvalidDirectory(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "good-skill", "", "Good skill body.")

	// Create an invalid skill directory (no SKILL.md).
	if err := os.MkdirAll(filepath.Join(sourceDir, "bad-skill"), 0o755); err != nil {
		t.Fatalf("create invalid skill dir: %v", err)
	}

	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if report.Created != 1 || len(report.Failures) != 1 || report.Failures[0].Code != "bad-skill" {
		t.Fatalf("failure isolation report = %#v", report)
	}

	// Good skill should be created.
	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "good-skill")
	if err != nil || plugin == nil {
		t.Fatalf("good plugin = %#v, %v", plugin, err)
	}
	if plugin.Origin != "builtin_worker" {
		t.Fatalf("good plugin origin = %s", plugin.Origin)
	}
}

func TestSyncWorkerSkillsInvalidDirectoryPreservesActiveRecord(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()

	// First sync: valid skill directory.
	writeBuiltinSkillTestFiles(t, sourceDir, "becomes-invalid", "", "Initially valid.")
	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil || report.Created != 1 {
		t.Fatalf("first sync report = %#v, %v", report, err)
	}

	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "becomes-invalid")
	if err != nil || plugin == nil || plugin.Status != types.PluginStatusActive {
		t.Fatalf("plugin after first sync = %#v, %v", plugin, err)
	}

	// Second sync: break SKILL.md by removing frontmatter name (still a valid file but invalid as skill).
	if err := os.WriteFile(filepath.Join(sourceDir, "becomes-invalid", "SKILL.md"), []byte("---\ndescription: Broken skill\n---\n\nNo name."), 0o644); err != nil {
		t.Fatalf("write broken SKILL.md: %v", err)
	}

	report, err = SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if len(report.Failures) != 1 || report.Failures[0].Code != "becomes-invalid" {
		t.Fatalf("failure report = %#v", report)
	}

	// The plugin should still be active because the directory still exists.
	plugin, err = infradb.GetSystemPluginByCode(context.Background(), database, "skill", "becomes-invalid")
	if err != nil || plugin == nil {
		t.Fatalf("plugin after second sync = %#v, %v", plugin, err)
	}
	if plugin.Status != types.PluginStatusActive {
		t.Fatalf("plugin status = %s, want active (preserved despite validation failure)", plugin.Status)
	}
}

func TestSyncWorkerSkillsOriginConflict(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()

	// Simulate a server builtin plugin with the same code.
	// We need to create a plugin with origin="builtin" first.
	// Use the server sync with a temp dir to create such a plugin.
	serverDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, serverDir, "conflict-skill", "", "Server content.")
	serverReport, err := SyncBuiltinServerSkillMarketplace(context.Background(), database, serverDir)
	if err != nil {
		t.Fatalf("server sync: %v", err)
	}
	if serverReport.Created != 1 {
		t.Fatalf("server sync should create, got %#v", serverReport)
	}

	// Now try to sync same code as worker skill.
	writeBuiltinSkillTestFiles(t, sourceDir, "conflict-skill", "", "Worker content.")
	report, err := SyncBuiltinWorkerSkills(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("worker sync: %v", err)
	}
	if len(report.Failures) != 1 || report.Failures[0].Code != "conflict-skill" {
		t.Fatalf("conflict report = %#v, want 1 failure for conflict-skill", report)
	}

	// Server plugin should be untouched.
	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "conflict-skill")
	if err != nil || plugin == nil {
		t.Fatalf("plugin = %#v, %v", plugin, err)
	}
	if plugin.Origin != "builtin" {
		t.Fatalf("plugin origin = %s, want builtin", plugin.Origin)
	}
}

func TestServerSyncRejectsWorkerOriginConflict(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)

	// First, create a worker builtin plugin.
	workerDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, workerDir, "bidirectional-conflict", "", "Worker content.")
	workerReport, err := SyncBuiltinWorkerSkills(context.Background(), database, workerDir)
	if err != nil || workerReport.Created != 1 {
		t.Fatalf("worker sync report = %#v, %v", workerReport, err)
	}

	// Now try to sync the same code as a server builtin skill.
	serverDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, serverDir, "bidirectional-conflict", "", "Server content.")
	serverReport, err := SyncBuiltinServerSkillMarketplace(context.Background(), database, serverDir)
	if err != nil {
		t.Fatalf("server sync: %v", err)
	}
	if len(serverReport.Failures) != 1 || serverReport.Failures[0].Code != "bidirectional-conflict" {
		t.Fatalf("server conflict report = %#v, want 1 failure for bidirectional-conflict", serverReport)
	}

	// Worker plugin should be untouched.
	plugin, err := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "bidirectional-conflict")
	if err != nil || plugin == nil {
		t.Fatalf("plugin = %#v, %v", plugin, err)
	}
	if plugin.Origin != "builtin_worker" {
		t.Fatalf("plugin origin = %s, want builtin_worker", plugin.Origin)
	}
}

func TestListBuiltinSkillsReturnsOnlyWorkerSkills(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)

	// Create a worker skill.
	workerDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, workerDir, "worker-skill", "", "Worker.")
	if _, err := SyncBuiltinWorkerSkills(context.Background(), database, workerDir); err != nil {
		t.Fatalf("worker sync: %v", err)
	}

	// Create a server builtin skill.
	serverDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, serverDir, "server-skill", "", "Server.")
	if _, err := SyncBuiltinServerSkillMarketplace(context.Background(), database, serverDir); err != nil {
		t.Fatalf("server sync: %v", err)
	}

	// ListBuiltinSkills should only return worker skills.
	svc := &pluginService{db: database}
	resp, err := svc.ListBuiltinSkills(context.Background())
	if err != nil {
		t.Fatalf("list builtin skills: %v", err)
	}
	if len(resp.Plugins) != 1 {
		t.Fatalf("plugin count = %d, want 1", len(resp.Plugins))
	}
	if resp.Plugins[0].Code != "worker-skill" || resp.Plugins[0].Origin != "builtin_worker" {
		t.Fatalf("plugin = %#v", resp.Plugins[0])
	}

	// Archive the worker skill and verify it no longer appears.
	workerPlugin, _ := infradb.GetSystemPluginByCode(context.Background(), database, "skill", "worker-skill")
	if workerPlugin != nil {
		if err := infradb.ArchivePlugin(context.Background(), database, workerPlugin.ID); err != nil {
			t.Fatalf("archive plugin: %v", err)
		}
	}
	resp, err = svc.ListBuiltinSkills(context.Background())
	if err != nil {
		t.Fatalf("list after archive: %v", err)
	}
	if len(resp.Plugins) != 0 {
		t.Fatalf("plugin count after archive = %d, want 0", len(resp.Plugins))
	}
}

func TestListBuiltinSkillsExcludesZeroRevision(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)

	// Create a system plugin without a revision (current_revision = 0).
	plugin := &types.Plugin{
		PublicID: "plugin_test_no_rev", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "no-rev-skill", Kind: "skill",
		Name: "NoRev", Status: types.PluginStatusActive,
		Origin: "builtin_worker", CurrentRevision: 0,
	}
	if err := infradb.CreatePlugin(context.Background(), database, plugin); err != nil {
		t.Fatalf("create zero-rev plugin: %v", err)
	}

	svc := &pluginService{db: database}
	resp, err := svc.ListBuiltinSkills(context.Background())
	if err != nil {
		t.Fatalf("list builtin skills: %v", err)
	}
	if len(resp.Plugins) != 0 {
		t.Fatalf("plugin count = %d, want 0 (current_revision=0 excluded)", len(resp.Plugins))
	}
}
