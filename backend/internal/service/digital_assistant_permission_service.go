package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Lework/backend/internal/api/contract"
	infradb "github.com/insmtx/Lework/backend/internal/infra/db"
	"github.com/insmtx/Lework/backend/types"
)

func (s *digitalAssistantService) GetDigitalAssistantPermissions(
	ctx context.Context,
	publicID string,
) (*contract.DigitalAssistantPermissionSettingsView, error) {
	if s.userRepo == nil {
		return nil, fmt.Errorf("digital assistant user repository is not configured")
	}
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	assistant, err := infradb.GetDigitalAssistantByPublicID(ctx, s.db, publicID)
	if err != nil {
		return nil, err
	}
	if assistant == nil {
		return nil, errDigitalAssistantNotFound
	}
	if _, err := newDigitalAssistantAccessManager(s.db).requireDirectRole(ctx, caller.OrgID, caller.Uin, assistant, types.ResourceRoleOwner, types.ResourceRoleAdmin); err != nil {
		return nil, err
	}
	return s.loadDigitalAssistantPermissionSettings(ctx, s.db, assistant)
}

func (s *digitalAssistantService) UpdateDigitalAssistantPermissions(
	ctx context.Context,
	req *contract.UpdateDigitalAssistantPermissionsRequest,
) (*contract.DigitalAssistantPermissionSettingsView, error) {
	if s.userRepo == nil {
		return nil, fmt.Errorf("digital assistant user repository is not configured")
	}
	caller, err := requireCallerOrg(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.PublicID) == "" {
		return nil, fmt.Errorf("public_id is required")
	}
	if !types.ValidDigitalAssistantVisibility(req.Visibility) {
		return nil, fmt.Errorf("visibility must be public or private")
	}
	ownerPublicID, err := validateDigitalAssistantPermissionMembers(req.Members)
	if err != nil {
		return nil, err
	}
	assistant, err := infradb.GetDigitalAssistantByPublicID(ctx, s.db, req.PublicID)
	if err != nil {
		return nil, err
	}
	if assistant == nil {
		return nil, errDigitalAssistantNotFound
	}
	currentVisibility := assistant.Visibility
	if currentVisibility == "" {
		currentVisibility = types.DigitalAssistantVisibilityPublic
	}

	var result *contract.DigitalAssistantPermissionSettingsView
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		access := newDigitalAssistantAccessManager(tx)
		callerRole, err := access.requireDirectRole(ctx, caller.OrgID, caller.Uin, assistant, types.ResourceRoleOwner, types.ResourceRoleAdmin)
		if err != nil {
			return err
		}
		resource, err := lockDigitalAssistantResource(ctx, tx, caller.OrgID, assistant.ID)
		if err != nil {
			return err
		}
		bindings, err := infradb.ListResourceBindingsByResourceID(ctx, tx, resource.ID)
		if err != nil {
			return err
		}
		ownerUin, err := currentDigitalAssistantOwner(bindings)
		if err != nil {
			return err
		}
		owner, err := s.userRepo.GetUserByUin(ctx, ownerUin)
		if err != nil {
			return err
		}
		if owner == nil || owner.PublicID != ownerPublicID {
			return fmt.Errorf("owner must be the current digital assistant owner")
		}
		if callerRole == types.ResourceRoleAdmin && req.Visibility != currentVisibility {
			return errDigitalAssistantForbidden
		}
		targetUins, err := s.resolveDigitalAssistantMemberUins(ctx, caller.OrgID, req.Members)
		if err != nil {
			return err
		}
		if targetUins[ownerPublicID] != ownerUin {
			return fmt.Errorf("owner must be the current digital assistant owner")
		}
		desired := make(map[uint]types.ResourceRole, len(targetUins))
		for _, member := range req.Members {
			desired[targetUins[member.User.PublicID]] = member.Role
		}
		current := make(map[uint]*types.ResourceBinding, len(bindings))
		for _, binding := range bindings {
			if binding.Uin != nil && *binding.Uin != 0 {
				current[*binding.Uin] = binding
			}
		}
		if err := applyDigitalAssistantPermissionDiff(ctx, tx, caller.OrgID, resource.ID, current, desired); err != nil {
			return err
		}
		if assistant.Visibility != req.Visibility {
			if err := tx.Model(&types.DigitalAssistant{}).Where("id = ?", assistant.ID).Update("visibility", req.Visibility).Error; err != nil {
				return err
			}
			assistant.Visibility = req.Visibility
		}
		result, err = s.loadDigitalAssistantPermissionSettings(ctx, tx, assistant)
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func validateDigitalAssistantPermissionMembers(members []contract.DigitalAssistantPermissionMemberInput) (string, error) {
	ownerPublicID := ""
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		publicID := strings.TrimSpace(member.User.PublicID)
		if publicID == "" {
			return "", fmt.Errorf("member user_public_id is required")
		}
		if _, ok := seen[publicID]; ok {
			return "", fmt.Errorf("duplicate member user_public_id")
		}
		seen[publicID] = struct{}{}
		switch member.Role {
		case types.ResourceRoleOwner:
			if ownerPublicID != "" {
				return "", fmt.Errorf("only the current owner may hold the owner role")
			}
			ownerPublicID = publicID
		case types.ResourceRoleAdmin, types.ResourceRoleMember:
		default:
			return "", fmt.Errorf("invalid member role")
		}
	}
	if ownerPublicID == "" {
		return "", fmt.Errorf("owner must be present in members")
	}
	return ownerPublicID, nil
}

