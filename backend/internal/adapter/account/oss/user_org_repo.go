//go:build !enterprise

package oss

import (
	"context"
	"strconv"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type userOrgRepo struct {
	dao *db.UserOrgEntityDao
}

func newUserOrgRepo(d *gorm.DB) *userOrgRepo {
	return &userOrgRepo{dao: db.NewUserOrgEntityDao(d)}
}

func (r *userOrgRepo) withTx(tx *gorm.DB) *userOrgRepo {
	return &userOrgRepo{dao: &db.UserOrgEntityDao{GenericDao: r.dao.WithTx(tx)}}
}

func (r *userOrgRepo) db() *gorm.DB {
	return r.dao.DB()
}

// GetByRef resolves a user-org mapping by the first non-zero field in the reference.
func (r *userOrgRepo) GetByRef(ctx context.Context, ref account.UserOrgRef) (*types.UserOrg, error) {
	var cond *db.UserOrgCond
	switch {
	case ref.ID != 0:
		cond = &db.UserOrgCond{BaseCond: &db.BaseCond{ID: ref.ID}}
	case ref.ExternalUin != 0:
		cond = &db.UserOrgCond{ExternalUin: ref.ExternalUin}
	case ref.Uin != 0 && ref.OrgID != 0:
		cond = &db.UserOrgCond{Uin: ref.Uin, OrgID: ref.OrgID}
	case ref.Uin != 0:
		cond = &db.UserOrgCond{Uin: ref.Uin}
	default:
		return nil, nil
	}
	return r.dao.GetByCond(ctx, cond)
}

func (r *userOrgRepo) GetByID(ctx context.Context, id uint) (*types.UserOrg, error) {
	return r.dao.GetByCond(ctx, &db.UserOrgCond{BaseCond: &db.BaseCond{ID: id}})
}

func (r *userOrgRepo) GetByUin(ctx context.Context, uin uint) (*types.UserOrg, error) {
	return r.dao.GetByCond(ctx, &db.UserOrgCond{BaseCond: &db.BaseCond{ID: uin, OrderBy: "is_default DESC"}})
}

func (r *userOrgRepo) GetByUinAndOrgID(ctx context.Context, uin, orgID uint) (*types.UserOrg, error) {
	return r.dao.GetByCond(ctx, &db.UserOrgCond{BaseCond: &db.BaseCond{ID: uin}, OrgID: orgID})
}

func (r *userOrgRepo) GetByUserIDAndOrgID(ctx context.Context, userID, orgID uint) (*types.UserOrg, error) {
	return r.dao.GetByCond(ctx, &db.UserOrgCond{UserID: userID, OrgID: orgID})
}

func (r *userOrgRepo) ListByUserID(ctx context.Context, userID uint) ([]*types.UserOrg, error) {
	return r.dao.ListByCond(ctx, &db.UserOrgCond{UserID: userID})
}

func (r *userOrgRepo) GetByUserIDsAndOrgID(ctx context.Context, userIDs []uint, orgID uint) ([]*types.UserOrg, error) {
	if len(userIDs) == 0 {
		return nil, nil
	}
	ids := make([]string, len(userIDs))
	for i, id := range userIDs {
		ids[i] = strconv.FormatUint(uint64(id), 10)
	}
	return r.dao.ListByCond(ctx, &db.UserOrgCond{
		BaseCond: &db.BaseCond{
			Filters: []db.Filter{{Field: "user_id", Value: ids, ExactMatch: true}},
		},
		OrgID: orgID,
	})
}

func (r *userOrgRepo) Create(ctx context.Context, uo *types.UserOrg) error {
	return r.dao.Insert(ctx, uo)
}

func (r *userOrgRepo) Update(ctx context.Context, uo *types.UserOrg) error {
	return r.dao.Update(ctx, uo)
}

func (r *userOrgRepo) Delete(ctx context.Context, id uint) error {
	return r.dao.Delete(ctx, id)
}

func (r *userOrgRepo) CountByUserID(ctx context.Context, userID uint) (int64, error) {
	return r.dao.CountByCond(ctx, &db.UserOrgCond{UserID: userID})
}

// List paginates user-org mappings with filtering via PageQuery.
func (r *userOrgRepo) List(ctx context.Context, opt *types.PageQuery) ([]*types.UserOrg, int64, error) {
	return db.ListUserOrgs(ctx, r.dao.DB(), opt)
}
