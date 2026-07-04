package types

import "gorm.io/gorm"

const (
	// OrgSkillInstallationStatusActive means the org wants this skill available on all workers.
	OrgSkillInstallationStatusActive = "active"
	// OrgSkillInstallationStatusInstalling means the org install is being synchronized.
	OrgSkillInstallationStatusInstalling = "installing"
	// OrgSkillInstallationStatusFailed means the last synchronization attempt failed.
	OrgSkillInstallationStatusFailed = "failed"
)

// OrgSkillInstallation is the org-level source of truth for installed skills.
type OrgSkillInstallation struct {
	gorm.Model

	// OrgID scopes the installed skill to one organization.
	OrgID uint `gorm:"column:org_id;type:bigint;not null;uniqueIndex:idx_org_skill_installation_key;index"`
	// Source records where the skill came from, such as Leros, ClawHub, or github.
	Source string `gorm:"column:source;type:varchar(64);not null;uniqueIndex:idx_org_skill_installation_key"`
	// Action records the worker skill command action needed to replay this install.
	Action string `gorm:"column:action;type:varchar(32);not null;default:'install'"`
	// SkillID is the source-specific skill identifier.
	SkillID string `gorm:"column:skill_id;type:varchar(255);not null;uniqueIndex:idx_org_skill_installation_key"`
	// Version records the requested version, using latest when omitted.
	Version string `gorm:"column:version;type:varchar(64);not null;default:'latest';uniqueIndex:idx_org_skill_installation_key"`
	// Name is the user-facing skill name when known.
	Name string `gorm:"column:name;type:varchar(255);not null;default:'';index"`
	// Description is copied from marketplace metadata when available.
	Description string `gorm:"column:description;type:text"`
	// Category supports installed skill filtering.
	Category string `gorm:"column:category;type:varchar(100);not null;default:'';index"`
	// Tags stores marketplace tags.
	Tags SkillStringList `gorm:"column:tags;type:jsonb"`
	// PackageStoragePath points to the cached zip package when available.
	PackageStoragePath string `gorm:"column:package_storage_path;type:varchar(500)"`
	// Status records the org-level desired install status.
	Status string `gorm:"column:status;type:varchar(32);not null;default:'active';index"`
	// LastError stores the last synchronization error.
	LastError string `gorm:"column:last_error;type:text"`
	// InstalledBy records the user who initiated the install.
	InstalledBy uint `gorm:"column:installed_by;type:bigint;not null;default:0;index"`
}

// TableName returns the org skill installation table name.
func (OrgSkillInstallation) TableName() string {
	return TableNameOrgSkillInstallation
}
