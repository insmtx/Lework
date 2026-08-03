package db

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

func TestFileUploadOwnerScopeValidationAndVisibility(t *testing.T) {
	database, err := gorm.Open(
		sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"),
		&gorm.Config{},
	)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.AutoMigrate(&types.FileUpload{}); err != nil {
		t.Fatalf("migrate file upload: %v", err)
	}
	ctx := context.Background()

	invalid := []types.FileUpload{
		{
			PublicID: "file_invalid_org", OwnerScope: types.OwnerScopeOrganization,
			OrgID: 0, OwnerID: 1, StorageURI: "local://test/invalid-org",
		},
		{
			PublicID: "file_invalid_system_org", OwnerScope: types.OwnerScopeSystem,
			OrgID: 1, OwnerID: 0, StorageURI: "local://test/invalid-system-org",
		},
		{
			PublicID: "file_invalid_system_owner", OwnerScope: types.OwnerScopeSystem,
			OrgID: 0, OwnerID: 1, StorageURI: "local://test/invalid-system-owner",
		},
	}
	for index := range invalid {
		if err := CreateFileUpload(ctx, database, &invalid[index]); err == nil {
			t.Fatalf("invalid owner combination %d was accepted", index)
		}
	}
	if err := database.Create(&types.FileUpload{
		PublicID: "file_invalid_database_constraint", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, OwnerID: 3, StorageURI: "local://test/invalid-database",
	}).Error; err == nil {
		t.Fatal("database accepted an invalid system file owner")
	}

	organization := &types.FileUpload{
		PublicID: "file_organization", OwnerScope: types.OwnerScopeOrganization,
		OrgID: 7, OwnerID: 8, StorageURI: "local://test/organization",
		Status: "active", Purpose: "artifact",
	}
	system := &types.FileUpload{
		PublicID: "file_system", OwnerScope: types.OwnerScopeSystem,
		OrgID: 0, OwnerID: 0, StorageURI: "local://test/system",
		Status: "active", Purpose: "artifact",
	}
	if err := CreateFileUpload(ctx, database, organization); err != nil {
		t.Fatalf("create organization file: %v", err)
	}
	if err := CreateFileUpload(ctx, database, system); err != nil {
		t.Fatalf("create system file: %v", err)
	}
	files, total, err := ListFileUploads(ctx, database, 7, "", 0, 10)
	if err != nil || total != 1 || len(files) != 1 || files[0].ID != organization.ID {
		t.Fatalf("organization files = %#v, total=%d, err=%v", files, total, err)
	}
	if got, err := GetFileUploadByPublicID(ctx, database, 7, system.PublicID); err != nil || got != nil {
		t.Fatalf("system file leaked through organization query: %#v, %v", got, err)
	}
	if got, err := GetSystemFileUploadByPublicID(ctx, database, system.PublicID); err != nil ||
		got == nil || got.ID != system.ID {
		t.Fatalf("system file lookup = %#v, %v", got, err)
	}
}
