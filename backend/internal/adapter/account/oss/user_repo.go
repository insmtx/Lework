//go:build !enterprise

package oss

import (
	"context"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type userRepo struct {
	dao *db.UserEntityDao
}

func newUserRepo(d *gorm.DB) *userRepo {
	return &userRepo{dao: db.NewUserEntityDao(d)}
}

func (r *userRepo) withTx(tx *gorm.DB) *userRepo {
	return &userRepo{dao: &db.UserEntityDao{GenericDao: r.dao.WithTx(tx)}}
}

func (r *userRepo) db() *gorm.DB {
	return r.dao.DB()
}

// GetByRef resolves a user by the first non-zero field in the reference.
func (r *userRepo) GetByRef(ctx context.Context, ref account.UserRef) (*types.User, error) {
	var cond *db.UserCond
	switch {
	case ref.ID != 0:
		cond = &db.UserCond{BaseCond: &db.BaseCond{ID: ref.ID}}
	case ref.PublicID != "":
		cond = &db.UserCond{PublicID: ref.PublicID}
	case ref.Email != "":
		cond = &db.UserCond{Email: ref.Email}
	case ref.Phone != "":
		cond = &db.UserCond{Phone: ref.Phone}
	default:
		return nil, nil
	}
	return r.dao.GetByCond(ctx, cond)
}

func (r *userRepo) GetByID(ctx context.Context, id uint) (*types.User, error) {
	return r.dao.GetByCond(ctx, &db.UserCond{BaseCond: &db.BaseCond{ID: id}})
}

func (r *userRepo) GetByEmail(ctx context.Context, email string) (*types.User, error) {
	return r.dao.GetByCond(ctx, &db.UserCond{Email: email})
}

func (r *userRepo) GetByPhone(ctx context.Context, phone string) (*types.User, error) {
	return r.dao.GetByCond(ctx, &db.UserCond{Phone: phone})
}

func (r *userRepo) GetByPublicID(ctx context.Context, publicID string) (*types.User, error) {
	return r.dao.GetByCond(ctx, &db.UserCond{PublicID: publicID})
}

func (r *userRepo) GetByIDs(ctx context.Context, ids []uint) ([]*types.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	return r.dao.ListByCond(ctx, &db.UserCond{BaseCond: &db.BaseCond{IDs: ids}})
}

func (r *userRepo) GetByPublicIDs(ctx context.Context, publicIDs []string) ([]*types.User, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	return r.dao.ListByCond(ctx, &db.UserCond{
		BaseCond: &db.BaseCond{
			Filters: []db.Filter{{Field: "public_id", Value: publicIDs, ExactMatch: true}},
		},
	})
}

func (r *userRepo) Create(ctx context.Context, user *types.User) error {
	return r.dao.Insert(ctx, user)
}

func (r *userRepo) Update(ctx context.Context, user *types.User) error {
	return r.dao.Update(ctx, user)
}

func (r *userRepo) Delete(ctx context.Context, id uint) error {
	return r.dao.Delete(ctx, id)
}

// GetByUin queries a user by organization member Uin (cross-table JOIN with user_org).
func (r *userRepo) GetByUin(ctx context.Context, uin uint) (*types.User, error) {
	return db.GetUserByUin(ctx, r.dao.DB(), uin)
}

// GetByUins batch-queries users by organization member Uins (cross-table JOIN with user_org).
func (r *userRepo) GetByUins(ctx context.Context, uins []uint) (map[uint]*types.User, error) {
	return db.GetUsersByUins(ctx, r.dao.DB(), uins)
}

// GetPublicIDUinMap returns a public_id -> uin mapping for the given org and public IDs.
func (r *userRepo) GetPublicIDUinMap(ctx context.Context, orgID uint, publicIDs []string) (map[string]uint, error) {
	return db.GetPublicIDUinMapByPublicIDs(ctx, r.dao.DB(), orgID, publicIDs)
}

// ListByOrg paginates users within an organization (complex JOIN + filtering with PageQuery).
func (r *userRepo) ListByOrg(ctx context.Context, opt *types.PageQuery) ([]*types.User, int64, error) {
	return db.ListUser(ctx, r.dao.DB(), opt)
}
