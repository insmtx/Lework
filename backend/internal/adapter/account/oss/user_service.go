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
	"github.com/insmtx/Leros/backend/types"
	"github.com/ygpkg/yg-go/encryptor/snowflake"
)

type user struct {
	db             *gorm.DB
	userRepo       *userRepo
	userOrgRepo    *userOrgRepo
	orgRepo        *orgRepo
	departmentRepo *departmentRepo
	memberDeptRepo *memberDeptRepo
}

func NewUser(d *gorm.DB) *user {
	return &user{
		db:             d,
		userRepo:       newUserRepo(d),
		userOrgRepo:    newUserOrgRepo(d),
		orgRepo:        newOrgRepo(d),
		departmentRepo: newDepartmentRepo(d),
		memberDeptRepo: newMemberDeptRepo(d),
	}
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
		existing, err := s.userRepo.GetByPhone(ctx, phone)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, accounterror.ErrPhoneAlreadyExists
		}
	}
	if email != "" {
		existing, err := s.userRepo.GetByEmail(ctx, email)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return nil, accounterror.ErrEmailAlreadyExists
		}
	}

	user := &types.User{
		PublicID: fmt.Sprintf("usr_%s", snowflake.GenerateIDBase58()),
		Name:     strings.TrimSpace(req.Name),
		Email:    email,
		Phone:    phone,
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.userRepo.withTx(tx).Create(ctx, user); err != nil {
			return err
		}

		existingUO, _ := s.userOrgRepo.withTx(tx).ListByUserID(ctx, user.ID)
		if len(existingUO) == 0 {
			userOrg := &types.UserOrg{
				UserID:    user.ID,
				OrgID:     caller.OrgID,
				IsDefault: true,
			}
			if err := s.userOrgRepo.withTx(tx).Create(ctx, userOrg); err != nil {
				return err
			}
			if len(req.DepartmentIDs) > 0 {
				for i, deptID := range req.DepartmentIDs {
					rel := &types.MemberDepartment{
						Uin:          userOrg.ID,
						OrgID:        caller.OrgID,
						DepartmentID: deptID,
						IsPrimary:    i == 0,
					}
					if err := s.memberDeptRepo.withTx(tx).Create(ctx, rel); err != nil {
						return err
					}
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
		user, err = s.userRepo.GetByPublicID(ctx, publicID)
	} else if phone != "" {
		user, err = s.userRepo.GetByPhone(ctx, phone)
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
		user, err = s.userRepo.withTx(tx).GetByPublicID(ctx, publicID)
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

		return s.userRepo.withTx(tx).Update(ctx, user)
	}); err != nil {
		return nil, err
	}

	return convertToContractUser(user, nil), nil
}

func (s *user) UpdateCurrentUser(ctx context.Context, req *account.UpdateCurrentUserInput) (*account.UserInfo, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	var user *types.User
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		user, err = s.userRepo.withTx(tx).GetByUin(ctx, caller.Uin)
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

		return s.userRepo.withTx(tx).Update(ctx, user)
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
		user, err := s.userRepo.withTx(tx).GetByPublicID(ctx, publicID)
		if err != nil {
			return err
		}
		if user == nil {
			return accounterror.ErrUserNotFound
		}
		return s.userRepo.withTx(tx).Delete(ctx, user.ID)
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

	users, total, err := s.userRepo.ListByOrg(ctx, opt)
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
		if userOrgs, _ := s.userOrgRepo.GetByUserIDsAndOrgID(ctx, userIDs, caller.OrgID); userOrgs != nil {
			for _, uo := range userOrgs {
				uinMap[uo.UserID] = uo.ID
			}
		}
		uins := make([]uint, 0, len(uinMap))
		for _, uin := range uinMap {
			uins = append(uins, uin)
		}
		relMap, _ := s.memberDeptRepo.ListByUinsAndOrgID(ctx, uins, caller.OrgID)
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
		if depts, _ := s.departmentRepo.GetByIDs(ctx, deptIDs); depts != nil {
			for _, dept := range depts {
				deptMap[dept.ID] = dept
			}
		}
		for i := range items {
			uin := uinMap[items[i].ID]
			items[i].Uin = uin
			rels := relMap[uin]
			depts := make([]account.OrgMemberDepartment, 0, len(rels))
			for _, rel := range rels {
				name := ""
				if d, ok := deptMap[rel.DepartmentID]; ok {
					name = d.Name
				}
				depts = append(depts, account.OrgMemberDepartment{
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
	user, err := s.userRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}
	avatarMap, _ := resolveSingleAvatarMap(ctx, s.db, 0, user.AvatarURL)
	info := convertToContractUser(user, avatarMap)
	if userOrg, _ := s.userOrgRepo.GetByUin(ctx, user.ID); userOrg != nil {
		info.Uin = userOrg.ID
	}
	return info, nil
}

func (s *user) GetUserByUin(ctx context.Context, uin uint) (*account.UserInfo, error) {
	user, err := s.userRepo.GetByUin(ctx, uin)
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
	users, err := s.userRepo.GetByIDs(ctx, ids)
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
	userMap, err := s.userRepo.GetByUins(ctx, uins)
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
	return s.userRepo.GetPublicIDUinMap(ctx, orgID, publicIDs)
}

func (s *user) ListUin(ctx context.Context) (*account.ListUinOutput, error) {
	caller, _ := auth.FromContext(ctx)
	if caller == nil || caller.Uin == 0 {
		return nil, accounterror.ErrLoginRequired
	}

	userOrg, err := s.userOrgRepo.GetByUin(ctx, caller.Uin)
	if err != nil {
		return nil, err
	}
	if userOrg == nil {
		return nil, accounterror.ErrUserNotFound
	}

	user, err := s.userRepo.GetByID(ctx, userOrg.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, accounterror.ErrUserNotFound
	}

	uins := make([]account.UinInfo, 0, 1)
	uinInfo := account.UinInfo{
		Uin:         userOrg.ID,
		UserID:      userOrg.UserID,
		SubjectType: "company",
		SubjectID:   userOrg.OrgID,
		Name:        user.Name,
		UinStatus:   "normal",
	}

	org, err := s.orgRepo.GetByID(ctx, userOrg.OrgID)
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
	users, err := s.userRepo.GetByPublicIDs(ctx, publicIDs)
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
		CreatedAt: &user.CreatedAt,
		UpdatedAt: &user.UpdatedAt,
	}
}
