package db

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestDepartmentDAOCRUDAndList(t *testing.T) {
	database := setupAccountDAOTestDB(t)
	ctx := context.Background()
	dao := NewGenericDao[types.Department](database)

	department := &types.Department{Name: "工程部", ParentID: 0, Sort: DepartmentSortGap, OrgID: 10}
	if err := dao.Insert(ctx, department); err != nil {
		t.Fatalf("Insert department failed: %v", err)
	}

	got, err := dao.GetByCond(ctx, &DepartmentCond{BaseCond: &BaseCond{ID: department.ID}})
	if err != nil {
		t.Fatalf("GetByCond failed: %v", err)
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
	if err := dao.Insert(ctx, child); err != nil {
		t.Fatalf("Insert child department failed: %v", err)
	}
	gotChild, err := dao.GetByCond(ctx, &DepartmentCond{BaseCond: &BaseCond{ID: child.ID}})
	if err != nil {
		t.Fatalf("GetByCond child failed: %v", err)
	}
	if len(gotChild.ParentIDs) != 1 || gotChild.ParentIDs[0] != department.ID {
		t.Fatalf("expected child parent_ids [%d], got %#v", department.ID, gotChild.ParentIDs)
	}

	got, err = dao.GetByCond(ctx, &DepartmentCond{OrgID: 10, Name: "工程部"})
	if err != nil {
		t.Fatalf("GetByCond by name/org failed: %v", err)
	}
	if got == nil || got.ID != department.ID {
		t.Fatalf("unexpected department by name: %#v", got)
	}

	if err := database.WithContext(ctx).Model(&types.Department{}).Where("id = ?", department.ID).Update("sort", DepartmentSortGap*2).Error; err != nil {
		t.Fatalf("update sort failed: %v", err)
	}

	opt := types.NewPageQuery(types.Caller{}, 0, 10)
	opt.AddExactFilter("org_id", "10")
	opt.AddExactFilter("parent_id", "0")
	items, total, err := ListDepartment(ctx, database, opt)
	if err != nil {
		t.Fatalf("ListDepartment failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].Sort != DepartmentSortGap*2 {
		t.Fatalf("unexpected department list: total=%d items=%#v", total, items)
	}

	if err := dao.Delete(ctx, department.ID); err != nil {
		t.Fatalf("Delete department failed: %v", err)
	}
	got, err = dao.GetByCond(ctx, &DepartmentCond{BaseCond: &BaseCond{ID: department.ID}})
	if err != nil {
		t.Fatalf("GetByCond after delete failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted department to be nil, got %#v", got)
	}
}
