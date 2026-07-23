//go:build !enterprise

package oss

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	"github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/db"
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

type user struct {
	db *gorm.DB
}

func NewUser(db *gorm.DB) *user {
	return &user{db: db}
}

func (s *user) CreateUser(ctx context.Context, req *account.CreateUserInput) (*account.CreateUserResponse, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	if strings.TrimSpace(req.Name) == "" {
		return nil, accounterror.ErrInvalidArg
	}

	phone := strings.TrimSpace(req.Phone)
	email := strings.TrimSpace(req.Email)

	if phone != "" {
		existing, err := db.GetUserByPhone(ctx, s.db, phone)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, accounterror.ErrInvalidArg
		}
	}
	if email != "" {
		existing, err := db.GetUserByEmail(ctx, s.db, email)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, accounterror.ErrInvalidArg
		}
	}

	user := &types.User{
		PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
		Name:     strings.TrimSpace(req.Name),
		Email:    email,
		Phone:    phone,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := db.CreateUser(ctx, tx, user); err != nil {
			return err
		}

		uin := user.ID
		existingUO, _ := db.GetUserOrgByUserID(ctx, tx, user.ID)
		if existingUO == nil {
			userOrg := &types.UserOrg{
				UserID:    user.ID,
				Uin:       uin,
				OrgID:     caller.OrgID,
				IsDefault: true,
			}
			if err := db.CreateUserOrg(ctx, tx, userOrg); err != nil {
				return err
			}
		}

		if len(req.DepartmentIDs) > 0 {
			for _, deptID := range req.DepartmentIDs {
				rel := &types.MemberDepartment{
					Uin:          uin,
					OrgID:        caller.OrgID,
					DepartmentID: deptID,
					IsPrimary:    true,
				}
				if err := db.CreateMemberDepartment(ctx, tx, rel); err != nil {
					return err
				}
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &account.CreateUserResponse{
		UserID: user.ID,
		Name:   user.Name,
		Email:  user.Email,
		Phone:  user.Phone,
		IsNew:  true,
	}, nil
}

func (s *user) GetUser(ctx context.Context, publicID string, phone string) (*account.UserInfo, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	var user *types.User
	var err error

	if publicID != "" {
		user, err = db.GetUserByPublicID(ctx, s.db, publicID)
	} else if phone != "" {
		user, err = db.GetUserByPhone(ctx, s.db, phone)
	} else {
		return nil, accounterror.ErrInvalidArg
	}

	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}

	avatarMap, _ := resolveSingleAvatarMap(ctx, s.db, caller.OrgID, user.AvatarURL)
	return convertToContractUser(user, avatarMap), nil
}

func (s *user) UpdateUser(ctx context.Context, publicID string, req *account.UpdateUserInput) (*account.UserInfo, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	if strings.TrimSpace(publicID) == "" {
		return nil, accounterror.ErrInvalidArg
	}

	var user *types.User
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		user, err = db.GetUserByPublicID(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if user == nil {
			return accounterror.ErrUserNotFound
		}

		if strings.TrimSpace(req.Name) != "" {
			user.Name = strings.TrimSpace(req.Name)
		}
		if req.Email != nil {
			user.Email = strings.TrimSpace(*req.Email)
		}
		if strings.TrimSpace(req.AvatarURL) != "" {
			user.AvatarURL = strings.TrimSpace(req.AvatarURL)
		}

		return db.UpdateUser(ctx, tx, user)
	}); err != nil {
		return nil, err
	}

	return convertToContractUser(user, nil), nil
}

func (s *user) DeleteUser(ctx context.Context, publicID string) error {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return accounterror.ErrLoginRequired
	}
	if strings.TrimSpace(publicID) == "" {
		return accounterror.ErrInvalidArg
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		user, err := db.GetUserByPublicID(ctx, tx, publicID)
		if err != nil {
			return err
		}
		if user == nil {
			return accounterror.ErrUserNotFound
		}
		return db.DeleteUser(ctx, tx, user.ID)
	})
}

