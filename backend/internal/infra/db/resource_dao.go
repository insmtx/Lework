package db

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// CreateResource 创建统一资源记录。
func CreateResource(ctx context.Context, d *gorm.DB, resource *types.Resource) error {
	return d.WithContext(ctx).Create(resource).Error
}

// GetResourceByID 按主键查询资源，未找到返回 nil, nil。
func GetResourceByID(ctx context.Context, d *gorm.DB, id uint) (*types.Resource, error) {
	var entity types.Resource
	err := d.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", id).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetResourceByBizID 按组织 ID、资源类型和业务对象 ID 查询唯一资源记录。
// 利用唯一索引 ux_leros_resource_active_biz，未找到返回 nil, nil。
func GetResourceByBizID(ctx context.Context, d *gorm.DB, orgID uint, resourceType types.ResourceType, bizID uint) (*types.Resource, error) {
	var entity types.Resource
	err := d.WithContext(ctx).
		Where("org_id = ? AND type = ? AND biz_id = ? AND deleted_at IS NULL", orgID, resourceType, bizID).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListResourcesByParentID 查询指定父资源的所有直接子资源。
// 利用索引 idx_leros_resource_parent。
func ListResourcesByParentID(ctx context.Context, d *gorm.DB, parentID uint) ([]*types.Resource, error) {
	var entities []*types.Resource
	if err := d.WithContext(ctx).
		Where("parent_resource_id = ? AND deleted_at IS NULL", parentID).
		Order("id ASC").
		Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// ListResourcesByIDs 按 ID 列表批量查询资源，供 PermissionService 遍历祖先链时使用。
// 返回顺序与传入 ids 无关，调用方按需排序。
func ListResourcesByIDs(ctx context.Context, d *gorm.DB, ids []uint) ([]*types.Resource, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.Resource
	if err := d.WithContext(ctx).
		Where("id IN ? AND deleted_at IS NULL", ids).
		Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// DeleteResource 软删除资源记录。
func DeleteResource(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.Resource{}, id).Error
}
