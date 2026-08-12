package service

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestGuardByPublicID_ProjectTaskCreateUsesTaskPolicy(t *testing.T) {
	database := seedAutomationLinkDB(t)
	project := seedLinkedProject(t, database, "prj_permission", 1, 7, 0)
	permissionService := NewPermissionService(database, NewPermissionCore(database))

	err := permissionService.GuardByPublicID(
		context.Background(),
		types.PermissionCaller{OrgID: 1, Uin: 7},
		types.ResourceTypeProject,
		project.PublicID,
		types.ActionTaskCreate,
	)
	if err != nil {
		t.Fatalf("project task:create should be allowed for owner: %v", err)
	}
}