func (s *user) ListUser(ctx context.Context, req *account.ListUserInput) (*account.UserList, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}
	req.Fill()

	opt := types.NewPageQuery(*caller, req.Offset, req.Limit)
	opt.ListAll = req.ListAll
	if req.Keyword != nil && *req.Keyword != "" {
		opt.AddFilter("keyword", *req.Keyword)
	}
	if req.DepartmentID != nil {
		opt.AddFilter("department_id", strconv.FormatUint(uint64(*req.DepartmentID), 10))
	}

	users, total, err := db.ListUser(ctx, s.db, opt)
	if err != nil {
		return nil, err
	}

	avatarMap := resolveAvatarURLs(ctx, s.db, caller.OrgID, users)
	items := make([]account.UserInfo, 0, len(users))
	for _, user := range users {
		items = append(items, *convertToContractUser(user, avatarMap))
	}

	if len(items) > 0 {
		userIDs := make([]uint, len(items))
		for i, item := range items {
			userIDs[i] = item.ID
		}
		uinMap := make(map[uint]uint, len(userIDs))
		if userOrgs, _ := db.GetUserOrgsByUserIDsAndOrgID(ctx, s.db, userIDs, caller.OrgID); userOrgs != nil {
			for _, uo := range userOrgs {
				uinMap[uo.UserID] = uo.Uin
			}
		}
		uins := make([]uint, 0, len(uinMap))
		for _, uin := range uinMap {
			uins = append(uins, uin)
		}
		relMap, _ := db.ListMemberDepartmentsByUinsAndOrgID(ctx, s.db, uins, caller.OrgID)
		deptIDSet := make(map[uint]struct{})
		for _, rels := range relMap {
			for _, rel := range rels {
				deptIDSet[rel.DepartmentID] = struct{}{}
			}
		}
		deptIDs := make([]uint, 0, len(deptIDSet))
		for id := range deptIDSet {
			deptIDs = append(deptIDs, id)
		}
		deptMap := make(map[uint]*types.Department)
		if depts, _ := db.GetDepartmentsByIDs(ctx, s.db, deptIDs); depts != nil {
			for _, dept := range depts {
				deptMap[dept.ID] = dept
			}
		}
		for i := range items {
			uin := uinMap[items[i].ID]
			rels := relMap[uin]
			depts := make([]account.OrgMemberDepartment, 0, len(rels))
			for _, rel := range rels {
				name := ""
				if d, ok := deptMap[rel.DepartmentID]; ok {
					name = d.Name
				}
				depts = append(depts, account.OrgMemberDepartment{
					ID:           rel.ID,
					DepartmentID: rel.DepartmentID,
					Name:         name,
					IsPrimary:    rel.IsPrimary,
				})
			}
			items[i].Departments = depts
		}
	}

	return &account.UserList{
		Total:  total,
		Offset: req.Offset,
		Limit:  req.Limit,
		Items:  items,
	}, nil
}

func (s *user) GetUserByID(ctx context.Context, id uint) (*account.UserInfo, error) {
	user, err := db.GetUserByID(ctx, s.db, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}
	avatarMap, _ := resolveSingleAvatarMap(ctx, s.db, 0, user.AvatarURL)
	info := convertToContractUser(user, avatarMap)
	if userOrg, _ := db.GetUserOrgByUin(ctx, s.db, user.ID); userOrg != nil {
		info.Uin = userOrg.Uin
	}
	return info, nil
}

func (s *user) GetUserByUin(ctx context.Context, uin uint) (*account.UserInfo, error) {
	user, err := db.GetUserByUin(ctx, s.db, uin)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}
	avatarMap, _ := resolveSingleAvatarMap(ctx, s.db, 0, user.AvatarURL)
	info := convertToContractUser(user, avatarMap)
	info.Uin = uin
	return info, nil
}

