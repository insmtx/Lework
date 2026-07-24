//go:build enterprise

package enterprise

import (
	"context"
	"fmt"
	"strconv"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
	"gorm.io/gorm"
)

type user struct {
	client *iamClient
	db     *gorm.DB
}

func NewUser(client *iamClient, db *gorm.DB) *user {
	return &user{client: client, db: db}
}

func (s *user) CreateUser(ctx context.Context, req *account.CreateUserInput) (*account.CreateUserResponse, error) {
	var resp iamCreateDepartmentEmployeeResponseBody
	if err := s.client.callWithAuth(ctx, "account.CreateDepartmentEmployee", &iamCreateDepartmentEmployeeReq{
		UserName:      req.Name,
		Name:          req.Name,
		Email:         req.Email,
		Phone:         req.Phone,
		DepartmentIDs: req.DepartmentIDs,
		Role:          "sys_employee",
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	var userID uint
	if resp.Employee != nil {
		userID = resp.Employee.UserID
	}

	return &account.CreateUserResponse{
		UserID: userID,
		Name:   req.Name,
		Email:  req.Email,
		Phone:  req.Phone,
		IsNew:  true,
	}, nil
}

func (s *user) GetUser(ctx context.Context, publicID string, phone string) (*account.UserInfo, error) {
	var resp iamDetailPersonalCenterResponseBody
	if err := s.client.callWithAuth(ctx, "account.DetailPersonalCenter", nil, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return mapDetailPersonalCenterToUserInfo(&resp), nil
}

func (s *user) UpdateUser(ctx context.Context, publicID string, req *account.UpdateUserInput) (*account.UserInfo, error) {
	iamReq := iamUpdateUserInfoReq{
		Name:      req.Name,
		Email:     req.Email,
		AvatarURL: req.AvatarURL,
	}
	if iamReq.AvatarURL != "" {
		avatarURL, err := s.resolveAvatarURL(ctx, iamReq.AvatarURL)
		if err != nil {
			return nil, fmt.Errorf("resolve avatar: %w", err)
		}
		iamReq.AvatarURL = avatarURL
	}

	if err := s.client.callWithAuth(ctx, "account.UpdateUserInfo", &iamReq, nil); err != nil {
		return nil, mapIAMError(err)
	}

	var resp iamDetailPersonalCenterResponseBody
	if err := s.client.callWithAuth(ctx, "account.DetailPersonalCenter", nil, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return mapDetailPersonalCenterToUserInfo(&resp), nil
}

func (s *user) resolveAvatarURL(ctx context.Context, avatarURL string) (string, error) {
	if !account.IsFilePublicID(avatarURL) {
		return avatarURL, nil
	}

	caller, _ := localauth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return "", fmt.Errorf("unable to resolve org id from context")
	}

	reader, fileUpload, err := filestore.OpenFileByPublicID(ctx, s.db, caller.OrgID, avatarURL)
	if err != nil {
		return "", fmt.Errorf("open avatar file: %w", err)
	}
	defer reader.Close()

	iamURL, err := s.client.UploadFileByMultipart(ctx, "cu-image", fileUpload.OriginalName, reader, fileUpload.FileSize)
	if err != nil {
		return "", fmt.Errorf("upload avatar to iam: %w", err)
	}

	return iamURL, nil
}

func (s *user) DeleteUser(ctx context.Context, publicID string) error {
	userID, err := strconv.ParseUint(publicID, 10, 64)
	if err != nil {
		return accounterror.ErrInvalidArg
	}
	return s.client.callWithAuth(ctx, "account.DeleteUser", &iamDeleteUserReq{UserID: uint(userID)}, nil)
}

func (s *user) ListUser(ctx context.Context, req *account.ListUserInput) (*account.UserList, error) {
	var resp iamDepartmentTreeResponseBody
	iamReq := iamGetDepartmentTreeReq{
		IncludeEmployee: true,
		Offset:          req.Offset,
		Limit:           req.Limit,
	}
	if req.Keyword != nil {
		iamReq.Keyword = *req.Keyword
	}
	if req.DepartmentID != nil {
		iamReq.DepartmentIDs = []uint{*req.DepartmentID}
	}
	if err := s.client.callWithAuth(ctx, "account.GetDepartmentTree", &iamReq, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	items := make([]account.UserInfo, 0, len(resp.Employees))
	for _, emp := range resp.Employees {
		info := mapDepartmentTreeEmployeeToUserInfo(emp)
		if len(emp.DepartmentIDs) > 0 {
			info.Departments = make([]account.OrgMemberDepartment, 0, len(emp.DepartmentIDs))
			for _, deptID := range emp.DepartmentIDs {
				info.Departments = append(info.Departments, account.OrgMemberDepartment{
					DepartmentID: deptID,
				})
			}
		}
		items = append(items, info)
	}

	return &account.UserList{
		Total:  resp.Total,
		Offset: resp.Offset,
		Limit:  resp.Limit,
		Items:  items,
	}, nil
}

func (s *user) loadUserMap(ctx context.Context) (map[uint]account.UserInfo, map[uint]account.UserInfo, error) {
	var resp iamDepartmentTreeResponseBody
	if err := s.client.callWithAuth(ctx, "account.GetDepartmentTree", &iamGetDepartmentTreeReq{
		IncludeEmployee: true,
	}, &resp); err != nil {
		return nil, nil, mapIAMError(err)
	}
	byUserID := make(map[uint]account.UserInfo, len(resp.Employees))
	byUin := make(map[uint]account.UserInfo, len(resp.Employees))
	for _, emp := range resp.Employees {
		info := mapDepartmentTreeEmployeeToUserInfo(emp)
		if emp.UserID != 0 {
			byUserID[emp.UserID] = info
		}
		byUin[emp.Uin] = info
	}
	return byUserID, byUin, nil
}

func (s *user) GetUserByID(ctx context.Context, id uint) (*account.UserInfo, error) {
	byUserID, _, err := s.loadUserMap(ctx)
	if err != nil {
		return nil, err
	}
	info, ok := byUserID[id]
	if !ok {
		return nil, accounterror.ErrUserNotFound
	}
	return &info, nil
}

func (s *user) GetUserByUin(ctx context.Context, uin uint) (*account.UserInfo, error) {
	var resp iamGetCompanyMemberResp
	if err := s.client.callWithAuth(ctx, "account.GetCompanyMember", &iamGetCompanyMemberReq{
		Uin: uin,
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return &account.UserInfo{
		ID:        resp.UserID,
		PublicID:  strconv.FormatUint(uint64(resp.UserID), 10),
		Uin:       resp.Uin,
		Name:      resp.UserName,
		Email:     resp.Email,
		Phone:     resp.Phone,
		AvatarURL: resp.AvatarURL,
	}, nil
}

func (s *user) GetUserByGithubID(ctx context.Context, githubID int64) (*account.UserInfo, error) {
	return nil, accounterror.ErrNotImplementedEdition
}

func (s *user) GetUsersByIDs(ctx context.Context, ids []uint) ([]*account.UserInfo, error) {
	byUserID, _, err := s.loadUserMap(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*account.UserInfo, 0, len(ids))
	for _, id := range ids {
		if info, ok := byUserID[id]; ok {
			copyInfo := info
			items = append(items, &copyInfo)
		}
	}
	return items, nil
}

func (s *user) GetUsersByUins(ctx context.Context, uins []uint) (map[uint]*account.UserInfo, error) {
	_, byUin, err := s.loadUserMap(ctx)
	if err != nil {
		return nil, err
	}
	result := make(map[uint]*account.UserInfo, len(uins))
	for _, uin := range uins {
		if info, ok := byUin[uin]; ok {
			copyInfo := info
			result[uin] = &copyInfo
		}
	}
	return result, nil
}

func (s *user) GetUsersByPublicIDs(ctx context.Context, publicIDs []string) ([]*account.UserInfo, error) {
	if len(publicIDs) == 0 {
		return nil, nil
	}
	byUserID, _, err := s.loadUserMap(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*account.UserInfo, 0, len(publicIDs))
	for _, pid := range publicIDs {
		id, err := strconv.ParseUint(pid, 10, 64)
		if err != nil {
			continue
		}
		if info, ok := byUserID[uint(id)]; ok {
			copyInfo := info
			result = append(result, &copyInfo)
		}
	}
	return result, nil
}

func (s *user) GetUinMapByPublicIDs(ctx context.Context, orgID uint, publicIDs []string) (map[string]uint, error) {
	if len(publicIDs) == 0 {
		return map[string]uint{}, nil
	}
	byUserID, _, err := s.loadUserMap(ctx)
	if err != nil {
		return nil, err
	}
	publicIDSet := make(map[string]struct{}, len(publicIDs))
	for _, id := range publicIDs {
		publicIDSet[id] = struct{}{}
	}
	result := make(map[string]uint, len(publicIDs))
	for _, info := range byUserID {
		if info.PublicID == "" {
			continue
		}
		if _, ok := publicIDSet[info.PublicID]; ok {
			result[info.PublicID] = info.Uin
		}
	}
	return result, nil
}

func (s *user) ListUin(ctx context.Context) (*account.ListUinOutput, error) {
	var resp iamListUinResponseBody
	if err := s.client.callWithAuth(ctx, "account.ListUin", nil, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	uins := make([]account.UinInfo, 0, len(resp.Uin))
	for _, u := range resp.Uin {
		if u.Uin.SubjectType != "company" {
			continue
		}
		uins = append(uins, account.UinInfo{
			Uin:           u.Uin.ID,
			UserID:        u.Uin.UserID,
			SubjectType:   u.Uin.SubjectType,
			SubjectID:     u.Uin.SubjectID,
			Name:          u.Uin.Name,
			UinStatus:     u.Uin.UinStatus,
			Issuer:        u.Uin.Issuer,
			CompanyName:   u.CompanyName,
			CompanyLogo:   u.CompanyLogo,
			CompanyStatus: u.CompanyStatus,
			Role:          u.Role,
			LastLoginAt:   u.LastLoginAt,
		})
	}

	return &account.ListUinOutput{Uin: uins}, nil
}
