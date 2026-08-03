package db

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// ProjectActivityListOptions 定义项目动态列表查询条件。
type ProjectActivityListOptions struct {
	ProjectID   string
	ProjectIDs  []string
	OperatorIDs []string
	BeforeTime  *time.Time
	BeforeID    uint
	Limit       int
}

// CreateProjectActivity 写入一条项目操作动态。
func CreateProjectActivity(ctx context.Context, db *gorm.DB, activity *types.ProjectActivity) error {
	return db.WithContext(ctx).Create(activity).Error
}

// ListProjectActivities 按时间倒序查询项目动态。
func ListProjectActivities(ctx context.Context, db *gorm.DB, opt ProjectActivityListOptions) ([]*types.ProjectActivity, error) {
	limit := opt.Limit
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	query := db.WithContext(ctx).Model(&types.ProjectActivity{})
	if opt.ProjectID != "" {
		query = query.Where("project_id = ?", opt.ProjectID)
	} else if opt.ProjectIDs != nil {
		if len(opt.ProjectIDs) == 0 {
			return nil, nil
		}
		query = query.Where("project_id IN (?)", opt.ProjectIDs)
	}
	if len(opt.OperatorIDs) == 1 {
		query = query.Where("operator_id = ?", opt.OperatorIDs[0])
	} else if len(opt.OperatorIDs) > 1 {
		query = query.Where("operator_id IN (?)", opt.OperatorIDs)
	}
	if opt.BeforeTime != nil && opt.BeforeID > 0 {
		query = query.Where("(created_at < ? OR (created_at = ? AND id < ?))", *opt.BeforeTime, *opt.BeforeTime, opt.BeforeID)
	}

	var activities []*types.ProjectActivity
	err := query.
		Order("created_at DESC").
		Order("id DESC").
		Limit(limit).
		Find(&activities).Error
	if err != nil {
		return nil, err
	}
	return activities, nil
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}
