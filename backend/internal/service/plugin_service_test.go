package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	_ "github.com/ygpkg/storage-go/driver/local"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/types"
)

func setupPluginServiceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(
		&types.Project{},
		&types.Plugin{},
		&types.PluginRevision{},
		&types.PluginRevisionContent{},
		&types.ProjectPluginBinding{},
		&types.PluginMarketplaceItem{},
		&types.MCPChannel{},
		&types.FileUpload{},
	); err != nil {
		t.Fatalf("migrate plugin service models: %v", err)
	}
	return database
}

func setupPluginServiceTestStorage(t *testing.T) {
	t.Helper()
	if err := filestore.Init(&config.StorageConfig{
		Driver:     "local",
		LocalDir:   t.TempDir(),
		Bucket:     "plugin-test",
		BaseURL:    "http://127.0.0.1:8080/storage",
		SignSecret: "plugin-test-secret",
	}); err != nil {
		t.Fatalf("initialize plugin test storage: %v", err)
	}
}

func uploadPluginServiceSkillArtifact(
	t *testing.T,
	database *gorm.DB,
	orgID, ownerID uint,
	code, body string,
) (*types.FileUpload, json.RawMessage) {
	t.Helper()
	archive := testSkillArchive(t, map[string]string{
		"SKILL.md": fmt.Sprintf(
			"---\nname: %s\ndescription: %s helper\n---\n\n%s",
			code,
			code,
			body,
		),
		"references/guide.md": "Guide for " + code,
	})
	archiveHash := sha256.Sum256(archive)
	scope := types.OwnerScopeOrganization
	if orgID == 0 {
		scope = types.OwnerScopeSystem
	}
	file, err := filestore.Upload(context.Background(), database, filestore.UploadParams{
		Data:         archive,
		Filename:     code + ".zip",
		OriginalName: code + ".zip",
		MimeType:     "application/zip",
		OwnerScope:   scope,
		OrgID:        orgID,
		OwnerID:      ownerID,
		ObjectKey:    fmt.Sprintf("plugin-tests/%s/%s-%s.zip", t.Name(), code, hex.EncodeToString(archiveHash[:4])),
		Purpose:      filestore.PurposeArtifact,
	})
	if err != nil {
		t.Fatalf("upload Skill artifact: %v", err)
	}
	definition := json.RawMessage(fmt.Sprintf(
		`{"schema":"skill/v1","artifact":{"file_upload_id":%q,"sha256":%q,"size_bytes":%d,"content_type":"application/zip"}}`,
		file.PublicID,
		file.Sha256,
		file.FileSize,
	))
	return file, definition
}

func createPluginServiceSystemSkill(
	t *testing.T,
	database *gorm.DB,
	code, body string,
) (*types.FileUpload, *types.Plugin, *types.PluginRevision) {
	t.Helper()
	plugin := &types.Plugin{
		PublicID: "plugin_system_" + code, OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: code, Kind: "skill", Name: code,
		Description: code + " helper", Status: types.PluginStatusActive,
		Origin: "builtin", CurrentRevision: 0, CreatedBy: 0, UpdatedBy: 0,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create system plugin: %v", err)
	}
	file, revision := addPluginServiceSystemRevision(t, database, plugin, 1, body)
	return file, plugin, revision
}

func createPluginServiceMarketplaceItem(
	t *testing.T,
	database *gorm.DB,
	plugin *types.Plugin,
	publicID string,
) *types.PluginMarketplaceItem {
	t.Helper()
	item := &types.PluginMarketplaceItem{
		PublicID:    publicID,
		PluginID:    plugin.ID,
		Kind:        "skill",
		Code:        plugin.Code,
		Name:        plugin.Name,
		Author:      "Lework",
		SourceType:  "builtin",
		SourceRef:   plugin.Code,
		Status:      "published",
		Tags:        types.PluginStringList{},
		PublishedAt: time.Now(),
	}
	if err := database.Create(item).Error; err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}
	return item
}

func createPluginServiceOrganizationSkill(
	t *testing.T,
	database *gorm.DB,
	orgID, ownerID uint,
	code, body string,
) (*types.FileUpload, *types.Plugin, *types.PluginRevision) {
	t.Helper()
	file, definition := uploadPluginServiceSkillArtifact(
		t,
		database,
		orgID,
		ownerID,
		code,
		body,
	)
	plugin := &types.Plugin{
		PublicID:        "plugin_org_" + code,
		OwnerScope:      types.OwnerScopeOrganization,
		OrgID:           orgID,
		Code:            code,
		Kind:            "skill",
		Name:            code,
		Status:          types.PluginStatusActive,
		Origin:          "org",
		CurrentRevision: 1,
		CreatedBy:       ownerID,
		UpdatedBy:       ownerID,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create organization plugin: %v", err)
	}
	revision := &types.PluginRevision{
		PluginID:        plugin.ID,
		Revision:        1,
		Status:          "published",
		Definition:      definition,
		PublishedByType: "user",
		PublishedByID:   ownerID,
		PublishedAt:     time.Now(),
	}
	if err := database.Create(revision).Error; err != nil {
		t.Fatalf("create organization revision: %v", err)
	}
	return file, plugin, revision
}

func addPluginServiceSystemRevision(
	t *testing.T,
	database *gorm.DB,
	plugin *types.Plugin,
	revisionNumber int,
	body string,
) (*types.FileUpload, *types.PluginRevision) {
	t.Helper()
	file, definition := uploadPluginServiceSkillArtifact(
		t, database, 0, 0, plugin.Code, body,
	)
	revision := &types.PluginRevision{
		PluginID: plugin.ID, Revision: revisionNumber, Status: "published",
		Definition: definition, PublishedByType: "system",
		PublishedByID: 0, PublishedAt: time.Now(),
	}
	if err := database.Create(revision).Error; err != nil {
		t.Fatalf("create system revision: %v", err)
	}
	reader, err := filestore.OpenFileUpload(context.Background(), file)
	if err != nil {
		t.Fatalf("open system artifact: %v", err)
	}
	archive, err := io.ReadAll(reader)
	reader.Close()
	if err != nil {
		t.Fatalf("read system artifact: %v", err)
	}
	content, err := buildSkillRevisionContent(archive, file.Sha256)
	if err != nil {
		t.Fatalf("build system content: %v", err)
	}
	if err := database.Create(content.model(revision.ID)).Error; err != nil {
		t.Fatalf("create system content: %v", err)
	}
	if err := database.Model(plugin).
		Select("current_revision").
		Update("current_revision", revisionNumber).Error; err != nil {
		t.Fatalf("update system current revision: %v", err)
	}
	plugin.CurrentRevision = revisionNumber
	return file, revision
}

