package service

import (
	"testing"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/types"
)

func TestDepartmentServiceCRUDAndList(t *testing.T) {
	database := setupAccountServiceTestDB(t)
	service := NewDepartmentService(database)
	ctx := accountServiceTestContext()

	created, err := service.CreateDepartment(ctx, &contract.CreateDepartmentRequest{
		Name:  "服务部门",
		OrgID: 1,
		Sort:  1000,
	})
	if err != nil {
		t.Fatalf("CreateDepartment failed: %v", err)
	}

	got, err := service.GetDepartment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetDepartment failed: %v", err)
	}
	if got.Name != "服务部门" {
		t.Fatalf("unexpected department by id: %#v", got)
	}

	sort := uint(2000)
	updated, err := service.UpdateDepartment(ctx, created.ID, &contract.UpdateDepartmentRequest{Sort: &sort})
	if err != nil {
		t.Fatalf("UpdateDepartment failed: %v", err)
	}
	if updated.Sort != 2000 {
		t.Fatalf("expected updated sort, got %#v", updated)
	}

	orgID := uint(1)
	list, err := service.ListDepartments(ctx, &contract.ListDepartmentsRequest{OrgID: &orgID, Pagination: types.Pagination{Limit: 10}})
	if err != nil {
		t.Fatalf("ListDepartments failed: %v", err)
	}
	if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != created.ID {
		t.Fatalf("unexpected department list: %#v", list)
	}

	if err := service.DeleteDepartment(ctx, created.ID); err != nil {
		t.Fatalf("DeleteDepartment failed: %v", err)
	}
	if _, err := service.GetDepartment(ctx, created.ID); err == nil || err.Error() != "department not found" {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestDepartmentParentIDs(t *testing.T) {
	database := setupAccountServiceTestDB(t)
	service := NewDepartmentService(database)
	ctx := accountServiceTestContext()

	root, err := service.CreateDepartment(ctx, &contract.CreateDepartmentRequest{
		Name:  "根部门",
		OrgID: 1,
	})
	if err != nil {
		t.Fatalf("CreateDepartment root failed: %v", err)
	}
	if len(root.ParentIDs) != 0 {
		t.Fatalf("expected root parent_ids empty, got %#v", root.ParentIDs)
	}

	child, err := service.CreateDepartment(ctx, &contract.CreateDepartmentRequest{
		Name:     "子部门",
		ParentID: root.ID,
		OrgID:    1,
	})
	if err != nil {
		t.Fatalf("CreateDepartment child failed: %v", err)
	}
	if len(child.ParentIDs) != 1 || child.ParentIDs[0] != root.ID {
		t.Fatalf("expected child parent_ids [%d], got %#v", root.ID, child.ParentIDs)
	}

	grandchild, err := service.CreateDepartment(ctx, &contract.CreateDepartmentRequest{
		Name:     "孙部门",
		ParentID: child.ID,
		OrgID:    1,
	})
	if err != nil {
		t.Fatalf("CreateDepartment grandchild failed: %v", err)
	}
	if len(grandchild.ParentIDs) != 2 || grandchild.ParentIDs[0] != root.ID || grandchild.ParentIDs[1] != child.ID {
		t.Fatalf("expected grandchild parent_ids [%d,%d], got %#v", root.ID, child.ID, grandchild.ParentIDs)
	}

	otherRoot, err := service.CreateDepartment(ctx, &contract.CreateDepartmentRequest{
		Name:  "另一根部门",
		OrgID: 1,
	})
	if err != nil {
		t.Fatalf("CreateDepartment other root failed: %v", err)
	}

	newParentID := otherRoot.ID
	movedChild, err := service.UpdateDepartment(ctx, child.ID, &contract.UpdateDepartmentRequest{
		ParentID: &newParentID,
	})
	if err != nil {
		t.Fatalf("UpdateDepartment move child failed: %v", err)
	}
	if len(movedChild.ParentIDs) != 1 || movedChild.ParentIDs[0] != otherRoot.ID {
		t.Fatalf("expected moved child parent_ids [%d], got %#v", otherRoot.ID, movedChild.ParentIDs)
	}

	gotGrandchild, err := service.GetDepartment(ctx, grandchild.ID)
	if err != nil {
		t.Fatalf("GetDepartment grandchild failed: %v", err)
	}
	if len(gotGrandchild.ParentIDs) != 2 || gotGrandchild.ParentIDs[0] != otherRoot.ID || gotGrandchild.ParentIDs[1] != child.ID {
		t.Fatalf("expected recomputed grandchild parent_ids [%d,%d], got %#v", otherRoot.ID, child.ID, gotGrandchild.ParentIDs)
	}

	cycleParentID := grandchild.ID
	if _, err := service.UpdateDepartment(ctx, child.ID, &contract.UpdateDepartmentRequest{
		ParentID: &cycleParentID,
	}); err == nil || err.Error() != "department parent creates a cycle" {
		t.Fatalf("expected cycle error, got %v", err)
	}
}
