package service

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"gorm.io/gorm"

	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

func TestSyncBuiltinServerSkillMarketplaceCreatesAndUpdatesSystemRevision(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "official-demo", "1.0.0", "First body.")

	report, err := SyncBuiltinServerSkillMarketplace(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if report.Scanned != 1 || report.Created != 1 || len(report.Failures) != 0 {
		t.Fatalf("first report = %#v", report)
	}
	item, err := infradb.GetPluginMarketplaceItemBySource(
		context.Background(), database, "builtin", "official-demo",
	)
	if err != nil || item == nil {
		t.Fatalf("marketplace item = %#v, %v", item, err)
	}
	if item.Author != "Lework" || item.PluginID == 0 {
		t.Fatalf("marketplace metadata = %#v", item)
	}
	plugin, err := infradb.GetPluginByID(context.Background(), database, item.PluginID)
	if err != nil || plugin == nil {
		t.Fatalf("system plugin = %#v, %v", plugin, err)
	}
	if plugin.OwnerScope != types.OwnerScopeSystem || plugin.OrgID != 0 ||
		plugin.Origin != "builtin" || plugin.CurrentRevision != 1 ||
		plugin.CreatedBy != 0 || plugin.UpdatedBy != 0 {
		t.Fatalf("system plugin ownership = %#v", plugin)
	}
	revisionV1, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revisionV1 == nil {
		t.Fatalf("revision 1 = %#v, %v", revisionV1, err)
	}
	if revisionV1.PublishedByType != "system" || revisionV1.PublishedByID != 0 {
		t.Fatalf("system revision audit = %#v", revisionV1)
	}
	contentV1, err := infradb.GetPluginRevisionContent(
		context.Background(), database, revisionV1.ID,
	)
	if err != nil || contentV1 == nil || len(contentV1.FileIndex) != 2 {
		t.Fatalf("revision 1 content = %#v, %v", contentV1, err)
	}
	artifactV1, err := ArtifactFromDefinition("skill", revisionV1.Definition)
	if err != nil || artifactV1 == nil {
		t.Fatalf("revision 1 artifact = %#v, %v", artifactV1, err)
	}
	fileV1, err := infradb.GetSystemFileUploadByPublicID(
		context.Background(), database, artifactV1.FileUploadID,
	)
	if err != nil || fileV1 == nil || fileV1.OwnerID != 0 ||
		fileV1.Purpose != "artifact" || fileV1.MimeType != "application/zip" {
		t.Fatalf("system artifact = %#v, %v", fileV1, err)
	}

	report, err = SyncBuiltinServerSkillMarketplace(context.Background(), database, sourceDir)
	if err != nil || report.Unchanged != 1 {
		t.Fatalf("idempotent report = %#v, %v", report, err)
	}
	assertBuiltinSkillRecordCounts(t, database, 1, 1, 1, 1)

	writeBuiltinSkillTestFiles(t, sourceDir, "official-demo", "99.7.3", "Second body.")
	report, err = SyncBuiltinServerSkillMarketplace(context.Background(), database, sourceDir)
	if err != nil || report.Updated != 1 {
		t.Fatalf("update report = %#v, %v", report, err)
	}
	if err := database.First(plugin, plugin.ID).Error; err != nil {
		t.Fatalf("reload system plugin: %v", err)
	}
	if plugin.CurrentRevision != 2 {
		t.Fatalf("current revision = %d, want 2", plugin.CurrentRevision)
	}
	revisionV2, err := infradb.GetCurrentPluginRevision(context.Background(), database, plugin)
	if err != nil || revisionV2 == nil || revisionV2.Revision != 2 {
		t.Fatalf("revision 2 = %#v, %v", revisionV2, err)
	}
	if revisionV2.Revision == 99 {
		t.Fatal("frontmatter version was used as database revision")
	}
	assertBuiltinSkillRecordCounts(t, database, 1, 2, 2, 2)

	view, err := (&pluginService{db: database}).GetOfficialPluginMarketplaceItem(
		context.Background(), 1, item.PublicID,
	)
	if err != nil || view.Version != "2" || view.Content == nil ||
		view.Content.Version != 2 || view.Content.SkillMD != "Second body." {
		t.Fatalf("marketplace detail = %#v, %v", view, err)
	}
}

