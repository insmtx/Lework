package service

import (
	"context"

	"gorm.io/gorm"

	infra_db "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type resourceStore struct {
	db *gorm.DB
}

func newResourceStore(db *gorm.DB) *resourceStore { return &resourceStore{db: db} }

// GetByID 根据主键 ID 查询资源
func (s *resourceStore) GetByID(ctx context.Context, id uint) (*types.Resource, error) {
	return infra_db.GetResourceByID(ctx, s.db, id)
}

// GetByBizID 根据组织 ID、资源类型与业务 ID 查询资源
func (s *resourceStore) GetByBizID(ctx context.Context, orgID uint, resourceType types.ResourceType, bizID uint) (*types.Resource, error) {
	return infra_db.GetResourceByBizID(ctx, s.db, orgID, resourceType, bizID)
}

// Create 创建新资源
func (s *resourceStore) Create(ctx context.Context, r *types.Resource) (*types.Resource, error) {
	if err := infra_db.CreateResource(ctx, s.db, r); err != nil {
		return nil, err
	}
	return r, nil
}

// GetBindingByUin 根据 uin 与资源 ID 查询资源绑定关系
func (s *resourceStore) GetBindingByUin(ctx context.Context, uin uint, resourceID uint) (*types.ResourceBinding, error) {
	return infra_db.GetResourceBindingByUin(ctx, s.db, resourceID, uin)
}

// GetBindingByAssistantID 根据资源 ID 与助手 ID 查询资源绑定关系
func (s *resourceStore) GetBindingByAssistantID(ctx context.Context, resourceID uint, assistantID uint) (*types.ResourceBinding, error) {
	return infra_db.GetResourceBindingByAssistantID(ctx, s.db, resourceID, assistantID)
}

// CreateBinding 创建新的资源绑定关系
func (s *resourceStore) CreateBinding(ctx context.Context, b *types.ResourceBinding) (*types.ResourceBinding, error) {
	if err := infra_db.CreateResourceBinding(ctx, s.db, b); err != nil {
		return nil, err
	}
	return b, nil
}

// ListBindingsByResourceID 根据资源 ID 列出所有绑定关系
func (s *resourceStore) ListBindingsByResourceID(ctx context.Context, resourceID uint) ([]*types.ResourceBinding, error) {
	return infra_db.ListResourceBindingsByResourceID(ctx, s.db, resourceID)
}

// CountBindingsByRole 统计某个资源下指定角色的绑定数量
func (s *resourceStore) CountBindingsByRole(ctx context.Context, resourceID uint, role types.ResourceRole) (int64, error) {
	return infra_db.CountResourceBindingsByRole(ctx, s.db, resourceID, role)
}