func TestPluginServiceScopesListsAndDeletes(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	ctx := context.Background()
	plugin := &types.Plugin{PublicID: "plg_service", OrgID: 1, Code: "service", Kind: "skill", Name: "Service", Status: types.PluginStatusActive, Origin: "org", CurrentRevision: 1, CreatedBy: 8, UpdatedBy: 8}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if err := database.Create(&types.PluginRevision{PluginID: plugin.ID, Revision: 1, Status: "published", Definition: []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_service","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`), PublishedByType: "user", PublishedByID: 8, PublishedAt: time.Now()}).Error; err != nil {
		t.Fatalf("create revision: %v", err)
	}
	project := &types.Project{PublicID: "prj_service", OrgID: 1, OwnerID: 8, Name: "Service"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := database.Create(&types.ProjectPluginBinding{ProjectID: project.ID, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 8, UpdatedBy: 8}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	systemPlugin := &types.Plugin{
		PublicID: "plugin_system_service", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "service", Kind: "skill", Name: "Service",
		Status: types.PluginStatusActive, Origin: "builtin",
	}
	if err := database.Create(systemPlugin).Error; err != nil {
		t.Fatalf("create system plugin: %v", err)
	}
	if err := database.Create(&types.PluginMarketplaceItem{PublicID: "mkt_service", PluginID: systemPlugin.ID, Kind: "skill", Code: "service", Name: "Service", Author: "Lework", SourceType: "builtin", SourceRef: "service", Status: "published", Tags: types.PluginStringList{}, PublishedAt: time.Now()}).Error; err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}

	service := NewPluginService(database)
	list, err := service.ListPlugins(ctx, 1, 8, &contract.ListPluginsRequest{})
	if err != nil || len(list.Plugins) != 1 || list.Plugins[0].PublicID != plugin.PublicID {
		t.Fatalf("organization list = %#v, %v", list, err)
	}
	if _, err := service.GetPlugin(ctx, 2, 8, plugin.PublicID, &contract.GetPluginRequest{}); !errors.Is(err, contract.ErrPluginNotFound) {
		t.Fatalf("cross-org detail error = %v, want not found", err)
	}
	versions, err := service.ListPluginVersions(ctx, 1, 8, plugin.PublicID)
	if err != nil || len(versions.Versions) != 1 || versions.Versions[0].Revision != 1 {
		t.Fatalf("versions = %#v, %v", versions, err)
	}

	result, err := service.DeletePlugin(ctx, 1, 8, plugin.PublicID, &contract.DeletePluginRequest{ProjectID: project.PublicID})
	if err != nil || result.Operation != "project_unbound" {
		t.Fatalf("unbind = %#v, %v", result, err)
	}
	result, err = service.DeletePlugin(ctx, 1, 8, plugin.PublicID, &contract.DeletePluginRequest{})
	if err != nil || result.Operation != "deleted" {
		t.Fatalf("delete = %#v, %v", result, err)
	}
	list, err = service.ListPlugins(ctx, 1, 8, &contract.ListPluginsRequest{})
	if err != nil || len(list.Plugins) != 0 {
		t.Fatalf("default list after delete = %#v, %v", list, err)
	}
	if _, err := service.GetPlugin(ctx, 1, 8, plugin.PublicID, &contract.GetPluginRequest{}); !errors.Is(err, contract.ErrPluginNotFound) {
		t.Fatalf("deleted plugin detail error = %v, want not found", err)
	}
	var deleted types.Plugin
	if err := database.Unscoped().Where("public_id = ?", plugin.PublicID).First(&deleted).Error; err != nil {
		t.Fatalf("load deleted plugin: %v", err)
	}
	if !deleted.DeletedAt.Valid {
		t.Fatalf("deleted_at is not set: %#v", deleted)
	}
}

func TestPluginServiceReturnsCurrentContentSnapshot(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	ctx := context.Background()
	plugin := &types.Plugin{
		PublicID: "plugin_content", OrgID: 1, Code: "content", Kind: "skill", Name: "Content",
		Status: types.PluginStatusActive, Origin: "org", CurrentRevision: 2, CreatedBy: 8, UpdatedBy: 8,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	revision := &types.PluginRevision{
		PluginID: plugin.ID, Revision: 2, Status: "published",
		Definition:      []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_content","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`),
		PublishedByType: "user", PublishedByID: 8, PublishedAt: time.Now(),
	}
	if err := database.Create(revision).Error; err != nil {
		t.Fatalf("create revision: %v", err)
	}
	rawSkillMD := "---\nname: content\ndescription: Content helper\n---\n\n# Current content"
	if err := database.Create(&types.PluginRevisionContent{
		PluginRevisionID:  revision.ID,
		Schema:            types.PluginRevisionContentSchemaSkillV1,
		ArtifactSHA256:    "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		EntrypointPath:    "SKILL.md",
		EntrypointContent: rawSkillMD,
		FileIndex: types.PluginRevisionFileList{
			{Path: "SKILL.md", SizeBytes: int64(len(rawSkillMD)), SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			{Path: "scripts/check.go", SizeBytes: 10, SHA256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		},
	}).Error; err != nil {
		t.Fatalf("create revision content: %v", err)
	}

	result, err := (&pluginService{db: database}).GetPlugin(ctx, 1, 8, plugin.PublicID, &contract.GetPluginRequest{})
	if err != nil {
		t.Fatalf("get plugin detail: %v", err)
	}
	if result.Content == nil || result.Content.Version != 2 || result.Content.SkillMD != "# Current content" {
		t.Fatalf("plugin content = %#v", result.Content)
	}
	if len(result.Content.Files) != 2 || result.Content.Files[1].Path != "scripts/check.go" {
		t.Fatalf("plugin files = %#v", result.Content.Files)
	}
}

func TestAddSkillPluginCreatesContentSnapshotsFromZIPAndMarkdown(t *testing.T) {
	tests := []struct {
		name         string
		originalName string
		data         func(t *testing.T) []byte
	}{
		{
			name:         "zip",
			originalName: "zip-skill.zip",
			data: func(t *testing.T) []byte {
				return testSkillArchive(t, map[string]string{
					"SKILL.md":         "---\nname: zip-skill\ndescription: ZIP helper\n---\n\n# ZIP content",
					"scripts/check.sh": "echo check",
				})
			},
		},
		{
			name:         "markdown",
			originalName: "SKILL.md",
			data: func(t *testing.T) []byte {
				return []byte("---\nname: markdown-skill\ndescription: Markdown helper\n---\n\n# Markdown content")
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := setupPluginServiceTestDB(t)
			setupPluginServiceTestStorage(t)
			ctx := context.Background()
			file, err := filestore.Upload(ctx, database, filestore.UploadParams{
				Data:         test.data(t),
				Filename:     test.originalName,
				OriginalName: test.originalName,
				MimeType:     "application/octet-stream",
				OrgID:        1,
				OwnerID:      9,
				ObjectKey:    "uploads/" + test.name + "/" + test.originalName,
				Purpose:      filestore.PurposeAttachment,
			})
			if err != nil {
				t.Fatalf("upload Skill source: %v", err)
			}
			service := &pluginService{db: database}
			if err := service.AddSkillPlugin(ctx, 1, 9, &contract.AddSkillPluginRequest{
				Mode:         contract.SkillAddModeFile,
				FileUploadID: file.PublicID,
			}); err != nil {
				t.Fatalf("add Skill: %v", err)
			}
			var plugin types.Plugin
			if err := database.Where("org_id = ?", 1).First(&plugin).Error; err != nil {
				t.Fatalf("load plugin: %v", err)
			}
			revision, err := infradb.GetCurrentPluginRevision(ctx, database, &plugin)
			if err != nil || revision == nil {
				t.Fatalf("current revision = %#v, %v", revision, err)
			}
			content, err := infradb.GetPluginRevisionContent(ctx, database, revision.ID)
			if err != nil || content == nil || content.EntrypointContent == "" {
				t.Fatalf("revision content = %#v, %v", content, err)
			}
			if content.FileIndex[0].Path != "SKILL.md" {
				t.Fatalf("file index = %#v", content.FileIndex)
			}
		})
	}
}

func TestPublishSkillRevisionReactivatesArchivedPlugin(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	plugin := &types.Plugin{PublicID: "plg_archived", OrgID: 1, Code: "archived", Kind: "skill", Name: "Archived", Status: types.PluginStatusArchived, Origin: "org", CurrentRevision: 1, CreatedBy: 8, UpdatedBy: 8}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	if err := database.Create(&types.PluginRevision{PluginID: plugin.ID, Revision: 1, Status: "published", Definition: []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_old","sha256":"old"}}`), PublishedByType: "user", PublishedByID: 8, PublishedAt: time.Now()}).Error; err != nil {
		t.Fatalf("create revision: %v", err)
	}

	service := &pluginService{db: database}
	archive := testSkillArchive(t, map[string]string{
		"SKILL.md": "---\nname: archived\ndescription: Archived helper\n---\n\nNew content.",
	})
	hash := sha256.Sum256(archive)
	sha := hex.EncodeToString(hash[:])
	definition := json.RawMessage(fmt.Sprintf(
		`{"schema":"skill/v1","artifact":{"file_upload_id":"file_new","sha256":%q}}`,
		sha,
	))
	contentDraft, err := buildSkillRevisionContent(archive, sha)
	if err != nil {
		t.Fatalf("build content draft: %v", err)
	}
	if err := service.publishSkillRevision(context.Background(), 1, 9, "archived", "Archived", "", definition, contentDraft); err != nil {
		t.Fatalf("publish revision: %v", err)
	}

	var got types.Plugin
	if err := database.First(&got, plugin.ID).Error; err != nil {
		t.Fatalf("reload plugin: %v", err)
	}
	if got.Status != types.PluginStatusActive || got.CurrentRevision != 2 {
		t.Fatalf("plugin after republish = %#v", got)
	}
	artifact, err := ArtifactFromDefinition("skill", definition)
	if err != nil || artifact == nil || artifact.FileUploadID != "file_new" {
		t.Fatalf("artifact = %#v, err = %v", artifact, err)
	}
}

func TestDeletedSkillReusesIdentityAndPublishesNextVersion(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	service := &pluginService{db: database}
	ctx := context.Background()
	archive := testSkillArchive(t, map[string]string{
		"SKILL.md":        "---\nname: restored\ndescription: Restored helper\n---\n\nRestore me.",
		"scripts/run.sh":  "#!/bin/sh\n",
		"references/a.md": "Reference.",
	})
	hash := sha256.Sum256(archive)
	sha := hex.EncodeToString(hash[:])
	definition := json.RawMessage(fmt.Sprintf(
		`{"schema":"skill/v1","artifact":{"file_upload_id":"file_restored","sha256":%q}}`,
		sha,
	))
	contentDraft, err := buildSkillRevisionContent(archive, sha)
	if err != nil {
		t.Fatalf("build content draft: %v", err)
	}
	if err := service.publishSkillRevision(ctx, 1, 9, "restored", "Restored", "", definition, contentDraft); err != nil {
		t.Fatalf("publish first revision: %v", err)
	}
	var original types.Plugin
	if err := database.Where("org_id = ? AND code = ?", 1, "restored").First(&original).Error; err != nil {
		t.Fatalf("load original plugin: %v", err)
	}
	project := &types.Project{PublicID: "project_restored", OrgID: 1, OwnerID: 9, Name: "Restored"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := database.Create(&types.ProjectPluginBinding{
		ProjectID: project.ID, PluginID: original.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 9, UpdatedBy: 9,
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	if _, err := service.DeletePlugin(ctx, 1, 9, original.PublicID, &contract.DeletePluginRequest{}); err != nil {
		t.Fatalf("delete plugin: %v", err)
	}
	if err := service.publishSkillRevision(ctx, 1, 10, "restored", "Restored", "", definition, contentDraft); err != nil {
		t.Fatalf("restore plugin: %v", err)
	}

	var restored types.Plugin
	if err := database.Where("org_id = ? AND code = ?", 1, "restored").First(&restored).Error; err != nil {
		t.Fatalf("load restored plugin: %v", err)
	}
	if restored.ID != original.ID || restored.PublicID != original.PublicID || restored.CurrentRevision != 2 {
		t.Fatalf("restored plugin = %#v, original = %#v", restored, original)
	}
	revision, err := infradb.GetPluginRevisionByNumber(ctx, database, restored.ID, 2)
	if err != nil || revision == nil {
		t.Fatalf("restored revision = %#v, %v", revision, err)
	}
	content, err := infradb.GetPluginRevisionContent(ctx, database, revision.ID)
	if err != nil || content == nil {
		t.Fatalf("restored content = %#v, %v", content, err)
	}
	var activeBindings int64
	if err := database.Model(&types.ProjectPluginBinding{}).
		Where("plugin_id = ?", restored.ID).
		Count(&activeBindings).Error; err != nil {
		t.Fatalf("count active bindings: %v", err)
	}
	if activeBindings != 0 {
		t.Fatalf("active bindings after restore = %d, want 0", activeBindings)
	}
}

func TestOfficialPluginMarketplaceInstallsAndTracksOfficialVersion(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, sourceRevisionV1 := createPluginServiceSystemSkill(
		t, database, "official-skill", "Version one.",
	)
	item := &types.PluginMarketplaceItem{
		PublicID: "mkt_official", PluginID: sourcePlugin.ID, Kind: "skill",
		Code: "official-skill", Name: "Official Skill", Description: "first",
		Author: "Lework", SourceType: "builtin", SourceRef: "official-skill",
		Status: "published",
		Tags:   types.PluginStringList{}, PublishedAt: time.Now(),
	}
	if err := database.Create(item).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&types.PluginMarketplaceItem{PublicID: "mkt_archived", PluginID: sourcePlugin.ID, Kind: "skill", Code: "archived", Name: "Archived", Author: "Lework", SourceType: "builtin", SourceRef: "archived", Status: "archived", Tags: types.PluginStringList{}, PublishedAt: time.Now()}).Error; err != nil {
		t.Fatal(err)
	}
	service := NewOfficialPluginMarketplaceService(database)
	list, err := service.ListOfficialPluginMarketplaceItems(ctx, 7, &contract.ListOfficialPluginMarketplaceItemsRequest{Kind: "skill"})
	if err != nil || len(list.Items) != 1 || list.Items[0].PublicID != item.PublicID ||
		list.Items[0].Version != "1" || list.Items[0].Author != "Lework" {
		t.Fatalf("official marketplace list = %#v, %v", list, err)
	}

	installed, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID)
	if err != nil || installed.Operation != "installed" || installed.Plugin.CurrentRevision != 1 {
		t.Fatalf("install official plugin = %#v, %v", installed, err)
	}
	var plugin types.Plugin
	if err := database.Where("org_id = ? AND code = ?", 7, item.Code).First(&plugin).Error; err != nil {
		t.Fatal(err)
	}
	var revision types.PluginRevision
	if err := database.Where("plugin_id = ? AND revision = ?", plugin.ID, 1).First(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if revision.SourceMarketplaceItemID == nil || *revision.SourceMarketplaceItemID != item.ID ||
		revision.SourcePluginRevisionID == nil ||
		*revision.SourcePluginRevisionID != sourceRevisionV1.ID {
		t.Fatalf("official revision source = %#v", revision)
	}
	content, err := infradb.GetPluginRevisionContent(ctx, database, revision.ID)
	if err != nil || content == nil || content.EntrypointContent == "" || len(content.FileIndex) != 2 {
		t.Fatalf("official revision content = %#v, %v", content, err)
	}

	_, sourceRevisionV2 := addPluginServiceSystemRevision(
		t, database, sourcePlugin, 2, "Version two.",
	)
	if err := database.Model(item).Updates(map[string]interface{}{"name": "Official Skill v2", "description": "second"}).Error; err != nil {
		t.Fatal(err)
	}
	updated, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID)
	if err != nil || updated.Operation != "updated" || updated.Plugin.CurrentRevision != 2 {
		t.Fatalf("update official plugin = %#v, %v", updated, err)
	}
	revision = types.PluginRevision{}
	if err := database.Where("plugin_id = ? AND revision = ?", plugin.ID, 2).First(&revision).Error; err != nil {
		t.Fatal(err)
	}
	if revision.SourcePluginRevisionID == nil ||
		*revision.SourcePluginRevisionID != sourceRevisionV2.ID ||
		string(revision.Definition) != string(sourceRevisionV2.Definition) {
		t.Fatalf("updated official revision = %#v", revision)
	}
	content, err = infradb.GetPluginRevisionContent(ctx, database, revision.ID)
	if err != nil || content == nil {
		t.Fatalf("updated official revision content = %#v, %v", content, err)
	}
	alreadyCurrent, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID)
	if err != nil || alreadyCurrent.Operation != "already_current" || alreadyCurrent.Plugin.CurrentRevision != 2 {
		t.Fatalf("idempotent official install = %#v, %v", alreadyCurrent, err)
	}
}

func TestOfficialPluginMarketplaceUsesInstalledVersionAndKeepsArchivedItemVisible(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"effective-market",
		"Installed body.",
	)
	sourcePlugin.Name = "Effective Market"
	item := createPluginServiceMarketplaceItem(t, database, sourcePlugin, "mkt_effective_market")
	item.Name = "Effective Market"
	item.Description = "Installed description"
	if err := database.Save(item).Error; err != nil {
		t.Fatalf("update marketplace metadata: %v", err)
	}
	service := &pluginService{db: database}

	latest, err := service.GetOfficialPluginMarketplaceItem(ctx, 7, item.PublicID)
	if err != nil || latest.Installed || !latest.MarketplaceAvailable ||
		latest.Version != "1" || latest.LatestVersion != "1" ||
		latest.Content == nil || latest.Content.SkillMD != "Installed body." {
		t.Fatalf("uninstalled marketplace detail = %#v, %v", latest, err)
	}
	if _, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID); err != nil {
		t.Fatalf("install marketplace Skill: %v", err)
	}
	if _, _ = addPluginServiceSystemRevision(t, database, sourcePlugin, 2, "Latest body."); sourcePlugin.CurrentRevision != 2 {
		t.Fatalf("source current revision = %d", sourcePlugin.CurrentRevision)
	}
	if err := database.Model(item).Updates(map[string]interface{}{
		"name":        "Effective Market v2",
		"description": "Latest description",
	}).Error; err != nil {
		t.Fatalf("update latest marketplace metadata: %v", err)
	}

	installed, err := service.GetOfficialPluginMarketplaceItem(ctx, 7, item.PublicID)
	if err != nil || !installed.Installed || installed.InstalledPluginID == "" ||
		!installed.MarketplaceAvailable || installed.Version != "1" ||
		installed.LatestVersion != "2" || !installed.UpdateAvailable ||
		installed.Name != "Effective Market" ||
		installed.Description != "Installed description" ||
		installed.Content == nil || installed.Content.Version != 1 ||
		installed.Content.SkillMD != "Installed body." {
		t.Fatalf("installed marketplace detail = %#v, %v", installed, err)
	}
	otherOrg, err := service.GetOfficialPluginMarketplaceItem(ctx, 8, item.PublicID)
	if err != nil || otherOrg.Installed || otherOrg.Version != "2" ||
		otherOrg.Content == nil || otherOrg.Content.SkillMD != "Latest body." {
		t.Fatalf("other organization detail = %#v, %v", otherOrg, err)
	}
	installedPlugin, err := infradb.GetPluginByPublicID(
		ctx,
		database,
		7,
		installed.InstalledPluginID,
	)
	if err != nil || installedPlugin == nil {
		t.Fatalf("load installed marketplace plugin = %#v, %v", installedPlugin, err)
	}
	installedRevision, err := infradb.GetCurrentPluginRevision(ctx, database, installedPlugin)
	if err != nil || installedRevision == nil {
		t.Fatalf("load installed marketplace revision = %#v, %v", installedRevision, err)
	}
	if err := database.Where("plugin_revision_id = ?", installedRevision.ID).
		Delete(&types.PluginRevisionContent{}).Error; err != nil {
		t.Fatalf("delete installed content snapshot: %v", err)
	}
	missingContent, err := service.GetOfficialPluginMarketplaceItem(ctx, 7, item.PublicID)
	if err != nil || missingContent.Version != "1" || missingContent.Content != nil {
		t.Fatalf("installed marketplace detail without snapshot = %#v, %v", missingContent, err)
	}

	if err := database.Model(item).Update("status", "archived").Error; err != nil {
		t.Fatalf("archive marketplace item: %v", err)
	}
	archivedList, err := service.ListOfficialPluginMarketplaceItems(
		ctx,
		7,
		&contract.ListOfficialPluginMarketplaceItemsRequest{Kind: "skill"},
	)
	if err != nil || len(archivedList.Items) != 1 ||
		archivedList.Items[0].PublicID != item.PublicID ||
		archivedList.Items[0].Version != "1" ||
		archivedList.Items[0].MarketplaceAvailable ||
		archivedList.Items[0].UpdateAvailable {
		t.Fatalf("archived installed marketplace list = %#v, %v", archivedList, err)
	}
	otherList, err := service.ListOfficialPluginMarketplaceItems(
		ctx,
		8,
		&contract.ListOfficialPluginMarketplaceItemsRequest{Kind: "skill"},
	)
	if err != nil || len(otherList.Items) != 0 {
		t.Fatalf("other organization archived marketplace list = %#v, %v", otherList, err)
	}
	if _, err := service.GetOfficialPluginMarketplaceItem(ctx, 8, item.PublicID); !errors.Is(
		err,
		contract.ErrPluginNotFound,
	) {
		t.Fatalf("other organization archived detail error = %v", err)
	}
}

func TestOfficialPluginMarketplaceMarksOrganizationOverrideAndFiltersMySkills(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"a-market-skill",
		"Market body.",
	)
	item := createPluginServiceMarketplaceItem(t, database, sourcePlugin, "mkt_filter_market")
	service := &pluginService{db: database}
	if _, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID); err != nil {
		t.Fatalf("install marketplace Skill: %v", err)
	}
	_, customPlugin, _ := createPluginServiceOrganizationSkill(
		t,
		database,
		7,
		9,
		"z-custom-skill",
		"Custom body.",
	)

	filtered, err := service.ListPlugins(ctx, 7, 9, &contract.ListPluginsRequest{
		Kind:                    "skill",
		Status:                  types.PluginStatusActive,
		Limit:                   1,
		ExcludeMarketplaceBased: true,
	})
	if err != nil || len(filtered.Plugins) != 1 ||
		filtered.Plugins[0].PublicID != customPlugin.PublicID {
		t.Fatalf("filtered organization Skills = %#v, %v", filtered, err)
	}

	_, conflictPlugin, _ := createPluginServiceOrganizationSkill(
		t,
		database,
		8,
		9,
		item.Code,
		"Organization override.",
	)
	conflict, err := service.GetOfficialPluginMarketplaceItem(ctx, 8, item.PublicID)
	if err != nil || conflict.Installed || !conflict.OrganizationOverride ||
		conflict.Version != "1" || conflict.InstalledPluginID != "" {
		t.Fatalf("organization override marketplace detail = %#v, %v", conflict, err)
	}
	conflictMine, err := service.ListPlugins(ctx, 8, 9, &contract.ListPluginsRequest{
		Kind:                    "skill",
		Status:                  types.PluginStatusActive,
		ExcludeMarketplaceBased: true,
	})
	if err != nil || len(conflictMine.Plugins) != 1 ||
		conflictMine.Plugins[0].PublicID != conflictPlugin.PublicID {
		t.Fatalf("organization override My Skills = %#v, %v", conflictMine, err)
	}
}

