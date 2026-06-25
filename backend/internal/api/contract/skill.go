package contract

import "context"

// SkillService defines the skill management contract.
type SkillService interface {
	ListRecentUsedSkills(ctx context.Context, orgID, uin uint, limit int) ([]SkillInstalledItem, error)
}
