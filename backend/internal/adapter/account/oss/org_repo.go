//go:build !enterprise

package oss

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type orgRepo struct {
	dao *db.OrgEntityDao
}

func newOrgRepo(d *gorm.DB) *orgRepo {
	return &orgRepo{dao: db.NewOrgEntityDao(d)}
}

func (r *orgRepo) withTx(tx *gorm.DB) *orgRepo {
	return &orgRepo{dao: &db.OrgEntityDao{GenericDao: r.dao.WithTx(tx)}}
}

func (r *orgRepo) db() *gorm.DB {
	return r.dao.DB()
}

// GetByRef resolves an organization by the first non-zero field in the reference.
func (r *orgRepo) GetByRef(ctx context.Context, ref account.OrgRef) (*types.Organization, error) {
	var cond *db.OrgCond
	switch {
	case ref.ID != 0:
		cond = &db.OrgCond{BaseCond: &db.BaseCond{ID: ref.ID}}
	case ref.PublicID != "":
		cond = &db.OrgCond{
			BaseCond: &db.BaseCond{
				Filters: []db.Filter{{Field: "public_id", Value: []string{ref.PublicID}, ExactMatch: true}},
			},
		}
	case ref.Code != "":
		cond = &db.OrgCond{Code: ref.Code}
	default:
		return nil, nil
	}
	return r.dao.GetByCond(ctx, cond)
}

func (r *orgRepo) GetByID(ctx context.Context, id uint) (*types.Organization, error) {
	return r.dao.GetByCond(ctx, &db.OrgCond{BaseCond: &db.BaseCond{ID: id}})
}

func (r *orgRepo) GetByPublicID(ctx context.Context, publicID string) (*types.Organization, error) {
	return r.dao.GetByCond(ctx, &db.OrgCond{
		BaseCond: &db.BaseCond{
			Filters: []db.Filter{{Field: "public_id", Value: []string{publicID}, ExactMatch: true}},
		},
	})
}

func (r *orgRepo) GetByCode(ctx context.Context, code string) (*types.Organization, error) {
	return r.dao.GetByCond(ctx, &db.OrgCond{Code: code})
}

func (r *orgRepo) GetByIDs(ctx context.Context, ids []uint) ([]*types.Organization, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.dao.ListByCond(ctx, &db.OrgCond{BaseCond: &db.BaseCond{IDs: ids}})
}

func (r *orgRepo) Create(ctx context.Context, org *types.Organization) error {
	return r.dao.Insert(ctx, org)
}

func (r *orgRepo) Update(ctx context.Context, org *types.Organization) error {
	return r.dao.Update(ctx, org)
}

func (r *orgRepo) Delete(ctx context.Context, id uint) error {
	return r.dao.Delete(ctx, id)
}

// List paginates organizations with filtering via PageQuery.
func (r *orgRepo) List(ctx context.Context, opt *types.PageQuery) ([]*types.Organization, int64, error) {
	return db.ListOrgs(ctx, r.dao.DB(), opt)
}
