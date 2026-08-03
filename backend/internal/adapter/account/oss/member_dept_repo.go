//go:build !enterprise

package oss

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type memberDeptRepo struct {
	dao *db.MemberDepartmentEntityDao
}

func newMemberDeptRepo(d *gorm.DB) *memberDeptRepo {
	return &memberDeptRepo{dao: db.NewMemberDepartmentEntityDao(d)}
}

func (r *memberDeptRepo) withTx(tx *gorm.DB) *memberDeptRepo {
	return &memberDeptRepo{dao: &db.MemberDepartmentEntityDao{GenericDao: r.dao.WithTx(tx)}}
}

func (r *memberDeptRepo) db() *gorm.DB {
	return r.dao.DB()
}

func (r *memberDeptRepo) Create(ctx context.Context, md *types.MemberDepartment) error {
	return r.dao.Insert(ctx, md)
}

func (r *memberDeptRepo) BatchCreate(ctx context.Context, mds []*types.MemberDepartment) error {
	if len(mds) == 0 {
		return nil
	}
	return r.dao.DB().WithContext(ctx).Create(&mds).Error
}

func (r *memberDeptRepo) Delete(ctx context.Context, id uint) error {
	return r.dao.Delete(ctx, id)
}

func (r *memberDeptRepo) DeleteByUinAndOrgID(ctx context.Context, uin, orgID uint) error {
	return r.dao.DB().WithContext(ctx).
		Where("uin = ? AND org_id = ? AND deleted_at IS NULL", uin, orgID).
		Delete(&types.MemberDepartment{}).Error
}

func (r *memberDeptRepo) ListByUin(ctx context.Context, uin uint) ([]*types.MemberDepartment, error) {
	return r.dao.ListByCond(ctx, &db.MemberDeptCond{Uin: uin})
}

func (r *memberDeptRepo) ListByUinAndOrgID(ctx context.Context, uin, orgID uint) ([]*types.MemberDepartment, error) {
	return r.dao.ListByCond(ctx, &db.MemberDeptCond{Uin: uin, OrgID: orgID})
}

// ListByUinsAndOrgID returns a map from uin to list of member-department relations.
func (r *memberDeptRepo) ListByUinsAndOrgID(ctx context.Context, uins []uint, orgID uint) (map[uint][]*types.MemberDepartment, error) {
	return db.ListMemberDepartmentsByUinsAndOrgID(ctx, r.dao.DB(), uins, orgID)
}