func (s *user) GetUserByGithubID(ctx context.Context, githubID int64) (*account.UserInfo, error) {
	return nil, accounterror.ErrNotImplementedEdition
}

func (s *user) GetUsersByIDs(ctx context.Context, ids []uint) ([]*account.UserInfo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	users, err := db.GetUsersByIDs(ctx, s.db, ids)
	if err != nil {
		return nil, err
	}
	avatarMap := resolveAvatarURLs(ctx, s.db, 0, users)
	items := make([]*account.UserInfo, len(users))
	for i, user := range users {
		items[i] = convertToContractUser(user, avatarMap)
	}
	return items, nil
}

func (s *user) GetUsersByUins(ctx context.Context, uins []uint) (map[uint]*account.UserInfo, error) {
	if len(uins) == 0 {
		return nil, nil
	}
	userMap, err := db.GetUsersByUins(ctx, s.db, uins)
	if err != nil {
		return nil, err
	}
	users := make([]*types.User, 0, len(userMap))
	for _, user := range userMap {
		if user != nil {
			users = append(users, user)
		}
	}
	avatarMap := resolveAvatarURLs(ctx, s.db, 0, users)
	result := make(map[uint]*account.UserInfo, len(userMap))
	for uin, user := range userMap {
		if user == nil {
			continue
		}
		info := convertToContractUser(user, avatarMap)
		info.Uin = uin
		result[uin] = info
	}
	return result, nil
}

func (s *user) GetUinMapByPublicIDs(ctx context.Context, orgID uint, publicIDs []string) (map[string]uint, error) {
	return db.GetPublicIDUinMapByPublicIDs(ctx, s.db, orgID, publicIDs)
}

func (s *user) ListUin(ctx context.Context) (*account.ListUinOutput, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	userOrg, err := db.GetUserOrgByUin(ctx, s.db, caller.Uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, accounterror.ErrUserNotFound
	}

	user, err := db.GetUserByID(ctx, s.db, userOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}

	uins := make([]account.UinInfo, 0, 1)
	uinInfo := account.UinInfo{
		Uin:         userOrg.Uin,
		UserID:      userOrg.UserID,
		SubjectType: "company",
		SubjectID:   userOrg.OrgID,
		Name:        user.Name,
		UinStatus:   "normal",
	}

	org, err := db.GetOrgByID(ctx, s.db, userOrg.OrgID)
	if err != nil {
		return nil, err
	}
	if org != nil {
		uinInfo.CompanyName = org.Name
		uinInfo.CompanyLogo = org.Logo
		uinInfo.CompanyStatus = org.Status
	}

	uins = append(uins, uinInfo)
	return &account.ListUinOutput{Uin: uins}, nil
}

func (s *user) GetUsersByPublicIDs(ctx context.Context, publicIDs []string) ([]*account.UserInfo, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	users, err := db.GetUsersByPublicIDs(ctx, s.db, publicIDs)
	if err != nil {
		return nil, err
	}
	avatarMap := resolveAvatarURLs(ctx, s.db, 0, users)
	items := make([]*account.UserInfo, len(users))
	for i, user := range users {
		items[i] = convertToContractUser(user, avatarMap)
	}
	return items, nil
}

func convertToContractUser(user *types.User, avatarURLMap map[string]string) *account.UserInfo {
	if user == nil {
		return nil
	}
	avatarURL := user.AvatarURL
	if account.IsFilePublicID(avatarURL) {
		if resolved, ok := avatarURLMap[avatarURL]; ok {
			avatarURL = resolved
		}
	}
	return &account.UserInfo{
		ID:        user.ID,
		PublicID:  user.PublicID,
		Name:      user.Name,
		Email:     user.Email,
		Phone:     user.Phone,
		AvatarURL: avatarURL,
		CreatedAt: user.CreatedAt,
		UpdatedAt: user.UpdatedAt,
	}
}
