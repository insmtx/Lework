package db

import (
	"context"
	"testing"

	"github.com/insmtx/Leros/backend/types"
)

func TestMemberDepartmentDAOCRUDAndList(t *testing.T) {
	database := setupAccountDAOTestDB(t)
	ctx := context.Background()

	relation := &types.MemberDepartment{
		Uin:          40,
		OrgID:        1,
		DepartmentID: 60,
		IsPrimary:    true,
	}
	if err := CreateMemberDepartment(ctx, database, relation); err != nil {
		t.Fatalf("CreateMemberDepartment failed: %v", err)
	}

	got, err := GetMemberDepartmentByID(ctx, database, relation.ID)
	if err != nil {
		t.Fatalf("GetMemberDepartmentByID failed: %v", err)
	}
	if got == nil || got.Uin != 40 {
		t.Fatalf("unexpected relation by id: %#v", got)
	}

	relation.IsPrimary = false
	if err := UpdateMemberDepartment(ctx, database, relation); err != nil {
		t.Fatalf("UpdateMemberDepartment failed: %v", err)
	}

	opt := types.NewPageQuery(types.Caller{}, 0, 10)
	opt.AddExactFilter("uin", "40")
	items, total, err := ListMemberDepartments(ctx, database, opt)
	if err != nil {
		t.Fatalf("ListMemberDepartments failed: %v", err)
	}
	if total != 1 || len(items) != 1 || items[0].IsPrimary {
		t.Fatalf("unexpected relation list: total=%d items=%#v", total, items)
	}

	if err := DeleteMemberDepartment(ctx, database, relation.ID); err != nil {
		t.Fatalf("DeleteMemberDepartment failed: %v", err)
	}
	got, err = GetMemberDepartmentByID(ctx, database, relation.ID)
	if err != nil {
		t.Fatalf("GetMemberDepartmentByID after delete failed: %v", err)
	}
	if got != nil {
		t.Fatalf("expected deleted relation to be nil, got %#v", got)
	}
}