func TestOfficialPluginLatestVersionReturnsAvailabilityByIdentity(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"latest-skill",
		"Version one.",
	)
	item := &types.PluginMarketplaceItem{
		PublicID: "mkt_latest", PluginID: sourcePlugin.ID, Kind: "skill",
		Code: "latest-skill", Name: "Latest Skill", Author: "Lework",
		SourceType: "builtin", SourceRef: "latest-skill", Status: "published",
		Tags: types.PluginStringList{}, PublishedAt: time.Now(),
	}
	if err := database.Create(item).Error; err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}
	service := &pluginService{db: database}

	missing, err := service.GetOfficialPluginLatestVersion(
		ctx,
		&contract.GetOfficialPluginLatestVersionRequest{
			Kind: "skill",
			Code: "missing-skill",
		},
	)
	if err != nil || missing.Available || missing.LatestVersion != "" {
		t.Fatalf("missing latest version = %#v, %v", missing, err)
	}

	available, err := service.GetOfficialPluginLatestVersion(
		ctx,
		&contract.GetOfficialPluginLatestVersionRequest{
			Kind: "skill",
			Code: "latest-skill",
		},
	)
	if err != nil || !available.Available || available.ItemID != item.PublicID ||
		available.LatestVersion != "1" {
		t.Fatalf("available latest version = %#v, %v", available, err)
	}

	differentKind, err := service.GetOfficialPluginLatestVersion(
		ctx,
		&contract.GetOfficialPluginLatestVersionRequest{
			Kind: "mcp",
			Code: "latest-skill",
		},
	)
	if err != nil || differentKind.Available {
		t.Fatalf("different kind latest version = %#v, %v", differentKind, err)
	}
}

