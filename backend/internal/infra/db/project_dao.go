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

// CreateProjectMember 创建项目成员
func CreateProjectMember(ctx context.Context, db *gorm.DB, member *types.ProjectMember) error {
	return db.WithContext(ctx).Create(member).Error
}

// ListProjectMembers 查询项目成员列表
func ListProjectMembers(ctx context.Context, db *gorm.DB, projectID uint) ([]*types.ProjectMember, error) {
	var entities []*types.ProjectMember
	err := db.WithContext(ctx).
		Where("project_id = ? AND deleted_at IS NULL", projectID).
		Order("joined_at ASC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// ListProjectIDsByMember 查询指定用户在某 org 下作为成员加入的所有 projectID。
//
// 通过 JOIN leros_project 表按 org_id 过滤，实现 org 隔离。
// 仅返回 member_type='user' 且未软删除的成员关系。
func ListProjectIDsByMember(ctx context.Context, db *gorm.DB, orgID, userID uint) ([]uint, error) {
	var projectIDs []uint
	err := db.WithContext(ctx).
		Table(types.TableNameProjectMember+" AS pm").
		Select("pm.project_id").
		Joins("INNER JOIN "+types.TableNameProject+" AS p ON p.id = pm.project_id").
		Where("pm.member_id = ? AND pm.member_type = ?", userID, string(types.MemberTypeUser)).
		Where("p.org_id = ?", orgID).
		Where("pm.deleted_at IS NULL AND p.deleted_at IS NULL").
		Pluck("pm.project_id", &projectIDs).Error
	if err != nil {
		return nil, err
	}
	return projectIDs, nil
}

// IsProjectUserMember 检查指定用户是否为某 project 的 user 类型成员。
// 仅校验 member_type='user' 且未软删除的记录。
func IsProjectUserMember(ctx context.Context, db *gorm.DB, orgID, userID, projectID uint) (bool, error) {
	var count int64
	err := db.WithContext(ctx).
		Table(types.TableNameProjectMember+" AS pm").
		Joins("INNER JOIN "+types.TableNameProject+" AS p ON p.id = pm.project_id").
		Where("pm.project_id = ? AND pm.member_id = ? AND pm.member_type = ?",
			projectID, userID, string(types.MemberTypeUser)).
		Where("p.org_id = ?", orgID).
		Where("pm.deleted_at IS NULL AND p.deleted_at IS NULL").
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

// ListProjects 查询项目列表，使用 PageQuery 作为查询参数
func ListProjects(ctx context.Context, d *gorm.DB, opt *types.PageQuery) ([]*types.Project, int64, error) {
	var entities []*types.Project
	var total int64

	query := d.WithContext(ctx).Table(types.TableNameProject).
		Where("org_id = ? AND deleted_at IS NULL", opt.OrgID)
	if opt.Uin > 0 {
		query = query.Where("owner_id = ?", opt.Uin)
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

// ListProjectsReferencingSkill 查询 org 内 metadata.extra.skills 引用了指定技能的项目。
func ListProjectsReferencingSkill(ctx context.Context, d *gorm.DB, orgID uint, skillName string) ([]*types.Project, error) {
	skillName = strings.TrimSpace(skillName)
	if skillName == "" {
		return nil, nil
	}

	var entities []*types.Project
	err := d.WithContext(ctx).
		Where("org_id = ? AND deleted_at IS NULL", orgID).
		Where(`EXISTS (
			SELECT 1 FROM jsonb_array_elements(COALESCE(metadata->'extra'->'skills', '[]'::jsonb)) AS elem
			WHERE lower(trim(both from elem->>'code')) = lower(?)
			   OR lower(trim(both from elem->>'name')) = lower(?)
		)`, skillName, skillName).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
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

// IsProjectMember 检查 member 是否为项目的指定类型成员（群聊准入 / AI 队友归属校验）。
func IsProjectMember(ctx context.Context, db *gorm.DB, projectID, memberID uint, memberType types.MemberType) (bool, error) {
	var count int64
	err := db.WithContext(ctx).
		Model(&types.ProjectMember{}).
		Where("project_id = ? AND member_id = ? AND member_type = ?", projectID, memberID, string(memberType)).
		Where("deleted_at IS NULL").
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// GetLatestProjectAssistant 查询项目最新加入的 AI 队友，优先返回 is_default=true 的成员；无则返回 (nil,nil)。
func GetLatestProjectAssistant(ctx context.Context, db *gorm.DB, projectID uint) (*types.ProjectMember, error) {
	var entity types.ProjectMember
	err := db.WithContext(ctx).
		Where("project_id = ? AND member_type = ?", projectID, string(types.MemberTypeAssistant)).
		Where("deleted_at IS NULL").
		Order("is_default DESC, id DESC").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetDefaultProjectAssistant 查询项目的默认 AI 队友（is_default=true）；无则返回 (nil,nil)。
func GetDefaultProjectAssistant(ctx context.Context, db *gorm.DB, projectID uint) (*types.ProjectMember, error) {
	var entity types.ProjectMember
	err := db.WithContext(ctx).
		Where("project_id = ? AND member_type = ? AND is_default = true", projectID, string(types.MemberTypeAssistant)).
		Where("deleted_at IS NULL").
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListProjectAssistantMembers 查询项目的所有 AI 队友成员。
func ListProjectAssistantMembers(ctx context.Context, db *gorm.DB, projectID uint) ([]*types.ProjectMember, error) {
	var entities []*types.ProjectMember
	err := db.WithContext(ctx).
		Where("project_id = ? AND member_type = ?", projectID, string(types.MemberTypeAssistant)).
		Where("deleted_at IS NULL").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// DeleteProjectMember 软删除项目成员记录。
func DeleteProjectMember(ctx context.Context, db *gorm.DB, memberID uint) error {
	return db.WithContext(ctx).Delete(&types.ProjectMember{}, memberID).Error
}

// BatchCreateProjectMembers 批量创建项目成员记录。
func BatchCreateProjectMembers(ctx context.Context, db *gorm.DB, members []*types.ProjectMember) error {
	if len(members) == 0 {
		return nil
	}
	return db.WithContext(ctx).Create(&members).Error
}
