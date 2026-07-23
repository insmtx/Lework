//go:build !enterprise

package oss

import (
	"context"

	"gorm.io/gorm"

	infra_db "github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
)

type userStore struct {
	db *gorm.DB
}

func newUserStore(db *gorm.DB) *userStore { return &userStore{db: db} }

func (s *userStore) Get(ctx context.Context, ref UserRef) (*types.User, error) {
	switch {
	case ref.ID != 0:
		return infra_db.GetUserByID(ctx, s.db, ref.ID)
	case ref.PublicID != "":
		return infra_db.GetUserByPublicID(ctx, s.db, ref.PublicID)
	case ref.Email != "":
		return infra_db.GetUserByEmail(ctx, s.db, ref.Email)
	case ref.Phone != "":
		return infra_db.GetUserByPhone(ctx, s.db, ref.Phone)
	}
	return nil, nil
}

func (s *userStore) GetByIDs(ctx context.Context, ids []uint) ([]*types.User, error) {
	return infra_db.GetUsersByIDs(ctx, s.db, ids)
}

func (s *userStore) Create(ctx context.Context, u *types.User) (*types.User, error) {
	if err := infra_db.CreateUser(ctx, s.db, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *userStore) Update(ctx context.Context, u *types.User) (*types.User, error) {
	if err := infra_db.UpdateUser(ctx, s.db, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (s *userStore) Delete(ctx context.Context, id uint) error {
	return infra_db.DeleteUser(ctx, s.db, id)
}