func TestPluginInstallationStatusTracksMarketplaceSourceAndLatestRevision(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, sourceRevisionV1 := createPluginServiceSystemSkill(
		t,
		database,
		"status-skill",
		"Version one.",
	)
	item := &types.PluginMarketplaceItem{
		PublicID: "mkt_status", PluginID: sourcePlugin.ID, Kind: "skill",
		Code: "status-skill", Name: "Status Skill", Author: "Lework",
		SourceType: "builtin", SourceRef: "status-skill", Status: "published",
		Tags: types.PluginStringList{}, PublishedAt: time.Now(),
	}
	if err := database.Create(item).Error; err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}
	service := &pluginService{db: database}
	request := &contract.GetPluginInstallationStatusRequest{
		Kind: "skill",
		Code: "status-skill",
	}

	notInstalled, err := service.GetPluginInstallationStatus(ctx, 7, 9, request)
	if err != nil || notInstalled.Installed || notInstalled.MarketplaceBased ||
		!notInstalled.MarketplaceAvailable ||
		notInstalled.LatestMarketplaceVersion != "1" ||
		notInstalled.UpdateAvailable {
		t.Fatalf("not installed status = %#v, %v", notInstalled, err)
	}

	localPlugin := &types.Plugin{
		PublicID: "plugin_local_status", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 8, Code: "status-skill", Kind: "skill", Name: "Local Status Skill",
		Status: types.PluginStatusActive, Origin: "org", CurrentRevision: 1,
		CreatedBy: 8, UpdatedBy: 8,
	}
	if err := database.Create(localPlugin).Error; err != nil {
		t.Fatalf("create local plugin: %v", err)
	}
	if err := database.Create(&types.PluginRevision{
		PluginID: localPlugin.ID, Revision: 1, Status: "published",
		Definition: sourceRevisionV1.Definition, PublishedByType: "user",
		PublishedByID: 8, PublishedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create local revision: %v", err)
	}
	localStatus, err := service.GetPluginInstallationStatus(ctx, 8, 9, request)
	if err != nil || !localStatus.Installed || localStatus.CurrentVersion != "1" ||
		localStatus.MarketplaceBased || !localStatus.MarketplaceAvailable ||
		localStatus.UpdateAvailable {
		t.Fatalf("local plugin status = %#v, %v", localStatus, err)
	}

	if _, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID); err != nil {
		t.Fatalf("install official plugin: %v", err)
	}
	currentStatus, err := service.GetPluginInstallationStatus(ctx, 7, 9, request)
	if err != nil || !currentStatus.Installed || !currentStatus.MarketplaceBased ||
		currentStatus.InstalledMarketplaceVersion != "1" ||
		currentStatus.LatestMarketplaceVersion != "1" ||
		currentStatus.UpdateAvailable {
		t.Fatalf("current marketplace status = %#v, %v", currentStatus, err)
	}

	_, sourceRevisionV2 := addPluginServiceSystemRevision(
		t,
		database,
		sourcePlugin,
		2,
		"Version two.",
	)
	var installedPlugin types.Plugin
	if err := database.Where(
		"owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
		types.OwnerScopeOrganization,
		7,
		"skill",
		"status-skill",
	).First(&installedPlugin).Error; err != nil {
		t.Fatalf("load installed plugin: %v", err)
	}
	sourceItemID, sourceRevisionID := item.ID, sourceRevisionV1.ID
	if err := database.Create(&types.PluginRevision{
		PluginID: installedPlugin.ID, Revision: 5, Status: "published",
		Definition: sourceRevisionV1.Definition, SourceMarketplaceItemID: &sourceItemID,
		SourcePluginRevisionID: &sourceRevisionID, PublishedByType: "user",
		PublishedByID: 9, PublishedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create organization revision 5: %v", err)
	}
	if err := database.Model(&installedPlugin).
		Update("current_revision", 5).Error; err != nil {
		t.Fatalf("set organization current revision: %v", err)
	}

	updateStatus, err := service.GetPluginInstallationStatus(ctx, 7, 9, request)
	if err != nil || updateStatus.CurrentVersion != "5" ||
		updateStatus.InstalledMarketplaceVersion != "1" ||
		updateStatus.LatestMarketplaceVersion != "2" ||
		!updateStatus.UpdateAvailable {
		t.Fatalf("available update status = %#v, %v", updateStatus, err)
	}

	updated, err := service.InstallOfficialPlugin(ctx, 7, 9, item.PublicID)
	if err != nil || updated.Operation != "updated" ||
		updated.Plugin.CurrentRevision != 6 {
		t.Fatalf("update official plugin = %#v, %v", updated, err)
	}
	updatedStatus, err := service.GetPluginInstallationStatus(ctx, 7, 9, request)
	if err != nil || updatedStatus.CurrentVersion != "6" ||
		updatedStatus.InstalledMarketplaceVersion != "2" ||
		updatedStatus.LatestMarketplaceVersion != "2" ||
		updatedStatus.UpdateAvailable {
		t.Fatalf("updated marketplace status = %#v, %v", updatedStatus, err)
	}
	if sourceRevisionV2.Revision != 2 {
		t.Fatalf("source revision = %d, want 2", sourceRevisionV2.Revision)
	}

	if err := database.Model(item).Update("status", "archived").Error; err != nil {
		t.Fatalf("archive marketplace item: %v", err)
	}
	archivedStatus, err := service.GetPluginInstallationStatus(ctx, 7, 9, request)
	if err != nil || !archivedStatus.MarketplaceBased ||
		archivedStatus.InstalledMarketplaceVersion != "2" ||
		archivedStatus.MarketplaceAvailable ||
		archivedStatus.LatestMarketplaceVersion != "" ||
		archivedStatus.UpdateAvailable {
		t.Fatalf("archived marketplace status = %#v, %v", archivedStatus, err)
	}

	otherOrg, err := service.GetPluginInstallationStatus(ctx, 9, 9, request)
	if err != nil || otherOrg.Installed || otherOrg.MarketplaceAvailable {
		t.Fatalf("other organization status = %#v, %v", otherOrg, err)
	}
}

