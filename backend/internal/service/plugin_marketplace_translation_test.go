package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	infradb "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/internal/skill/catalog"
	"github.com/insmtx/Leros/backend/types"
)

type fakeSkillDescriptionTranslator struct {
	metadataCalls int
	documentCalls int
}

func (f *fakeSkillDescriptionTranslator) Translate(
	_ context.Context,
	_ uint,
	items []TranslateItem,
) (map[string]TranslatedSkillText, error) {
	f.metadataCalls++
	result := make(map[string]TranslatedSkillText, len(items))
	for _, item := range items {
		result[item.SkillID] = TranslatedSkillText{
			DisplayName: "中文技能",
			Description: "中文展示描述",
		}
	}
	return result, nil
}

func (f *fakeSkillDescriptionTranslator) TranslateDocument(
	_ context.Context,
	_ uint,
	items []TranslateDocumentItem,
) (map[string]string, error) {
	f.documentCalls++
	result := make(map[string]string, len(items))
	for _, item := range items {
		manifest, _, err := catalog.ParseDocument([]byte(item.Content))
		if err != nil {
			return nil, err
		}
		result[item.SkillID] = fmt.Sprintf(
			"---\nname: %s\ndescription: 中文展示描述\n---\n\n中文展示正文",
			manifest.Name,
		)
	}
	return result, nil
}

func TestSkillDisplayTranslationServiceIsSharedAcrossSkillViews(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	shared := newSkillDisplayTranslationService(database, &fakeSkillDescriptionTranslator{})

	plugin := NewPluginService(database, shared).(*pluginService)
	marketplace := NewOfficialPluginMarketplaceService(database, shared).(*pluginService)
	project := NewProjectService(
		database,
		newTestPermissionService(database),
		nil,
		nil,
		"test",
		nil,
		shared,
	).(*projectService)

	if plugin.displayTranslation != shared || marketplace.displayTranslation != shared || project.displayTranslation != shared {
		t.Fatalf("Skill display translation service was not shared: plugin=%p marketplace=%p project=%p shared=%p",
			plugin.displayTranslation, marketplace.displayTranslation, project.displayTranslation, shared)
	}
}

func TestOfficialMarketplaceTranslationCachesByOrganizationAndSourceRevision(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, sourceRevision := createPluginServiceSystemSkill(t, database, "translate-cache", "English body.")
	item := createPluginServiceMarketplaceItem(t, database, sourcePlugin, "mkt_translate_cache")
	item.Name = "English Skill"
	item.Description = "An English description for display."
	if err := database.Save(item).Error; err != nil {
		t.Fatalf("save marketplace metadata: %v", err)
	}

	translator := &fakeSkillDescriptionTranslator{}
	service := NewOfficialPluginMarketplaceService(database, newSkillDisplayTranslationService(database, translator))
	list, err := service.ListOfficialPluginMarketplaceItems(ctx, 7, nil)
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("list translated marketplace = %#v, %v", list, err)
	}
	if list.Items[0].DisplayName != "中文技能" || list.Items[0].Description != "中文展示描述" {
		t.Fatalf("translated marketplace view = %#v", list.Items[0])
	}
	if translator.metadataCalls != 1 {
		t.Fatalf("metadata calls after first list = %d, want 1", translator.metadataCalls)
	}

	if _, err := service.ListOfficialPluginMarketplaceItems(ctx, 7, nil); err != nil {
		t.Fatalf("list cached marketplace: %v", err)
	}
	if translator.metadataCalls != 1 {
		t.Fatalf("metadata calls after cache hit = %d, want 1", translator.metadataCalls)
	}

	_, secondRevision := addPluginServiceSystemRevision(t, database, sourcePlugin, 2, "Second English body.")
	if _, err := service.ListOfficialPluginMarketplaceItems(ctx, 7, nil); err != nil {
		t.Fatalf("list new marketplace revision: %v", err)
	}
	if translator.metadataCalls != 2 {
		t.Fatalf("metadata calls after new revision = %d, want 2", translator.metadataCalls)
	}

	rows, err := infradb.ListPluginTranslations(ctx, database, 7, types.PluginTranslationSourceMarketplace, []uint{item.ID}, translationLocale)
	if err != nil || len(rows) != 2 {
		t.Fatalf("versioned translation rows = %#v, %v", rows, err)
	}
	if !containsTranslationRevision(rows, sourceRevision.ID) || !containsTranslationRevision(rows, secondRevision.ID) {
		t.Fatalf("translation rows do not retain both revisions: %#v", rows)
	}
	if _, err := service.ListOfficialPluginMarketplaceItems(ctx, 8, nil); err != nil {
		t.Fatalf("list other organization marketplace: %v", err)
	}
	if translator.metadataCalls != 3 {
		t.Fatalf("metadata calls after other organization = %d, want 3", translator.metadataCalls)
	}
}

