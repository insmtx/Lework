package db

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

// CreateResourceBinding 创建资源角色绑定记录。
func CreateResourceBinding(ctx context.Context, d *gorm.DB, binding *types.ResourceBinding) error {
	return d.WithContext(ctx).Create(binding).Error
}

// GetResourceBindingByID 按主键查询绑定记录，未找到返回 nil, nil。
func GetResourceBindingByID(ctx context.Context, d *gorm.DB, id uint) (*types.ResourceBinding, error) {
	var entity types.ResourceBinding
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

// GetResourceBindingByUin 查询指定用户在指定资源上的唯一有效绑定。
// 利用唯一索引 ux_leros_resource_binding_uin，供 PermissionService 查用户直接绑定。
// 未找到返回 nil, nil。
func GetResourceBindingByUin(ctx context.Context, d *gorm.DB, resourceID uint, uin uint) (*types.ResourceBinding, error) {
	var entity types.ResourceBinding
	err := d.WithContext(ctx).
		Where("resource_id = ? AND uin = ? AND deleted_at IS NULL", resourceID, uin).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetResourceBindingByAssistantID 查询指定助手在指定资源上的唯一有效绑定。
// 利用唯一索引 ux_leros_resource_binding_assistant，供 PermissionService 查助手直接绑定。
// 未找到返回 nil, nil。
func GetResourceBindingByAssistantID(ctx context.Context, d *gorm.DB, resourceID uint, assistantID uint) (*types.ResourceBinding, error) {
	var entity types.ResourceBinding
	err := d.WithContext(ctx).
		Where("resource_id = ? AND assistant_id = ? AND deleted_at IS NULL", resourceID, assistantID).
		First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// ListResourceBindingsByResourceID 查询指定资源上的所有有效绑定。
// 用于 ListResourceBindings 接口展示协作者列表。
func ListResourceBindingsByResourceID(ctx context.Context, d *gorm.DB, resourceID uint) ([]*types.ResourceBinding, error) {
	var entities []*types.ResourceBinding
	if err := d.WithContext(ctx).
		Where("resource_id = ? AND deleted_at IS NULL", resourceID).
		Order("id ASC").
		Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// ListResourceBindingsByResourceIDs 按资源 ID 列表批量查询有效绑定。
// 供 PermissionService 沿资源树一次性加载所有祖先资源的绑定。
func ListResourceBindingsByResourceIDs(ctx context.Context, d *gorm.DB, resourceIDs []uint) ([]*types.ResourceBinding, error) {
	if len(resourceIDs) == 0 {
		return nil, nil
	}
	var entities []*types.ResourceBinding
	if err := d.WithContext(ctx).
		Where("resource_id IN ? AND deleted_at IS NULL", resourceIDs).
		Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// ListResourceBindingsByUin 查询指定用户在组织内的所有有效绑定。
// 利用复合索引 idx_leros_resource_binding_org_uin。
func ListResourceBindingsByUin(ctx context.Context, d *gorm.DB, orgID uint, uin uint) ([]*types.ResourceBinding, error) {
	var entities []*types.ResourceBinding
	if err := d.WithContext(ctx).
		Where("org_id = ? AND uin = ? AND deleted_at IS NULL", orgID, uin).
		Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// ListResourceBindingsByAssistantID 查询指定助手在组织内的所有有效绑定。
// 利用复合索引 idx_leros_resource_binding_org_assistant。
func ListResourceBindingsByAssistantID(ctx context.Context, d *gorm.DB, orgID uint, assistantID uint) ([]*types.ResourceBinding, error) {
	var entities []*types.ResourceBinding
	if err := d.WithContext(ctx).
		Where("org_id = ? AND assistant_id = ? AND deleted_at IS NULL", orgID, assistantID).
		Find(&entities).Error; err != nil {
		return nil, err
	}
	return entities, nil
}

// UpdateResourceBindingRole 更新绑定记录的资源角色字段。
func UpdateResourceBindingRole(ctx context.Context, d *gorm.DB, id uint, role types.ResourceRole) error {
	return d.WithContext(ctx).
		Model(&types.ResourceBinding{}).
		Where("id = ? AND deleted_at IS NULL", id).
		Update("resource_role", role).Error
}

// DeleteResourceBinding 软删除资源角色绑定记录。
func DeleteResourceBinding(ctx context.Context, d *gorm.DB, id uint) error {
	return d.WithContext(ctx).Delete(&types.ResourceBinding{}, id).Error
}

// CountResourceBindingsByRole 统计指定资源上指定角色的有效绑定数量。
// 供 PermissionService 判断 is_last_owner 时使用。
func CountResourceBindingsByRole(ctx context.Context, d *gorm.DB, resourceID uint, role types.ResourceRole) (int64, error) {
	var count int64
	if err := d.WithContext(ctx).
		Model(&types.ResourceBinding{}).
		Where("resource_id = ? AND resource_role = ? AND deleted_at IS NULL", resourceID, role).
		Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}