func TestPluginInstallationStatusRejectsIncompleteMarketplaceLineage(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	ctx := context.Background()
	plugin := &types.Plugin{
		PublicID: "plugin_incomplete_lineage", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 7, Code: "incomplete-lineage", Kind: "skill", Name: "Incomplete",
		Status: types.PluginStatusActive, Origin: "marketplace", CurrentRevision: 1,
		CreatedBy: 9, UpdatedBy: 9,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	sourceItemID := uint(99)
	if err := database.Create(&types.PluginRevision{
		PluginID: plugin.ID, Revision: 1, Status: "published",
		Definition:              []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_missing","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`),
		SourceMarketplaceItemID: &sourceItemID, PublishedByType: "user",
		PublishedByID: 9, PublishedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create revision: %v", err)
	}
	_, err := (&pluginService{db: database}).GetPluginInstallationStatus(
		ctx,
		7,
		9,
		&contract.GetPluginInstallationStatusRequest{
			Kind: "skill",
			Code: "incomplete-lineage",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "lineage is incomplete") {
		t.Fatalf("incomplete lineage error = %v", err)
	}

	sourcePlugin := &types.Plugin{
		PublicID: "plugin_lineage_source", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "invalid-lineage", Kind: "skill", Name: "Source",
		Status: types.PluginStatusActive, Origin: "builtin",
	}
	if err := database.Create(sourcePlugin).Error; err != nil {
		t.Fatalf("create source plugin: %v", err)
	}
	sourceItem := &types.PluginMarketplaceItem{
		PublicID: "mkt_invalid_lineage", PluginID: sourcePlugin.ID, Kind: "skill",
		Code: "invalid-lineage", Name: "Invalid Lineage", Author: "Lework",
		SourceType: "builtin", SourceRef: "invalid-lineage", Status: "archived",
		Tags: types.PluginStringList{}, PublishedAt: time.Now(),
	}
	if err := database.Create(sourceItem).Error; err != nil {
		t.Fatalf("create source item: %v", err)
	}
	otherSource := &types.Plugin{
		PublicID: "plugin_other_lineage_source", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, Code: "other-lineage", Kind: "skill", Name: "Other Source",
		Status: types.PluginStatusActive, Origin: "builtin",
	}
	if err := database.Create(otherSource).Error; err != nil {
		t.Fatalf("create other source plugin: %v", err)
	}
	otherRevision := &types.PluginRevision{
		PluginID: otherSource.ID, Revision: 1, Status: "published",
		Definition:      []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_other","sha256":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}`),
		PublishedByType: "system", PublishedAt: time.Now(),
	}
	if err := database.Create(otherRevision).Error; err != nil {
		t.Fatalf("create other source revision: %v", err)
	}
	invalidPlugin := &types.Plugin{
		PublicID: "plugin_invalid_lineage", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 7, Code: "invalid-lineage", Kind: "skill", Name: "Invalid",
		Status: types.PluginStatusActive, Origin: "marketplace", CurrentRevision: 1,
		CreatedBy: 9, UpdatedBy: 9,
	}
	if err := database.Create(invalidPlugin).Error; err != nil {
		t.Fatalf("create invalid lineage plugin: %v", err)
	}
	invalidSourceItemID, invalidSourceRevisionID := sourceItem.ID, otherRevision.ID
	if err := database.Create(&types.PluginRevision{
		PluginID: invalidPlugin.ID, Revision: 1, Status: "published",
		Definition:              otherRevision.Definition,
		SourceMarketplaceItemID: &invalidSourceItemID,
		SourcePluginRevisionID:  &invalidSourceRevisionID,
		PublishedByType:         "user",
		PublishedByID:           9,
		PublishedAt:             time.Now(),
	}).Error; err != nil {
		t.Fatalf("create invalid lineage revision: %v", err)
	}
	_, err = (&pluginService{db: database}).GetPluginInstallationStatus(
		ctx,
		7,
		9,
		&contract.GetPluginInstallationStatusRequest{
			Kind: "skill",
			Code: "invalid-lineage",
		},
	)
	if err == nil || !strings.Contains(err.Error(), "lineage is invalid") {
		t.Fatalf("invalid lineage error = %v", err)
	}
}

func TestOfficialMarketplaceArtifactDownloadsWithoutCopyingFileUpload(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	sourceFile, sourcePlugin, _ := createPluginServiceSystemSkill(
		t, database, "market-skill", "Marketplace Skill.",
	)
	item := &types.PluginMarketplaceItem{
		PublicID:    "mkt_download",
		PluginID:    sourcePlugin.ID,
		Kind:        "skill",
		Code:        "market-skill",
		Name:        "Market Skill",
		Author:      "Lework",
		SourceType:  "builtin",
		SourceRef:   "market-skill",
		Status:      "published",
		Tags:        types.PluginStringList{},
		PublishedAt: time.Now(),
	}
	if err := database.Create(item).Error; err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}
	service := &pluginService{db: database}
	if _, err := service.InstallOfficialPlugin(ctx, 7, 8, item.PublicID); err != nil {
		t.Fatalf("install marketplace Skill: %v", err)
	}
	_, sourceRevisionV2 := addPluginServiceSystemRevision(
		t, database, sourcePlugin, 2, "Marketplace Skill v2.",
	)
	if sourceRevisionV2.Revision != 2 {
		t.Fatalf("source revision 2 = %#v", sourceRevisionV2)
	}

	var before int64
	if err := database.Model(&types.FileUpload{}).Count(&before).Error; err != nil {
		t.Fatalf("count file uploads: %v", err)
	}
	resolved, err := service.ResolveSkillDownloadURLs(ctx, 7, types.CallerKindUser, 1, &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{item.Code},
	})
	if err != nil || len(resolved.Skills) != 1 || resolved.Skills[0].DownloadURL == "" {
		t.Fatalf("resolve installed marketplace Skill = %#v, %v", resolved, err)
	}
	if resolved.Skills[0].SHA256 != sourceFile.Sha256 {
		t.Fatalf("installed old revision changed after official update: %#v", resolved.Skills[0])
	}
	var after int64
	if err := database.Model(&types.FileUpload{}).Count(&after).Error; err != nil {
		t.Fatalf("count file uploads after resolve: %v", err)
	}
	if after != before {
		t.Fatalf("file upload count changed from %d to %d", before, after)
	}

	if err := database.Model(item).Update("status", "archived").Error; err != nil {
		t.Fatalf("archive marketplace item: %v", err)
	}
	resolved, err = service.ResolveSkillDownloadURLs(ctx, 7, types.CallerKindUser, 1, &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{item.Code},
	})
	if err != nil || len(resolved.Skills) != 1 {
		t.Fatalf("resolve installed archived marketplace Skill = %#v, %v", resolved, err)
	}
	if err := database.Model(sourceFile).Update("status", "inactive").Error; err != nil {
		t.Fatalf("deactivate marketplace file: %v", err)
	}
	resolved, err = service.ResolveSkillDownloadURLs(ctx, 7, types.CallerKindUser, 1, &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{item.Code},
	})
	if err != nil || len(resolved.Skills) != 0 {
		t.Fatalf("inactive marketplace file resolution = %#v, %v", resolved, err)
	}
	if err := database.Model(sourceFile).Updates(map[string]interface{}{
		"status": "active",
		"sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
	}).Error; err != nil {
		t.Fatalf("change marketplace file hash: %v", err)
	}
	resolved, err = service.ResolveSkillDownloadURLs(ctx, 7, types.CallerKindUser, 1, &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{item.Code},
	})
	if err != nil || len(resolved.Skills) != 0 {
		t.Fatalf("mismatched marketplace file resolution = %#v, %v", resolved, err)
	}
	otherOrg, err := service.ResolveSkillDownloadURLs(ctx, 8, types.CallerKindUser, 1, &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{item.Code},
	})
	if err != nil || len(otherOrg.Skills) != 0 {
		t.Fatalf("cross-org marketplace resolution = %#v, %v", otherOrg, err)
	}
}

