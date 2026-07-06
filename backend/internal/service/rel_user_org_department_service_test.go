package service

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func TestMemberDepartmentServiceCRUDAndList(t *testing.T) {
	database := setupAccountServiceTestDB(t)
	if err := database.AutoMigrate(&types.UserOrg{}); err != nil {
		t.Fatalf("failed to migrate user org: %v", err)
	}
	service := NewMemberDepartmentService(database)
	ctx := accountServiceTestContext()

	userOrg := &types.UserOrg{Uin: 30, UserID: 30, OrgID: 1, IsDefault: true}
	if err := database.Create(userOrg).Error; err != nil {
		t.Fatalf("Create user org failed: %v", err)
	}
	department := &types.Department{Name: "测试部门", ParentID: 0, Sort: 1000, OrgID: 1}
	if err := database.Create(department).Error; err != nil {
		t.Fatalf("Create department failed: %v", err)
	}

	created, err := service.CreateMemberDepartment(ctx, &contract.CreateMemberDepartmentRequest{
		Uin:          userOrg.Uin,
		DepartmentID: department.ID,
		IsPrimary:    true,
	})
	if err != nil {
		t.Fatalf("CreateMemberDepartment failed: %v", err)
	}
	if created.ID == 0 || created.Uin != userOrg.Uin {
		t.Fatalf("unexpected created relation: %#v", created)
	}
	if created.OrgID != userOrg.OrgID {
		t.Fatalf("expected OrgID %d, got %d", userOrg.OrgID, created.OrgID)
	}

	got, err := service.GetMemberDepartment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMemberDepartment failed: %v", err)
	}
	if got.Uin != userOrg.Uin {
		t.Fatalf("unexpected relation by id: %#v", got)
	}

	isPrimary := false
	updated, err := service.UpdateMemberDepartment(ctx, created.ID, &contract.UpdateMemberDepartmentRequest{IsPrimary: &isPrimary})
	if err != nil {
		t.Fatalf("UpdateMemberDepartment failed: %v", err)
	}
	if updated.IsPrimary {
		t.Fatalf("expected updated is_primary, got %#v", updated)
	}

	uin := userOrg.Uin
	list, err := service.ListMemberDepartments(ctx, &contract.ListMemberDepartmentsRequest{Uin: &uin, Pagination: types.Pagination{Limit: 10}})
	if err != nil {
		t.Fatalf("ListMemberDepartments failed: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("unexpected relation list: %#v", list)
	}

	if err := service.DeleteMemberDepartment(ctx, created.ID); err != nil {
		t.Fatalf("DeleteMemberDepartment failed: %v", err)
	}
	if _, err := service.GetMemberDepartment(ctx, created.ID); err == nil || err.Error() != "member department relation not found" {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}
