package db

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// UpsertOrgSkillInstallation creates or updates an org-level installed skill record.
func UpsertOrgSkillInstallation(ctx context.Context, database *gorm.DB, item *types.OrgSkillInstallation) error {
	if item == nil {
		return nil
	}
	return database.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "org_id"},
			{Name: "source"},
			{Name: "skill_id"},
			{Name: "version"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"action",
			"name",
			"description",
			"category",
			"tags",
			"package_storage_path",
			"status",
			"last_error",
			"installed_by",
			"updated_at",
			"deleted_at",
		}),
	}).Create(item).Error
}

// ListOrgSkillInstallations returns active org-level installed skills.
func ListOrgSkillInstallations(ctx context.Context, database *gorm.DB, orgID uint) ([]*types.OrgSkillInstallation, error) {
	var items []*types.OrgSkillInstallation
	if orgID == 0 {
		return items, nil
	}
	err := database.WithContext(ctx).
		Where("org_id = ? AND status = ?", orgID, types.OrgSkillInstallationStatusActive).
		Order("updated_at DESC").
		Find(&items).Error
	return items, err
}

// DeleteOrgSkillInstallation removes one org-level installed skill by its source key.
func DeleteOrgSkillInstallation(ctx context.Context, database *gorm.DB, orgID uint, source, skillID, version string) error {
	if orgID == 0 {
		return nil
	}
	return database.WithContext(ctx).
		Where("org_id = ? AND source = ? AND skill_id = ? AND version = ?", orgID, source, skillID, version).
		Delete(&types.OrgSkillInstallation{}).Error
}

// DeleteOrgSkillInstallationByName removes org-level installed skills by display name or skill id.
func DeleteOrgSkillInstallationByName(ctx context.Context, database *gorm.DB, orgID uint, name string) error {
	if orgID == 0 || name == "" {
		return nil
	}
	return database.WithContext(ctx).
		Where("org_id = ? AND (name = ? OR skill_id = ?)", orgID, name, name).
		Delete(&types.OrgSkillInstallation{}).Error
}
