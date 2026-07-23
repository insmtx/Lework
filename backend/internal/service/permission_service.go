package service

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

type PermissionCore interface {
	Can(ctx context.Context, caller types.PermissionCaller, action types.Action,
		ref types.ResourceRef, input *types.MemberInput) (types.PermissionDecision, error)
	Explain(ctx context.Context, caller types.PermissionCaller, action types.Action,
		ref types.ResourceRef, input *types.MemberInput) (types.PermissionExplainDecision, error)
	BatchCan(ctx context.Context, caller types.PermissionCaller, actions []types.Action,
		refs []types.ResourceRef, input *types.MemberInput) ([]types.PermissionDecision, error)
	ResolveEffectiveRole(ctx context.Context, caller types.PermissionCaller,
		resource *types.Resource) (types.ResourceRole, *types.ResourceBinding, *types.Resource, error)
}

const (
	reasonPolicyDenied = "policy_denied"
)

type PermissionService struct {
	db      *gorm.DB
	core    PermissionCore
	newCore func(db *gorm.DB) PermissionCore
}

func NewPermissionService(db *gorm.DB, core PermissionCore) *PermissionService {
	return &PermissionService{db: db, core: core, newCore: func(d *gorm.DB) PermissionCore {
		return NewPermissionCore(d)
	}}
}

type Caller = types.PermissionCaller

type ResourceRef = types.ResourceRef

type Decision = types.PermissionDecision

type ExplainDecision = types.PermissionExplainDecision

type MemberInput = types.MemberInput

type MemberAuthContext struct {
	TargetRole  types.ResourceRole
	NewRole     types.ResourceRole
	IsSelf      bool
	IsLastOwner bool
}

func (s *PermissionService) Can(ctx context.Context, caller Caller, action Action, ref ResourceRef, input *MemberInput) (Decision, error) {
	return s.core.Can(ctx, caller, action, ref, input)
}

func (s *PermissionService) Explain(ctx context.Context, caller Caller, action Action, ref ResourceRef, input *MemberInput) (ExplainDecision, error) {
	return s.core.Explain(ctx, caller, action, ref, input)
}

func (s *PermissionService) BatchCan(ctx context.Context, caller Caller, actions []Action, refs []ResourceRef, input *MemberInput) ([]Decision, error) {
	return s.core.BatchCan(ctx, caller, actions, refs, input)
}

func (s *PermissionService) ResolveEffectiveRole(ctx context.Context, caller Caller, resource *types.Resource) (types.ResourceRole, *types.ResourceBinding, *types.Resource, error) {
	return s.core.ResolveEffectiveRole(ctx, caller, resource)
}
