package service

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/types"
)

const (
	reasonAllowed             = "allowed"
	reasonNoBinding           = "no_binding"
	reasonOrgMismatch         = "org_mismatch"
	reasonResourceNotFound    = "resource_not_found"
	reasonMemberContextDenied = "member_context_denied"
)

type memberAuthContext struct {
	TargetRole  types.ResourceRole
	NewRole     types.ResourceRole
	IsSelf      bool
	IsLastOwner bool
}

type refGroupKey struct {
	resourceType types.ResourceType
	bizID        uint
}

type permissionCore struct {
	store *resourceStore
}

// NewPermissionCore 创建权限核心实例，负责权限评估
func NewPermissionCore(db *gorm.DB) *permissionCore {
	return &permissionCore{store: newResourceStore(db)}
}

// Can 评估调用者对指定资源和操作是否拥有权限
func (s *permissionCore) Can(ctx context.Context, caller types.PermissionCaller, action types.Action, ref types.ResourceRef, input *types.MemberInput) (types.PermissionDecision, error) {
	return s.evaluate(ctx, caller, action, ref, input, false)
}

// Explain 评估权限并返回带有继承信息的解释性决策
func (s *permissionCore) Explain(ctx context.Context, caller types.PermissionCaller, action types.Action, ref types.ResourceRef, input *types.MemberInput) (types.PermissionExplainDecision, error) {
	decision, err := s.evaluate(ctx, caller, action, ref, input, true)
	if err != nil {
		return types.PermissionExplainDecision{}, err
	}

	explain := types.PermissionExplainDecision{PermissionDecision: decision}
	if decision.Allowed {
		explain.Inherited = decision.MatchedResourceID != 0 && decision.MatchedResourceID != decision.ResourceID
	}
	return explain, nil
}

// BatchCan 批量评估多个操作和资源引用的权限
func (s *permissionCore) BatchCan(ctx context.Context, caller types.PermissionCaller, actions []types.Action, refs []types.ResourceRef, input *types.MemberInput) ([]types.PermissionDecision, error) {
	if len(actions) != len(refs) {
		return nil, fmt.Errorf("actions and refs must have the same length")
	}
	if len(actions) == 0 {
		return nil, nil
	}

	results := make([]types.PermissionDecision, len(actions))
	groups := make(map[refGroupKey][]int)
	for i := range actions {
		key := refGroupKey{resourceType: refs[i].Type, bizID: refs[i].BizID}
		groups[key] = append(groups[key], i)
	}

	for key, indices := range groups {
		ref := types.ResourceRef{Type: key.resourceType, BizID: key.bizID}
		groupActions := make([]types.Action, len(indices))
		for j, idx := range indices {
			groupActions[j] = actions[idx]
		}

		decisions, err := s.evaluateActions(ctx, caller, ref, groupActions, input)
		if err != nil {
			return nil, err
		}
		for j, idx := range indices {
			results[idx] = decisions[j]
		}
	}
	return results, nil
}

func (s *permissionCore) evaluateActions(ctx context.Context, caller types.PermissionCaller, ref types.ResourceRef, actions []types.Action, input *types.MemberInput) ([]types.PermissionDecision, error) {
	if len(actions) == 0 {
		return nil, nil
	}

	resource, err := s.loadResourceForEval(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		denied := denyDecision(reasonResourceNotFound, 0)
		out := make([]types.PermissionDecision, len(actions))
		for i := range out {
			out[i] = denied
		}
		return out, nil
	}
	if resource.OrgID != caller.OrgID {
		denied := denyDecision(reasonOrgMismatch, resource.ID)
		out := make([]types.PermissionDecision, len(actions))
		for i := range out {
			out[i] = denied
		}
		return out, nil
	}

	return s.evaluateLoadedActions(ctx, caller, resource, actions, input)
}

func (s *permissionCore) evaluate(ctx context.Context, caller types.PermissionCaller, action types.Action, ref types.ResourceRef, input *types.MemberInput, explain bool) (types.PermissionDecision, error) {
	_ = explain
	if caller.OrgID == 0 {
		return denyDecision(reasonOrgMismatch, 0), nil
	}

	decisions, err := s.evaluateActions(ctx, caller, ref, []types.Action{action}, input)
	if err != nil {
		return types.PermissionDecision{}, err
	}
	return decisions[0], nil
}

func (s *permissionCore) loadResourceForEval(ctx context.Context, caller types.PermissionCaller, ref types.ResourceRef) (*types.Resource, error) {
	resource, err := s.store.GetByBizID(ctx, caller.OrgID, ref.Type, ref.BizID)
	if err != nil {
		return nil, fmt.Errorf("load resource: %w", err)
	}
	return resource, nil
}

func (s *permissionCore) evaluateLoadedActions(ctx context.Context, caller types.PermissionCaller, resource *types.Resource, actions []types.Action, input *types.MemberInput) ([]types.PermissionDecision, error) {
	effectiveRole, matchedBinding, matchedResource, err := s.resolveEffectiveRole(ctx, caller, resource)
	if err != nil {
		return nil, fmt.Errorf("resolve effective role: %w", err)
	}

	decisions := make([]types.PermissionDecision, len(actions))
	for i, action := range actions {
		if effectiveRole == "" {
			decisions[i] = denyDecision(reasonNoBinding, resource.ID)
			continue
		}
		if !SystemPolicy.Allows(resource.Type, effectiveRole, action) {
			decisions[i] = denyDecision(reasonPolicyDenied, resource.ID)
			continue
		}
		if IsMemberManagementAction(action) {
			authCtx, buildErr := s.buildMemberAuthContext(ctx, caller, resource, effectiveRole, input)
			if buildErr != nil {
				return nil, fmt.Errorf("build member auth context: %w", buildErr)
			}
			if !s.memberActionAllowed(action, effectiveRole, authCtx) {
				decisions[i] = denyDecision(reasonMemberContextDenied, resource.ID)
				continue
			}
		}
		decision := types.PermissionDecision{
			Allowed:    true,
			Reason:     reasonAllowed,
			Role:       effectiveRole,
			ResourceID: resource.ID,
		}
		if matchedBinding != nil {
			decision.MatchedBindingID = matchedBinding.ID
		}
		if matchedResource != nil {
			decision.MatchedResourceID = matchedResource.ID
		}
		decisions[i] = decision
	}
	return decisions, nil
}