func TestOrganizationSkillDownloadResolvesCurrentRevisionByCode(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	upload := func(name, content string) *types.FileUpload {
		t.Helper()
		file, err := filestore.Upload(ctx, database, filestore.UploadParams{
			Data:         []byte(content),
			Filename:     name + ".zip",
			OriginalName: name + ".zip",
			MimeType:     "application/zip",
			OrgID:        7,
			OwnerID:      8,
			ObjectKey:    "organization/" + name + ".zip",
			Purpose:      filestore.PurposeArtifact,
		})
		if err != nil {
			t.Fatalf("upload %s: %v", name, err)
		}
		return file
	}
	first := upload("first", "first revision")
	second := upload("second", "second revision")
	definition := func(file *types.FileUpload) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(
			`{"schema":"skill/v1","artifact":{"file_upload_id":%q,"sha256":%q,"size_bytes":%d,"content_type":"application/zip"}}`,
			file.PublicID,
			file.Sha256,
			file.FileSize,
		))
	}
	plugin := &types.Plugin{
		PublicID:        "plugin_current_download",
		OrgID:           7,
		Code:            "current-download",
		Kind:            "skill",
		Name:            "Current Download",
		Status:          types.PluginStatusActive,
		Origin:          "org",
		CurrentRevision: 2,
		CreatedBy:       8,
		UpdatedBy:       8,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create plugin: %v", err)
	}
	for revision, file := range map[int]*types.FileUpload{1: first, 2: second} {
		if err := database.Create(&types.PluginRevision{
			PluginID:        plugin.ID,
			Revision:        revision,
			Status:          "published",
			Definition:      definition(file),
			PublishedByType: "user",
			PublishedByID:   8,
			PublishedAt:     time.Now(),
		}).Error; err != nil {
			t.Fatalf("create revision %d: %v", revision, err)
		}
	}
	resolved, err := (&pluginService{db: database}).ResolveSkillDownloadURLs(
		ctx,
		7, types.CallerKindUser, 1,
		&contract.ResolveSkillDownloadURLsRequest{SkillCodes: []string{plugin.Code}},
	)
	if err != nil || len(resolved.Skills) != 1 {
		t.Fatalf("resolve organization Skill = %#v, %v", resolved, err)
	}
	if resolved.Skills[0].Revision != 2 || resolved.Skills[0].SHA256 != second.Sha256 {
		t.Fatalf("resolved current Skill = %#v", resolved.Skills[0])
	}
}