func TestOfficialMarketplaceDetailTranslationCachesDocument(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, sourcePlugin, _ := createPluginServiceSystemSkill(t, database, "translate-detail", "English body.")
	item := createPluginServiceMarketplaceItem(t, database, sourcePlugin, "mkt_translate_detail")
	item.Name = "English Detail"
	item.Description = "English detail description."
	if err := database.Save(item).Error; err != nil {
		t.Fatalf("save marketplace detail metadata: %v", err)
	}

	translator := &fakeSkillDescriptionTranslator{}
	service := NewOfficialPluginMarketplaceService(database, newSkillDisplayTranslationService(database, translator))
	detail, err := service.GetOfficialPluginMarketplaceItem(ctx, 7, item.PublicID)
	if err != nil {
		t.Fatalf("get translated marketplace detail: %v", err)
	}
	if detail.DisplayName != "中文技能" || detail.Description != "中文展示描述" ||
		detail.Content == nil || detail.Content.SkillMD != "中文展示正文" {
		t.Fatalf("translated marketplace detail = %#v", detail)
	}
	if translator.metadataCalls != 1 || translator.documentCalls != 1 {
		t.Fatalf("translation calls after first detail = metadata %d, document %d", translator.metadataCalls, translator.documentCalls)
	}

	detail, err = service.GetOfficialPluginMarketplaceItem(ctx, 7, item.PublicID)
	if err != nil || detail.Content == nil || detail.Content.SkillMD != "中文展示正文" {
		t.Fatalf("cached marketplace detail = %#v, %v", detail, err)
	}
	if translator.metadataCalls != 1 || translator.documentCalls != 1 {
		t.Fatalf("translation calls after detail cache hit = metadata %d, document %d", translator.metadataCalls, translator.documentCalls)
	}
}