// ResolveEffectiveRole 解析调用者在该资源及其父资源链上的生效角色
func (s *permissionCore) ResolveEffectiveRole(ctx context.Context, caller types.PermissionCaller, resource *types.Resource) (types.ResourceRole, *types.ResourceBinding, *types.Resource, error) {
	return s.resolveEffectiveRole(ctx, caller, resource)
}

func (s *permissionCore) resolveEffectiveRole(ctx context.Context, caller types.PermissionCaller, resource *types.Resource) (types.ResourceRole, *types.ResourceBinding, *types.Resource, error) {
	var roles []types.ResourceRole
	var matchedBinding *types.ResourceBinding
	var matchedResource *types.Resource

	direct, err := s.findDirectBinding(ctx, caller, resource.ID)
	if err != nil {
		return "", nil, nil, err
	}
	if direct != nil {
		roles = append(roles, direct.Role)
		matchedBinding = direct
		matchedResource = resource
	}

	current := resource
	for current.ParentResourceID != nil && *current.ParentResourceID != 0 {
		parent, err := s.store.GetByID(ctx, *current.ParentResourceID)
		if err != nil {
			return "", nil, nil, err
		}
		if parent == nil {
			break
		}
		binding, err := s.findDirectBinding(ctx, caller, parent.ID)
		if err != nil {
			return "", nil, nil, err
		}
		if binding != nil {
			roles = append(roles, binding.Role)
			if matchedBinding == nil {
				matchedBinding = binding
				matchedResource = parent
			}
		}
		current = parent
	}

	if len(roles) == 0 {
		return "", nil, nil, nil
	}
	return MaxRole(roles), matchedBinding, matchedResource, nil
}

func (s *permissionCore) findDirectBinding(ctx context.Context, caller types.PermissionCaller, resourceID uint) (*types.ResourceBinding, error) {
	if caller.Uin != 0 {
		return s.store.GetBindingByUin(ctx, caller.Uin, resourceID)
	}
	if caller.AssistantID != 0 {
		return s.store.GetBindingByAssistantID(ctx, resourceID, caller.AssistantID)
	}
	return nil, nil
}

func (s *permissionCore) buildMemberAuthContext(ctx context.Context, caller types.PermissionCaller, resource *types.Resource, operatorRole types.ResourceRole, input *types.MemberInput) (memberAuthContext, error) {
	ctxOut := memberAuthContext{}
	if input == nil {
		return ctxOut, nil
	}

	if input.TargetAssistantID == nil && input.TargetUin == caller.Uin {
		ctxOut.IsSelf = true
	}
	if input.TargetAssistantID != nil && caller.AssistantID != 0 && *input.TargetAssistantID == caller.AssistantID {
		ctxOut.IsSelf = true
	}

	ctxOut.NewRole = input.RequestedRole

	var targetBinding *types.ResourceBinding
	if input.TargetAssistantID == nil && input.TargetUin != 0 {
		b, err := s.store.GetBindingByUin(ctx, input.TargetUin, resource.ID)
		if err != nil {
			return ctxOut, err
		}
		targetBinding = b
	} else if input.TargetAssistantID != nil && *input.TargetAssistantID != 0 {
		b, err := s.store.GetBindingByAssistantID(ctx, resource.ID, *input.TargetAssistantID)
		if err != nil {
			return ctxOut, err
		}
		targetBinding = b
	}

	if targetBinding != nil {
		ctxOut.TargetRole = targetBinding.Role
	}

	if ctxOut.TargetRole == types.ResourceRoleOwner {
		ownerCount, err := s.store.CountBindingsByRole(ctx, resource.ID, types.ResourceRoleOwner)
		if err != nil {
			return ctxOut, err
		}
		ctxOut.IsLastOwner = ownerCount <= 1
	}

	return ctxOut, nil
}

func (s *permissionCore) memberActionAllowed(action types.Action, operatorRole types.ResourceRole, ctx memberAuthContext) bool {
	if action == types.ActionProjectMemberLeave {
		if !ctx.IsSelf {
			return false
		}
		if ctx.IsLastOwner {
			return false
		}
		return true
	}

	switch operatorRole {
	case types.ResourceRoleOwner:
		if ctx.IsLastOwner && (action == types.ActionProjectMemberDelete || (action == types.ActionProjectMemberUpdate && ctx.TargetRole == types.ResourceRoleOwner && ctx.NewRole != types.ResourceRoleOwner)) {
			return false
		}
		return true
	case types.ResourceRoleAdmin:
		if ctx.TargetRole == types.ResourceRoleOwner {
			return false
		}
		if ctx.NewRole == types.ResourceRoleOwner {
			return false
		}
		return action == types.ActionProjectMemberCreate || action == types.ActionProjectMemberUpdate || action == types.ActionProjectMemberDelete
	case types.ResourceRoleMember:
		return action == types.ActionProjectMemberList
	}
	return false
}

func denyDecision(reason string, resourceID uint) types.PermissionDecision {
	return types.PermissionDecision{
		Allowed:    false,
		Reason:     reason,
		ResourceID: resourceID,
	}
}
