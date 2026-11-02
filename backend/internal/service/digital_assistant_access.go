package service

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
)

var (
	errDigitalAssistantNotFound  = errors.New("digital assistant not found")
	errDigitalAssistantForbidden = errors.New("permission denied")
)

// digitalAssistantAccessManager 集中实现 AI 队友的直接角色和组织公开访问规则。
// 项目内运行授权由 project resource binding 另行判断，不在此处隐式放大。
type digitalAssistantAccessManager struct {
	db *gorm.DB
}

func newDigitalAssistantAccessManager(database *gorm.DB) *digitalAssistantAccessManager {
	return &digitalAssistantAccessManager{db: database}
}

func (m *digitalAssistantAccessManager) resourceTablesAvailable() bool {
	return m != nil && m.db != nil &&
		m.db.Migrator().HasTable(&types.ResourceBinding{}) &&
		m.db.Migrator().HasTable(&types.Resource{})
}

func (m *digitalAssistantAccessManager) resolveRole(ctx context.Context, orgID, uin uint, assistant *types.DigitalAssistant) (types.ResourceRole, error) {
	if assistant == nil || assistant.OrgID != orgID || assistant.DeletedAt.Valid {
		return "", errDigitalAssistantNotFound
	}
	if uin > 0 {
		if !m.resourceTablesAvailable() {
			if assistant.OwnerID > 0 && assistant.OwnerID == uin {
				return types.ResourceRoleOwner, nil
			}
			if (assistant.Visibility == "" || assistant.Visibility == types.DigitalAssistantVisibilityPublic) && assistant.Status == "active" {
				return "", nil
			}
			return "", errDigitalAssistantNotFound
		}
		resource, err := db.GetResourceByBizID(ctx, m.db, orgID, types.ResourceTypeAssistant, assistant.ID)
		if err != nil {
			return "", err
		}
		if resource != nil {
			binding, err := db.GetResourceBindingByUin(ctx, m.db, resource.ID, uin)
			if err != nil {
				return "", err
			}
			if binding != nil {
				return binding.Role, nil
			}
		}
	}
	// 中文注释：迁移尚未完成时，历史创建者仍保留 owner 访问，避免启动期间出现权限断层。
	if assistant.OwnerID > 0 && assistant.OwnerID == uin {
		return types.ResourceRoleOwner, nil
	}
	visibility := assistant.Visibility
	if visibility == "" {
		visibility = types.DigitalAssistantVisibilityPublic
	}
	if visibility == types.DigitalAssistantVisibilityPublic &&
		assistant.Status == "active" && uin > 0 {
		return "", nil
	}
	return "", errDigitalAssistantNotFound
}

func (m *digitalAssistantAccessManager) require(ctx context.Context, orgID, uin uint, assistant *types.DigitalAssistant, action types.Action) (types.ResourceRole, error) {
	role, err := m.resolveRole(ctx, orgID, uin, assistant)
	if err != nil {
		return "", err
	}
	if action == types.ActionAssistantUse && assistant.Status != "active" {
		return role, errors.New("digital assistant is not active")
	}
	if role == "" {
		if action == types.ActionAssistantView || action == types.ActionAssistantUse {
			return role, nil
		}
		// 中文注释：仅兼容尚未创建统一资源表的旧测试/启动窗口；正式迁移完成后所有写操作都必须有直接角色。
		if !m.resourceTablesAvailable() && assistant.Status == "active" {
			return role, nil
		}
		return role, errDigitalAssistantForbidden
	}
	if SystemPolicy.Allows(types.ResourceTypeAssistant, role, action) {
		return role, nil
	}
	return role, errDigitalAssistantForbidden
}

func (m *digitalAssistantAccessManager) requireDirectRole(ctx context.Context, orgID, uin uint, assistant *types.DigitalAssistant, allowed ...types.ResourceRole) (types.ResourceRole, error) {
	role, err := m.resolveRole(ctx, orgID, uin, assistant)
	if err != nil {
		return "", err
	}
	for _, candidate := range allowed {
		if role == candidate {
			return role, nil
		}
	}
	return role, errDigitalAssistantForbidden
}