func TestOrganizationSkillTranslationCachesByRevisionAndSourceType(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := context.Background()
	_, plugin, revision := createPluginServiceOrganizationSkill(t, database, 7, 9, "org-translate", "English body.")
	plugin.Name = "English Organization Skill"
	plugin.Description = "An English organization Skill description."
	if err := database.Save(plugin).Error; err != nil {
		t.Fatalf("save organization Skill metadata: %v", err)
	}
	translator := &fakeSkillDescriptionTranslator{}
	service := NewPluginService(database, newSkillDisplayTranslationService(database, translator))
	list, err := service.ListPlugins(ctx, 7, 9, &contract.ListPluginsRequest{Kind: "skill", Status: types.PluginStatusActive})
	if err != nil || len(list.Plugins) != 1 {
		t.Fatalf("list translated organization Skills = %#v, %v", list, err)
	}
	if list.Plugins[0].DisplayName != "中文技能" || list.Plugins[0].Description != "中文展示描述" {
		t.Fatalf("translated organization Skill list view = %#v", list.Plugins[0])
	}
	if translator.metadataCalls != 1 {
		t.Fatalf("organization metadata calls after first list = %d, want 1", translator.metadataCalls)
	}

	if _, err := service.ListPlugins(ctx, 7, 9, &contract.ListPluginsRequest{Kind: "skill", Status: types.PluginStatusActive}); err != nil {
		t.Fatalf("list cached organization Skills: %v", err)
	}
	if translator.metadataCalls != 1 {
		t.Fatalf("organization metadata calls after cache hit = %d, want 1", translator.metadataCalls)
	}

	plugin.Description = "A changed English organization Skill description."
	if err := database.Save(plugin).Error; err != nil {
		t.Fatalf("update organization Skill description: %v", err)
	}
	if _, err := service.ListPlugins(ctx, 7, 9, &contract.ListPluginsRequest{Kind: "skill", Status: types.PluginStatusActive}); err != nil {
		t.Fatalf("list organization Skill after metadata change: %v", err)
	}
	if translator.metadataCalls != 2 {
		t.Fatalf("organization metadata calls after same-revision metadata change = %d, want 2", translator.metadataCalls)
	}

	detail, err := service.GetPlugin(ctx, 7, 9, plugin.PublicID, nil)
	if err != nil {
		t.Fatalf("get translated organization Skill: %v", err)
	}
	if detail.Plugin == nil || detail.Plugin.DisplayName != "中文技能" || detail.Plugin.Description != "中文展示描述" ||
		detail.Content == nil || detail.Content.SkillMD != "中文展示正文" {
		t.Fatalf("translated organization Skill detail = %#v", detail)
	}
	if translator.metadataCalls != 2 || translator.documentCalls != 1 {
		t.Fatalf("organization translation calls after detail = metadata %d, document %d", translator.metadataCalls, translator.documentCalls)
	}

	rows, err := infradb.ListPluginTranslations(ctx, database, 7, types.PluginTranslationSourceOrganization, []uint{plugin.ID}, translationLocale)
	if err != nil || len(rows) != 1 {
		t.Fatalf("organization translation rows = %#v, %v", rows, err)
	}
	if rows[0].SourceType != types.PluginTranslationSourceOrganization || rows[0].SourceID != plugin.ID ||
		rows[0].PluginRevisionID != revision.ID || rows[0].MetadataSourceHash != skillMetadataHash(plugin.Name, plugin.Description) {
		t.Fatalf("organization translation identity = %#v", rows[0])
	}

	otherOrgContext := auth.WithContext(ctx, &types.Caller{Uin: 9, OrgID: 8, State: types.AuthStateSucc}, nil)
	if _, err := service.GetPlugin(otherOrgContext, 8, 9, plugin.PublicID, nil); err == nil {
		t.Fatal("organization Skill must not be visible across organizations")
	}
}

func TestProjectSkillListUsesOrganizationDisplayTranslation(t *testing.T) {
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	ctx := auth.WithContext(context.Background(), &types.Caller{Uin: 9, OrgID: 7, State: types.AuthStateSucc}, nil)
	project := &types.Project{PublicID: "project-translation", OrgID: 7, OwnerID: 9, Name: "Translation project", Status: "active"}
	if err := database.Create(project).Error; err != nil {
		t.Fatalf("create translation project: %v", err)
	}
	_, plugin, _ := createPluginServiceOrganizationSkill(t, database, 7, 9, "project-org-translate", "English body.")
	plugin.Name = "English Project Skill"
	plugin.Description = "An English project Skill description."
	if err := database.Save(plugin).Error; err != nil {
		t.Fatalf("save project Skill metadata: %v", err)
	}
	if err := database.Create(&types.ProjectPluginBinding{ProjectID: project.ID, PluginID: plugin.ID, Enabled: true, Config: []byte(`{}`), CreatedBy: 9, UpdatedBy: 9}).Error; err != nil {
		t.Fatalf("bind project Skill: %v", err)
	}

	translator := &fakeSkillDescriptionTranslator{}
	service := NewProjectService(
		database,
		newTestPermissionService(database),
		nil,
		nil,
		"test",
		nil,
		newSkillDisplayTranslationService(database, translator),
	).(*projectService)
	plugins, err := service.ListProjectPlugins(ctx, &contract.ListProjectPluginsRequest{PublicID: project.PublicID, Kind: "skill"})
	if err != nil || len(plugins) != 1 {
		t.Fatalf("list translated project Skills = %#v, %v", plugins, err)
	}
	if plugins[0].DisplayName != "中文技能" || plugins[0].Description != "中文展示描述" {
		t.Fatalf("translated project Skill view = %#v", plugins[0])
	}
}

func containsTranslationRevision(rows []types.PluginTranslation, revisionID uint) bool {
	for _, row := range rows {
		if row.PluginRevisionID == revisionID {
			return true
		}
	}
	return false
}
