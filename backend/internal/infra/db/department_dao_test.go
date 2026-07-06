package db

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestDepartmentDAOCRUDAndList(t *testing.T) {
	database := setupAccountDAOTestDB(t)
	ctx := context.Background()

	department := &types.Department{Name: "工程部", ParentID: 0, Sort: DepartmentSortGap, OrgID: 10}
	if err := CreateDepartment(ctx, database, department); err != nil {
		t.Fatalf("CreateDepartment failed: %v", err)
	}

	got, err := GetDepartmentByID(ctx, database, department.ID)
	if err != nil {
		t.Fatalf("GetDepartmentByID failed: %v", err)
	}
	if got == nil || got.Name != "工程部" {
		t.Fatalf("unexpected department by id: %#v", got)
	}
	if len(got.ParentIDs) != 0 {
		t.Fatalf("expected empty parent_ids, got %#v", got.ParentIDs)
	}

	child := &types.Department{
		Name:      "后端组",
		ParentID:  department.ID,
		ParentIDs: types.BuildDepartmentParentIDs(department),
		Sort:      DepartmentSortGap,
		OrgID:     10,
	}
	if err := CreateDepartment(ctx, database, child); err != nil {
		t.Fatalf("CreateDepartment child failed: %v", err)
	}
	gotChild, err := GetDepartmentByID(ctx, database, child.ID)
	if err != nil {
		t.Fatalf("GetDepartmentByID child failed: %v", err)
	}
	if len(gotChild.ParentIDs) != 1 || gotChild.ParentIDs[0] != department.ID {
		t.Fatalf("expected child parent_ids [%d], got %#v", department.ID, gotChild.ParentIDs)
	}

	got, err = GetDepartmentByName(ctx, database, 10, "工程部")
	if err != nil {
		t.Fatalf("GetDepartmentByName failed: %v", err)
	}
	if got == nil || got.ID != department.ID {
		t.Fatalf("unexpected department by name: %#v", got)
	}

	if err := UpdateDepartmentSort(ctx, database, department.ID, DepartmentSortGap*2); err != nil {
		t.Fatalf("UpdateDepartmentSort failed: %v", err)
	}

	opt := types.NewPageQuery(types.Caller{}, 0, 10)
	opt.AddExactFilter("org_id", "10")
	opt.AddExactFilter("parent_id", "0")
	items, total, err := ListDepartments(ctx, database, opt)
	if err != nil {
		t.Fatalf("ListDepartments failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Sort != DepartmentSortGap*2 {
		t.Fatalf("unexpected department list: total=%d items=%#v", total, items)
	}

	if err := DeleteDepartment(ctx, database, department.ID); err != nil {
		t.Fatalf("DeleteDepartment failed: %v", err)
	}
	got, err = GetDepartmentByID(ctx, database, department.ID)
	if err != nil {
		t.Fatalf("GetDepartmentByID after delete failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted department to be nil, got %#v", got)
	}
}
