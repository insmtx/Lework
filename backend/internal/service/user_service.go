package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

var _ contract.UserService = (*userService)(nil)

type userService struct {
	db *gorm.DB
}

func NewUserService(d *gorm.DB) contract.UserService {
	return &userService{db: d}
}

func (s *userService) CreateUser(ctx context.Context, req *contract.CreateUserRequest) (*contract.UserInfo, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, errors.New("name is required")
	}
	if strings.TrimSpace(req.GithubLogin) == "" {
		return nil, errors.New("github_login is required")
	}

	existing, err := db.GetUserByGithubLogin(ctx, s.db, strings.TrimSpace(req.GithubLogin))
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, errors.New("github_login already exists")
	}

	user := &types.User{
		PublicID:    fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
		GithubLogin: strings.TrimSpace(req.GithubLogin),
		Name:        strings.TrimSpace(req.Name),
		Email:       strings.TrimSpace(req.Email),
		AvatarURL:   strings.TrimSpace(req.AvatarURL),
		Bio:         strings.TrimSpace(req.Bio),
		Company:     strings.TrimSpace(req.Company),
		Location:    strings.TrimSpace(req.Location),
	}

	if strings.TrimSpace(req.Password) != "" {
		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		user.Password = string(hashedPassword)
	}

	if err := db.CreateUser(ctx, s.db, user); err != nil {
		return nil, err
	}

	return convertToContractUser(user), nil
}

func (s *userService) GetUser(ctx context.Context, publicID string, githubLogin string) (*contract.UserInfo, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}

	var user *types.User
	var err error

	if publicID != "" {
		user, err = db.GetUserByPublicID(ctx, s.db, publicID)
	} else if githubLogin != "" {
		user, err = db.GetUserByGithubLogin(ctx, s.db, githubLogin)
	} else {
		return nil, errors.New("public_id or github_login is required")
	}

	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("user not found")
	}

	return convertToContractUser(user), nil
}

func (s *userService) UpdateUser(ctx context.Context, publicID string, req *contract.UpdateUserRequest) (*contract.UserInfo, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, errors.New("public_id is required")
	}

	var user *types.User
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		user, err = db.GetUserByPublicID(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if user == nil {
			return errors.New("user not found")
		}

		if req.GithubLogin != nil {
			user.GithubLogin = strings.TrimSpace(*req.GithubLogin)
		}
		if req.Password != nil && strings.TrimSpace(*req.Password) != "" {
			hashedPassword, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
			if err != nil {
				return err
			}
			user.Password = string(hashedPassword)
		}
		if req.Name != nil {
			user.Name = strings.TrimSpace(*req.Name)
		}
		if req.Email != nil {
			user.Email = strings.TrimSpace(*req.Email)
		}
		if req.AvatarURL != nil {
			user.AvatarURL = strings.TrimSpace(*req.AvatarURL)
		}
		if req.Bio != nil {
			user.Bio = strings.TrimSpace(*req.Bio)
		}
		if req.Company != nil {
			user.Company = strings.TrimSpace(*req.Company)
		}
		if req.Location != nil {
			user.Location = strings.TrimSpace(*req.Location)
		}

		return db.UpdateUser(ctx, tx, user)
	}); err != nil {
		return nil, err
	}

	return convertToContractUser(user), nil
}

func (s *userService) DeleteUser(ctx context.Context, publicID string) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return errors.New("user not authenticated")
	}
	if strings.TrimSpace(publicID) == "" {
		return errors.New("public_id is required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := db.GetUserByPublicID(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if user == nil {
			return errors.New("user not found")
		}
		return db.DeleteUser(ctx, tx, user.ID)
	})
}

func (s *userService) ListUsers(ctx context.Context, req *contract.ListUsersRequest) (*contract.UserList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, errors.New("user not authenticated")
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.GithubLogin != nil && *req.GithubLogin != "" {
		opt.AddExactFilter("github_login", *req.GithubLogin)
	}

	users, total, err := db.ListUsers(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	items := make([]contract.UserInfo, 0, len(users))
	for _, user := range users {
		items = append(items, *convertToContractUser(user))
	}
	return &contract.UserList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func convertToContractUser(user *types.User) *contract.UserInfo {
	if user == nil {
		return nil
	}
	return &contract.UserInfo{
		PublicID:    user.PublicID,
		GithubID:    user.GithubID,
		GithubLogin: user.GithubLogin,
		Name:        user.Name,
		Email:       user.Email,
		Phone:       user.Phone,
		AvatarURL:   user.AvatarURL,
		Bio:         user.Bio,
		Company:     user.Company,
		Location:    user.Location,
		PublicRepos: user.PublicRepos,
		Followers:   user.Followers,
		CreatedAt:   user.CreatedAt,
		UpdatedAt:   user.UpdatedAt,
	}
}