func (s *digitalAssistantService) resolveDigitalAssistantMemberUins(
	ctx context.Context,
	orgID uint,
	members []contract.DigitalAssistantPermissionMemberInput,
) (map[string]uint, error) {
	if s.userRepo == nil {
		return nil, fmt.Errorf("digital assistant user repository is not configured")
	}
	publicIDs := make([]string, 0, len(members))
	for _, member := range members {
		publicIDs = append(publicIDs, member.User.PublicID)
	}
	result, err := s.userRepo.GetUinMapByPublicIDs(ctx, orgID, publicIDs)
	if err != nil {
		return nil, err
	}
	for _, publicID := range publicIDs {
		if result[publicID] == 0 {
			return nil, fmt.Errorf("member is not an active organization member")
		}
	}
	return result, nil
}

func currentDigitalAssistantOwner(bindings []*types.ResourceBinding) (uint, error) {
	var ownerUin uint
	for _, binding := range bindings {
		if binding.Role != types.ResourceRoleOwner || binding.Uin == nil || *binding.Uin == 0 {
			continue
		}
		if ownerUin != 0 && ownerUin != *binding.Uin {
			return 0, fmt.Errorf("digital assistant has multiple owners")
		}
		ownerUin = *binding.Uin
	}
	if ownerUin == 0 {
		return 0, fmt.Errorf("digital assistant owner is missing")
	}
	return ownerUin, nil
}

func applyDigitalAssistantPermissionDiff(
	ctx context.Context,
	tx *gorm.DB,
	orgID, resourceID uint,
	current map[uint]*types.ResourceBinding,
	desired map[uint]types.ResourceRole,
) error {
	for uin, binding := range current {
		if _, keep := desired[uin]; keep {
			continue
		}
		if err := infradb.DeleteResourceBinding(ctx, tx, binding.ID); err != nil {
			return err
		}
	}
	for uin, role := range desired {
		if existing, ok := current[uin]; ok {
			if existing.Role == role {
				continue
			}
			if err := infradb.UpdateResourceBindingRole(ctx, tx, existing.ID, role); err != nil {
				return err
			}
			continue
		}
		uinCopy := uin
		if err := infradb.CreateResourceBinding(ctx, tx, &types.ResourceBinding{
			OrgID:      orgID,
			Uin:        &uinCopy,
			ResourceID: resourceID,
			Role:       role,
		}); err != nil {
			return err
		}
	}
	return nil
}

func lockDigitalAssistantResource(ctx context.Context, tx *gorm.DB, orgID, assistantID uint) (*types.Resource, error) {
	var resource types.Resource
	err := tx.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("org_id = ? AND type = ? AND biz_id = ? AND deleted_at IS NULL", orgID, types.ResourceTypeAssistant, assistantID).
		First(&resource).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, errDigitalAssistantNotFound
		}
		return nil, err
	}
	return &resource, nil
}

func (s *digitalAssistantService) loadDigitalAssistantPermissionSettings(
	ctx context.Context,
	database *gorm.DB,
	assistant *types.DigitalAssistant,
) (*contract.DigitalAssistantPermissionSettingsView, error) {
	resource, err := infradb.GetResourceByBizID(ctx, database, assistant.OrgID, types.ResourceTypeAssistant, assistant.ID)
	if err != nil {
		return nil, err
	}
	if resource == nil {
		return nil, errDigitalAssistantNotFound
	}
	bindings, err := infradb.ListResourceBindingsByResourceID(ctx, database, resource.ID)
	if err != nil {
		return nil, err
	}
	uins := make([]uint, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Uin != nil && *binding.Uin != 0 {
			uins = append(uins, *binding.Uin)
		}
	}
	if s.userRepo == nil {
		return nil, fmt.Errorf("digital assistant user repository is not configured")
	}
	users, err := s.userRepo.GetUsersByUins(ctx, uins)
	if err != nil {
		return nil, err
	}
	members := make([]contract.DigitalAssistantPermissionMemberView, 0, len(bindings))
	for _, binding := range bindings {
		if binding.Uin == nil || *binding.Uin == 0 {
			continue
		}
		user := users[*binding.Uin]
		if user == nil {
			continue
		}
		members = append(members, contract.DigitalAssistantPermissionMemberView{
			User: contract.DigitalAssistantPermissionUserView{
				PublicID: user.PublicID,
				Name:     user.Name,
				Email:    user.Email,
				Avatar:   user.AvatarURL,
			},
			Role: binding.Role,
		})
	}
	sort.SliceStable(members, func(i, j int) bool {
		return types.ResourceRoleStrength[members[i].Role] > types.ResourceRoleStrength[members[j].Role]
	})
	visibility := assistant.Visibility
	if visibility == "" {
		visibility = types.DigitalAssistantVisibilityPublic
	}
	return &contract.DigitalAssistantPermissionSettingsView{Visibility: visibility, Members: members}, nil
}