func TestRepositoryBuiltinServerSkillsPassUnifiedPublisher(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	report, err := SyncBuiltinServerSkillMarketplace(context.Background(), database, "")
	if err != nil {
		t.Fatalf("sync repository built-in Skills: %v", err)
	}
	if report.Scanned == 0 || len(report.Failures) != 0 ||
		report.Created != report.Scanned {
		t.Fatalf("repository built-in Skill report = %#v", report)
	}
	var wrongAuthors int64
	if err := database.Model(&types.PluginMarketplaceItem{}).
		Where("author <> ?", "Lework").
		Count(&wrongAuthors).Error; err != nil {
		t.Fatalf("count marketplace authors: %v", err)
	}
	if wrongAuthors != 0 {
		t.Fatalf("%d built-in marketplace items have a non-Lework author", wrongAuthors)
	}
}

func TestSyncBuiltinServerSkillMarketplaceIsolatesFailuresAndKeepsDeletedSource(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "good-skill", "", "Good.")
	if err := os.MkdirAll(filepath.Join(sourceDir, "bad-skill"), 0o755); err != nil {
		t.Fatalf("create invalid Skill: %v", err)
	}

	report, err := SyncBuiltinServerSkillMarketplace(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("sync with invalid Skill: %v", err)
	}
	if report.Created != 1 || len(report.Failures) != 1 ||
		report.Failures[0].Code != "bad-skill" {
		t.Fatalf("failure isolation report = %#v", report)
	}
	item, err := infradb.GetPluginMarketplaceItemBySource(
		context.Background(), database, "builtin", "good-skill",
	)
	if err != nil || item == nil {
		t.Fatalf("good marketplace item = %#v, %v", item, err)
	}
	if err := os.RemoveAll(filepath.Join(sourceDir, "good-skill")); err != nil {
		t.Fatalf("remove source Skill: %v", err)
	}
	report, err = SyncBuiltinServerSkillMarketplace(context.Background(), database, sourceDir)
	if err != nil {
		t.Fatalf("sync after source deletion: %v", err)
	}
	if got, err := infradb.GetPluginMarketplaceItemBySource(
		context.Background(), database, "builtin", "good-skill",
	); err != nil || got == nil || got.ID != item.ID {
		t.Fatalf("deleted source marketplace retention = %#v, %v", got, err)
	}
}

func TestSyncBuiltinServerSkillMarketplaceConcurrentCallsRemainUnique(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	sourceDir := t.TempDir()
	writeBuiltinSkillTestFiles(t, sourceDir, "concurrent-skill", "", "Concurrent.")

	var wait sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := SyncBuiltinServerSkillMarketplace(
				context.Background(), database, sourceDir,
			)
			errors <- err
		}()
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("concurrent sync: %v", err)
		}
	}
	assertBuiltinSkillRecordCounts(t, database, 1, 1, 1, 1)
	var itemCount int64
	if err := database.Model(&types.PluginMarketplaceItem{}).Count(&itemCount).Error; err != nil {
		t.Fatalf("count marketplace items: %v", err)
	}
	if itemCount != 1 {
		t.Fatalf("marketplace item count = %d, want 1", itemCount)
	}
}

func writeBuiltinSkillTestFiles(
	t *testing.T,
	root, code, version, body string,
) {
	t.Helper()
	skillDir := filepath.Join(root, code)
	if err := os.MkdirAll(filepath.Join(skillDir, "references"), 0o755); err != nil {
		t.Fatalf("create Skill directory: %v", err)
	}
	versionLine := ""
	if version != "" {
		versionLine = "version: " + version + "\n"
	}
	document := "---\nname: " + code + "\ndescription: Official demo\n" +
		versionLine + "metadata:\n  category: official\n  tags: [demo, builtin]\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(document), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(skillDir, "references", "guide.md"),
		[]byte("Guide."),
		0o644,
	); err != nil {
		t.Fatalf("write reference: %v", err)
	}
}

func assertBuiltinSkillRecordCounts(
	t *testing.T,
	database *gorm.DB,
	plugins, revisions, contents, files int64,
) {
	t.Helper()
	tests := []struct {
		name  string
		model interface{}
		want  int64
	}{
		{name: "plugins", model: &types.Plugin{}, want: plugins},
		{name: "revisions", model: &types.PluginRevision{}, want: revisions},
		{name: "contents", model: &types.PluginRevisionContent{}, want: contents},
		{name: "files", model: &types.FileUpload{}, want: files},
	}
	for _, test := range tests {
		var got int64
		if err := database.Model(test.model).Count(&got).Error; err != nil {
			t.Fatalf("count %s: %v", test.name, err)
		}
		if got != test.want {
			t.Fatalf("%s count = %d, want %d", test.name, got, test.want)
		}
	}
}
