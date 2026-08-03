package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/types"
)

// CreateProject 创建项目
func CreateProject(ctx context.Context, db *gorm.DB, project *types.Project) error {
	return db.WithContext(ctx).Create(project).Error
}

// GetProjectByPublicID 根据组织ID和PublicID获取项目
func GetProjectByPublicID(ctx context.Context, db *gorm.DB, orgID uint, publicID string) (*types.Project, error) {
	var entity types.Project
	err := db.WithContext(ctx).Where("org_id = ? AND public_id = ?", orgID, publicID).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// UpdateProject 更新项目
func UpdateProject(ctx context.Context, db *gorm.DB, project *types.Project) error {
	return db.WithContext(ctx).Save(project).Error
}

// TouchProjectUpdatedAt 仅刷新项目活跃时间，避免覆盖项目其他字段。
func TouchProjectUpdatedAt(ctx context.Context, db *gorm.DB, id uint, updatedAt time.Time) error {
	return db.WithContext(ctx).
		Model(&types.Project{}).
		Where("id = ?", id).
		Update("updated_at", updatedAt).Error
}

// DeleteProject 删除项目（软删除）
func DeleteProject(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.Project{}, id).Error
}

// GetProjectsByIDs 根据项目ID列表批量获取项目
func GetProjectsByIDs(ctx context.Context, db *gorm.DB, ids []uint) ([]*types.Project, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.Project
	err := db.WithContext(ctx).Where("id IN (?)", ids).Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// IsProjectUserMember 检查指定用户（uin）是否在项目资源上拥有有效 binding。
func IsProjectUserMember(ctx context.Context, db *gorm.DB, orgID, uin, projectID uint) (bool, error) {
	var count int64
	err := db.WithContext(ctx).
		Table(types.TableNameResourceBinding+" AS rb").
		Joins("INNER JOIN "+types.TableNameResource+" AS r ON r.id = rb.resource_id").
		Where("r.type = ? AND r.biz_id = ? AND r.org_id = ?", string(types.ResourceTypeProject), projectID, orgID).
		Where("rb.uin = ? AND rb.deleted_at IS NULL AND r.deleted_at IS NULL", uin).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetProjectSession 根据项目ID获取scope=project的会话
func GetProjectSession(ctx context.Context, db *gorm.DB, projectID uint) (*types.Session, error) {
	var entity types.Session
	err := db.WithContext(ctx).
		Where("project_id = ? AND type = ?", projectID, string(types.SessionTypeProject)).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListProjectIDsByUser 查询用户在组织内通过 resource_binding 可访问的项目 ID 列表。
func ListProjectIDsByUser(ctx context.Context, db *gorm.DB, orgID, uin uint) ([]uint, error) {
	var boundBizIDs []uint
	if err := db.WithContext(ctx).
		Table(types.TableNameResourceBinding+" AS rb").
		Select("r.biz_id").
		Joins("INNER JOIN "+types.TableNameResource+" AS r ON r.id = rb.resource_id").
		Where("rb.org_id = ? AND rb.uin = ? AND r.type = ?", orgID, uin, string(types.ResourceTypeProject)).
		Where("rb.deleted_at IS NULL AND r.deleted_at IS NULL").
		Pluck("r.biz_id", &boundBizIDs).Error; err != nil {
		return nil, err
	}
	return boundBizIDs, nil
}

// IsProjectAssistantBound 检查助手是否在项目资源上拥有有效 binding。
func IsProjectAssistantBound(ctx context.Context, db *gorm.DB, orgID, projectID, assistantID uint) (bool, error) {
	resource, err := GetResourceByBizID(ctx, db, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		return false, err
	}
	if resource == nil {
		return false, nil
	}
	binding, err := GetResourceBindingByAssistantID(ctx, db, resource.ID, assistantID)
	if err != nil {
		return false, err
	}
	return binding != nil, nil
}

// ResolveBoundProjectAssistantID 解析项目上已绑定的 AI 队友 ID：优先组织默认队友，否则取最新绑定的助手。
// 未找到时返回 0, nil。
func ResolveBoundProjectAssistantID(ctx context.Context, db *gorm.DB, orgID, projectID uint) (uint, error) {
	resource, err := GetResourceByBizID(ctx, db, orgID, types.ResourceTypeProject, projectID)
	if err != nil {
		return 0, err
	}
	if resource == nil {
		return 0, nil
	}

	defaultAssistantID, err := GetDefaultAssistantIDByOrg(ctx, db, orgID)
	if err != nil {
		return 0, err
	}
	if defaultAssistantID > 0 {
		binding, err := GetResourceBindingByAssistantID(ctx, db, resource.ID, defaultAssistantID)
		if err != nil {
			return 0, err
		}
		if binding != nil {
			return defaultAssistantID, nil
		}
	}

	bindings, err := ListResourceBindingsByResourceID(ctx, db, resource.ID)
	if err != nil {
		return 0, err
	}
	var latestAssistantID uint
	var latestBindingID uint
	for _, b := range bindings {
		if b.AssistantID == nil || *b.AssistantID == 0 {
			continue
		}
		if b.ID > latestBindingID {
			latestBindingID = b.ID
			latestAssistantID = *b.AssistantID
		}
	}
	return latestAssistantID, nil
}

// ListProjects 查询项目列表，使用 PageQuery 作为查询参数
func ListProjects(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.Project, int64, error) {
	var entities []*types.Project
	var total int64

	query := d.WithContext(ctx).Table(types.TableNameProject).
		Where("org_id = ? AND deleted_at IS NULL", opt.OrgID)
	if len(opt.ProjectIDs) > 0 {
		query = query.Where("id IN (?)", opt.ProjectIDs)
	}

	for _, filter := range opt.Filters {
		switch filter.Field {
		case "name":
			if filter.ExactMatch {
				query = query.Where("name IN (?)", filter.Value)
			} else {
				query = query.Where("name LIKE ?", "%"+filter.Value[0]+"%")
			}
		case "status":
			query = query.Where("status IN (?)", filter.Value)
		case "public_id":
			query = query.Where("public_id IN (?)", filter.Value)
		default:
			logs.WarnContextf(ctx, "[project][ListProjects] invalid filter field: %s", filter.Field)
		}
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return nil, 0, nil
	}

	if len(opt.OrderBy) > 0 {
		query = query.Order(strings.Join(opt.OrderBy, ","))
	} else {
		// 中文注释：项目列表默认按最近活跃时间排序，避免项目内新增任务/消息后仍停留在旧位置。
		query = query.Order("updated_at DESC, created_at DESC")
	}

	query = query.Offset(opt.Offset)
	if !opt.ListAll && opt.Limit > 0 {
		query = query.Limit(opt.Limit)
	} else {
		query = query.Limit(150)
	}

	if err := query.Find(&entities).Error; err != nil {
		return nil, 0, err
	}
	return entities, total, nil
}

// GetProjectByID 根据主键ID获取项目
func GetProjectByID(ctx context.Context, d *gorm.DB, id uint) (*types.Project, error) {
	var entity types.Project
	err := d.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", id).First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}
