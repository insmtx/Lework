package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/ygpkg/storage-go"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	skillcache "github.com/insmtx/Leros/backend/internal/skill/cache"
	"github.com/insmtx/Leros/backend/pkg/messaging"
	"github.com/insmtx/Leros/backend/types"
)

func TestProcessSkillPackageUploadedIsIdempotentAndDoesNotCreateProjectFile(t *testing.T) {
	ctx := context.Background()
	database := setupPluginServiceTestDB(t)
	if err := database.AutoMigrate(&types.ProjectFile{}); err != nil {
		t.Fatal(err)
	}
	setupPluginServiceTestStorage(t)
	project := &types.Project{PublicID: "prj_skill_sync", OrgID: 7, OwnerID: 9, Name: "Skill Project", Status: "active"}
	if err := database.Create(project).Error; err != nil {
		t.Fatal(err)
	}
	document := []byte("---\nname: demo\ndescription: demo helper\n---\n\nUse demo.\n")
	archive, err := skillcache.GenerateSkillZip(document, map[string][]byte{"references/guide.md": []byte("guide")})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	sha256Hex := hex.EncodeToString(digest[:])
	putResult, err := filestore.GetStorage().PutObject(
		ctx,
		filestore.DefaultBucket(),
		"arbitrary/no-required-prefix/demo.zip",
		bytes.NewReader(archive),
		storage.WithContentType("application/zip"),
	)
	if err != nil {
		t.Fatal(err)
	}
	event := messaging.SkillPackageUploadedEvent{
		EventID: "skill_evt_test", WorkerID: 4, RunID: "run-1",
		ProjectID: project.ID, ActorUIN: 9, SkillCode: "demo",
		ChangeType: messaging.SkillChangeCreated,
		StorageURI: putResult.Path.URI(), SHA256: sha256Hex,
		FileSize: int64(len(archive)), Filename: "demo.zip", MimeType: "application/zip",
	}
	if err := processSkillPackageUploaded(ctx, database, 7, event); err != nil {
		t.Fatalf("process first event: %v", err)
	}
	if err := processSkillPackageUploaded(ctx, database, 7, event); err != nil {
		t.Fatalf("process duplicate event: %v", err)
	}

	var uploads []types.FileUpload
	if err := database.Find(&uploads).Error; err != nil {
		t.Fatal(err)
	}
	if len(uploads) != 1 {
		t.Fatalf("FileUploads = %d, want 1", len(uploads))
	}
	if uploads[0].Purpose != filestore.PurposeSkillPackage ||
		uploads[0].PublicID == event.EventID ||
		uploads[0].PublicID == "" {
		t.Fatalf("FileUpload = %#v", uploads[0])
	}
	var plugins []types.Plugin
	if err := database.Find(&plugins).Error; err != nil {
		t.Fatal(err)
	}
	if len(plugins) != 1 || plugins[0].Code != "demo" {
		t.Fatalf("plugins = %#v", plugins)
	}
	var revisionCount int64
	if err := database.Model(&types.PluginRevision{}).Count(&revisionCount).Error; err != nil {
		t.Fatal(err)
	}
	if revisionCount != 1 {
		t.Fatalf("PluginRevisions = %d, want 1", revisionCount)
	}
	var bindingCount int64
	if err := database.Model(&types.ProjectPluginBinding{}).Count(&bindingCount).Error; err != nil {
		t.Fatal(err)
	}
	if bindingCount != 1 {
		t.Fatalf("ProjectPluginBindings = %d, want 1", bindingCount)
	}
	var projectFileCount int64
	if err := database.Model(&types.ProjectFile{}).Count(&projectFileCount).Error; err != nil {
		t.Fatal(err)
	}
	if projectFileCount != 0 {
		t.Fatalf("ProjectFiles = %d, want 0", projectFileCount)
	}
	downloads, err := (&pluginService{db: database}).ResolveSkillDownloadURLs(
		ctx,
		7, types.CallerKindUser, 1,
		&contract.ResolveSkillDownloadURLsRequest{SkillCodes: []string{"demo"}},
	)
	if err != nil || len(downloads.Skills) != 1 || downloads.Skills[0].DownloadURL == "" {
		t.Fatalf("Skill downloads = %#v, error = %v", downloads, err)
	}

	orgOnlyDocument := []byte("---\nname: org-only\ndescription: organization helper\n---\n\nUse organization helper.\n")
	orgOnlyArchive, err := skillcache.GenerateSkillZip(orgOnlyDocument, nil)
	if err != nil {
		t.Fatal(err)
	}
	orgOnlyDigest := sha256.Sum256(orgOnlyArchive)
	orgOnlyPut, err := filestore.GetStorage().PutObject(
		ctx,
		filestore.DefaultBucket(),
		"another/arbitrary/path/org-only.zip",
		bytes.NewReader(orgOnlyArchive),
		storage.WithContentType("application/zip"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := processSkillPackageUploaded(ctx, database, 7, messaging.SkillPackageUploadedEvent{
		EventID: "skill_evt_org_only", WorkerID: 4, RunID: "run-2",
		SkillCode: "org-only", ChangeType: messaging.SkillChangeCreated,
		StorageURI: orgOnlyPut.Path.URI(), SHA256: hex.EncodeToString(orgOnlyDigest[:]),
		FileSize: int64(len(orgOnlyArchive)), Filename: "org-only.zip", MimeType: "application/zip",
	}); err != nil {
		t.Fatalf("process organization-only Skill: %v", err)
	}
	if err := database.Model(&types.ProjectPluginBinding{}).Count(&bindingCount).Error; err != nil {
		t.Fatal(err)
	}
	if bindingCount != 1 {
		t.Fatalf("organization-only Skill created a project binding: count=%d", bindingCount)
	}
}

func TestProcessSkillPackageUploadedRejectsStorageURISHAConflict(t *testing.T) {
	ctx := context.Background()
	database := setupPluginServiceTestDB(t)
	setupPluginServiceTestStorage(t)
	document := []byte("---\nname: demo\ndescription: demo helper\n---\n\nUse demo.\n")
	archive, err := skillcache.GenerateSkillZip(document, nil)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(archive)
	sha256Hex := hex.EncodeToString(digest[:])
	putResult, err := filestore.GetStorage().PutObject(
		ctx,
		filestore.DefaultBucket(),
		"conflict/demo.zip",
		bytes.NewReader(archive),
		storage.WithContentType("application/zip"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&types.FileUpload{
		PublicID: "file_existing", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 7, OwnerID: 9, Filename: "demo.zip", OriginalName: "demo.zip",
		MimeType: "application/zip", FileSize: int64(len(archive)),
		StorageURI: putResult.Path.URI(), Sha256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Purpose: filestore.PurposeSkillPackage, Status: "active",
	}).Error; err != nil {
		t.Fatal(err)
	}
	event := messaging.SkillPackageUploadedEvent{
		EventID: "skill_evt_conflict", WorkerID: 4, RunID: "run-1",
		SkillCode: "demo", ChangeType: messaging.SkillChangeUpdated,
		StorageURI: putResult.Path.URI(), SHA256: sha256Hex,
		FileSize: int64(len(archive)), Filename: "demo.zip", MimeType: "application/zip",
	}
	err = processSkillPackageUploaded(ctx, database, 7, event)
	var permanent *permanentSkillPackageError
	if err == nil || !errors.As(err, &permanent) {
		t.Fatalf("error = %v, want permanent conflict", err)
	}
}