func TestSkillDownloadURLsAutoInstallsMarketplaceSkillOnlyForWorker(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	sourceFile, sourcePlugin, sourceRevision := createPluginServiceSystemSkill(
		t,
		database,
		"worker-auto",
		"Worker auto install.",
	)
	item := createPluginServiceMarketplaceItem(
		t,
		database,
		sourcePlugin,
		"mkt_worker_auto",
	)
	service := &pluginService{db: database}
	request := &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{item.Code},
	}

	userResult, err := service.ResolveSkillDownloadURLs(
		ctx,
		7,
		types.CallerKindUser,
		8,
		request,
	)
	if err != nil || len(userResult.Skills) != 0 {
		t.Fatalf("user missing Skill resolution = %#v, %v", userResult, err)
	}
	userPlugin, err := infradb.GetOrganizationPluginByIdentity(
		ctx,
		database,
		7,
		"skill",
		item.Code,
	)
	if err != nil || userPlugin != nil {
		t.Fatalf("user request must not install Skill = %#v, %v", userPlugin, err)
	}

	workerResult, err := service.ResolveSkillDownloadURLs(
		ctx,
		7,
		types.CallerKindWorker,
		19,
		request,
	)
	if err != nil || len(workerResult.Skills) != 1 {
		t.Fatalf("worker auto-install resolution = %#v, %v", workerResult, err)
	}
	if workerResult.Skills[0].Code != item.Code ||
		workerResult.Skills[0].SHA256 != sourceFile.Sha256 ||
		workerResult.Skills[0].DownloadURL == "" {
		t.Fatalf("worker auto-install download = %#v", workerResult.Skills[0])
	}
	plugin, err := infradb.GetOrganizationPluginByIdentity(
		ctx,
		database,
		7,
		"skill",
		item.Code,
	)
	if err != nil || plugin == nil {
		t.Fatalf("load auto-installed Skill = %#v, %v", plugin, err)
	}
	if plugin.Origin != "marketplace" || plugin.CreatedBy != 19 ||
		plugin.UpdatedBy != 19 || plugin.CurrentRevision != 1 {
		t.Fatalf("auto-installed Skill identity = %#v", plugin)
	}
	revision, err := infradb.GetCurrentPluginRevision(ctx, database, plugin)
	if err != nil || revision == nil {
		t.Fatalf("load auto-installed revision = %#v, %v", revision, err)
	}
	if revision.PublishedByType != "worker" || revision.PublishedByID != 19 ||
		revision.SourceMarketplaceItemID == nil ||
		*revision.SourceMarketplaceItemID != item.ID ||
		revision.SourcePluginRevisionID == nil ||
		*revision.SourcePluginRevisionID != sourceRevision.ID {
		t.Fatalf("auto-installed revision audit = %#v", revision)
	}
	sourceContent, err := infradb.GetPluginRevisionContent(ctx, database, sourceRevision.ID)
	if err != nil || sourceContent == nil {
		t.Fatalf("load source content = %#v, %v", sourceContent, err)
	}
	installedContent, err := infradb.GetPluginRevisionContent(ctx, database, revision.ID)
	if err != nil || installedContent == nil {
		t.Fatalf("load installed content = %#v, %v", installedContent, err)
	}
	if installedContent.ArtifactSHA256 != sourceContent.ArtifactSHA256 ||
		installedContent.EntrypointContent != sourceContent.EntrypointContent ||
		len(installedContent.FileIndex) != len(sourceContent.FileIndex) {
		t.Fatalf("installed content = %#v, source = %#v", installedContent, sourceContent)
	}
	var bindingCount int64
	if err := database.Model(&types.ProjectPluginBinding{}).
		Where("plugin_id = ?", plugin.ID).
		Count(&bindingCount).Error; err != nil {
		t.Fatalf("count auto-install project bindings: %v", err)
	}
	if bindingCount != 0 {
		t.Fatalf("auto-install project bindings = %d, want 0", bindingCount)
	}
	mySkills, err := service.ListPlugins(ctx, 7, 9, &contract.ListPluginsRequest{
		Kind:                    "skill",
		Status:                  types.PluginStatusActive,
		ExcludeMarketplaceBased: true,
	})
	if err != nil || len(mySkills.Plugins) != 0 {
		t.Fatalf("Worker-installed Skill must be hidden from My Skills = %#v, %v", mySkills, err)
	}
}

func TestSkillDownloadURLsPreservesOrganizationSkillsAndSortsMixedResults(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()

	_, marketPlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"a-market",
		"Marketplace Skill.",
	)
	createPluginServiceMarketplaceItem(t, database, marketPlugin, "mkt_a_market")

	_, existingMarketPlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"z-existing",
		"Marketplace replacement.",
	)
	createPluginServiceMarketplaceItem(
		t,
		database,
		existingMarketPlugin,
		"mkt_z_existing",
	)
	existingFile, existingPlugin, _ := createPluginServiceOrganizationSkill(
		t,
		database,
		7,
		8,
		"z-existing",
		"Organization version.",
	)

	_, invalidMarketPlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"b-invalid",
		"Marketplace fallback.",
	)
	createPluginServiceMarketplaceItem(
		t,
		database,
		invalidMarketPlugin,
		"mkt_b_invalid",
	)
	_, invalidPlugin, invalidRevision := createPluginServiceOrganizationSkill(
		t,
		database,
		7,
		8,
		"b-invalid",
		"Invalid organization artifact.",
	)
	if err := database.Model(invalidRevision).Update(
		"definition",
		json.RawMessage(`{"schema":"skill/v1","artifact":{"file_upload_id":"missing","sha256":"invalid"}}`),
	).Error; err != nil {
		t.Fatalf("invalidate organization revision: %v", err)
	}

	resolved, err := (&pluginService{db: database}).ResolveSkillDownloadURLs(
		ctx,
		7,
		types.CallerKindWorker,
		19,
		&contract.ResolveSkillDownloadURLsRequest{
			SkillCodes: []string{"z-existing", "missing", "b-invalid", "a-market"},
		},
	)
	if err != nil || len(resolved.Skills) != 2 {
		t.Fatalf("mixed Skill resolution = %#v, %v", resolved, err)
	}
	if resolved.Skills[0].Code != "a-market" ||
		resolved.Skills[1].Code != "z-existing" ||
		resolved.Skills[1].SHA256 != existingFile.Sha256 {
		t.Fatalf("sorted mixed Skills = %#v", resolved.Skills)
	}
	var reloadedExisting types.Plugin
	if err := database.First(&reloadedExisting, existingPlugin.ID).Error; err != nil {
		t.Fatalf("reload existing organization Skill: %v", err)
	}
	if reloadedExisting.Origin != "org" || reloadedExisting.CurrentRevision != 1 {
		t.Fatalf("existing organization Skill was replaced: %#v", reloadedExisting)
	}
	var existingRevisionCount int64
	if err := database.Model(&types.PluginRevision{}).
		Where("plugin_id = ?", existingPlugin.ID).
		Count(&existingRevisionCount).Error; err != nil {
		t.Fatalf("count existing revisions: %v", err)
	}
	if existingRevisionCount != 1 {
		t.Fatalf("existing revision count = %d, want 1", existingRevisionCount)
	}
	var reloadedInvalid types.Plugin
	if err := database.First(&reloadedInvalid, invalidPlugin.ID).Error; err != nil {
		t.Fatalf("reload invalid organization Skill: %v", err)
	}
	if reloadedInvalid.Origin != "org" || reloadedInvalid.CurrentRevision != 1 {
		t.Fatalf("invalid organization Skill was replaced: %#v", reloadedInvalid)
	}
}

func TestSkillDownloadURLsAutoInstallRestoresSoftDeletedSkill(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"restore-auto",
		"Restore automatic install.",
	)
	createPluginServiceMarketplaceItem(
		t,
		database,
		sourcePlugin,
		"mkt_restore_auto",
	)
	service := &pluginService{db: database}
	request := &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{sourcePlugin.Code},
	}
	if result, err := service.ResolveSkillDownloadURLs(
		ctx,
		7,
		types.CallerKindWorker,
		19,
		request,
	); err != nil || len(result.Skills) != 1 {
		t.Fatalf("initial auto-install = %#v, %v", result, err)
	}
	plugin, err := infradb.GetOrganizationPluginByIdentity(
		ctx,
		database,
		7,
		"skill",
		sourcePlugin.Code,
	)
	if err != nil || plugin == nil {
		t.Fatalf("load initial auto-install = %#v, %v", plugin, err)
	}
	if err := database.Delete(plugin).Error; err != nil {
		t.Fatalf("soft delete auto-installed Skill: %v", err)
	}
	if result, err := service.ResolveSkillDownloadURLs(
		ctx,
		7,
		types.CallerKindWorker,
		20,
		request,
	); err != nil || len(result.Skills) != 1 {
		t.Fatalf("restored auto-install = %#v, %v", result, err)
	}
	restored, err := infradb.GetOrganizationPluginByIdentity(
		ctx,
		database,
		7,
		"skill",
		sourcePlugin.Code,
	)
	if err != nil || restored == nil {
		t.Fatalf("load restored auto-install = %#v, %v", restored, err)
	}
	if restored.ID != plugin.ID || restored.CurrentRevision != 2 ||
		restored.UpdatedBy != 20 {
		t.Fatalf("restored auto-install identity = %#v", restored)
	}
}

func TestSkillDownloadURLsConcurrentAutoInstallRemainsUnique(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	_, sourcePlugin, _ := createPluginServiceSystemSkill(
		t,
		database,
		"concurrent-auto",
		"Concurrent automatic install.",
	)
	createPluginServiceMarketplaceItem(
		t,
		database,
		sourcePlugin,
		"mkt_concurrent_auto",
	)
	service := &pluginService{db: database}
	request := &contract.ResolveSkillDownloadURLsRequest{
		SkillCodes: []string{sourcePlugin.Code},
	}

	type resolveResult struct {
		response *contract.ResolveSkillDownloadURLsResponse
		err      error
	}
	start := make(chan struct{})
	results := make(chan resolveResult, 2)
	var wait sync.WaitGroup
	for _, workerID := range []uint{19, 20} {
		wait.Add(1)
		go func(id uint) {
			defer wait.Done()
			<-start
			response, err := service.ResolveSkillDownloadURLs(
				context.Background(),
				7,
				types.CallerKindWorker,
				id,
				request,
			)
			results <- resolveResult{response: response, err: err}
		}(workerID)
	}
	close(start)
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil || result.response == nil ||
			len(result.response.Skills) != 1 {
			t.Fatalf("concurrent auto-install = %#v, %v", result.response, result.err)
		}
	}
	var pluginCount int64
	if err := database.Unscoped().Model(&types.Plugin{}).
		Where(
			"owner_scope = ? AND org_id = ? AND kind = ? AND code = ?",
			types.OwnerScopeOrganization,
			7,
			"skill",
			sourcePlugin.Code,
		).
		Count(&pluginCount).Error; err != nil {
		t.Fatalf("count concurrent organization Skills: %v", err)
	}
	if pluginCount != 1 {
		t.Fatalf("concurrent organization Skill count = %d, want 1", pluginCount)
	}
	var revisionCount int64
	if err := database.Model(&types.PluginRevision{}).
		Joins(
			"JOIN "+types.TableNamePlugin+" AS p ON p.id = "+
				types.TableNamePluginRevision+".plugin_id",
		).
		Where(
			"p.owner_scope = ? AND p.org_id = ? AND p.kind = ? AND p.code = ?",
			types.OwnerScopeOrganization,
			7,
			"skill",
			sourcePlugin.Code,
		).
		Count(&revisionCount).Error; err != nil {
		t.Fatalf("count concurrent revisions: %v", err)
	}
	if revisionCount != 1 {
		t.Fatalf("concurrent revision count = %d, want 1", revisionCount)
	}
}

func TestOfficialMarketplaceInstallOverwritesSameCodeAndKeepsBindings(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	plugin := &types.Plugin{
		PublicID:        "plugin_existing",
		OrgID:           7,
		Code:            "same-code",
		Kind:            "skill",
		Name:            "Organization Skill",
		Status:          types.PluginStatusActive,
		Origin:          "org",
		CurrentRevision: 1,
		CreatedBy:       8,
		UpdatedBy:       8,
	}
	if err := database.Create(plugin).Error; err != nil {
		t.Fatalf("create organization plugin: %v", err)
	}
	if err := database.Create(&types.PluginRevision{
		PluginID:        plugin.ID,
		Revision:        1,
		Status:          "published",
		Definition:      []byte(`{"schema":"skill/v1","artifact":{"file_upload_id":"file_org","sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}`),
		PublishedByType: "user",
		PublishedByID:   8,
		PublishedAt:     time.Now(),
	}).Error; err != nil {
		t.Fatalf("create organization revision: %v", err)
	}
	project := &types.Project{PublicID: "project_existing", OrgID: 7, OwnerID: 8, Name: "Project"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	binding := &types.ProjectPluginBinding{
		ProjectID: project.ID,
		PluginID:  plugin.ID,
		Enabled:   true,
		Config:    []byte(`{}`),
		CreatedBy: 8,
		UpdatedBy: 8,
	}
	if err := database.Create(binding).Error; err != nil {
		t.Fatalf("create project binding: %v", err)
	}
	_, sourcePlugin, _ := createPluginServiceSystemSkill(
		t, database, plugin.Code, "Marketplace content.",
	)
	item := &types.PluginMarketplaceItem{
		PublicID:    "mkt_same_code",
		PluginID:    sourcePlugin.ID,
		Kind:        "skill",
		Code:        plugin.Code,
		Name:        "Marketplace Skill",
		Author:      "Lework",
		SourceType:  "builtin",
		SourceRef:   plugin.Code,
		Status:      "published",
		Tags:        types.PluginStringList{},
		PublishedAt: time.Now(),
	}
	if err := database.Create(item).Error; err != nil {
		t.Fatalf("create marketplace item: %v", err)
	}

	result, err := (&pluginService{db: database}).InstallOfficialPlugin(ctx, 7, 9, item.PublicID)
	if err != nil || result.Operation != "updated" {
		t.Fatalf("overwrite same-code plugin = %#v, %v", result, err)
	}
	var got types.Plugin
	if err := database.First(&got, plugin.ID).Error; err != nil {
		t.Fatalf("reload overwritten plugin: %v", err)
	}
	if got.PublicID != plugin.PublicID || got.Origin != "marketplace" || got.CurrentRevision != 2 {
		t.Fatalf("overwritten plugin = %#v", got)
	}
	var bindingCount int64
	if err := database.Model(&types.ProjectPluginBinding{}).
		Where("id = ? AND plugin_id = ? AND deleted_at IS NULL", binding.ID, plugin.ID).
		Count(&bindingCount).Error; err != nil {
		t.Fatalf("count preserved binding: %v", err)
	}
	if bindingCount != 1 {
		t.Fatalf("binding count = %d, want 1", bindingCount)
	}
}
