//go:build enterprise

package enterprise

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"gorm.io/gorm"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/insmtx/Leros/backend/internal/infra/filestore"
	"github.com/insmtx/Leros/backend/pkg/accounterror"
)

type org struct {
	db           *gorm.DB
	client       *iamClient
	provisioning account.WorkerProvisioner
}

func NewOrg(database *gorm.DB, client *iamClient, provisioning account.WorkerProvisioner) *org {
	return &org{db: database, client: client, provisioning: provisioning}
}

var errCreateOrgUnsupported = errors.New("CreateOrg is deprecated, use auth.CreateOrganization instead")

func (s *org) CreateOrg(ctx context.Context, req *account.CreateOrgInput) (*account.Org, error) {
	return nil, errCreateOrgUnsupported
}

func (s *org) GetOrg(ctx context.Context, publicID string, code string) (*account.Org, error) {
	var resp iamDetailPersonalCenterResponseBody
	if err := s.client.callWithAuth(ctx, "account.DetailPersonalCenter", nil, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return mapIAMCompanyToOrg(&resp.CompanyInfo), nil
}

func (s *org) UpdateOrg(ctx context.Context, publicID string, req *account.UpdateOrgInput) (*account.Org, error) {
	var detailResp iamDetailPersonalCenterResponseBody
	if err := s.client.callWithAuth(ctx, "account.DetailPersonalCenter", nil, &detailResp); err != nil {
		return nil, mapIAMError(err)
	}

	iamReq := iamEditCompanyInfoReq{
		CompanyID: detailResp.CompanyInfo.ID,
		Name:      detailResp.CompanyInfo.Name,
	}
	if req.Name != nil {
		iamReq.Name = *req.Name
	}
	if req.Description != nil {
		iamReq.Description = *req.Description
	}
	if req.Logo != nil {
		logoURL, err := s.resolveOrgLogo(ctx, *req.Logo)
		if err != nil {
			return nil, fmt.Errorf("resolve org logo: %w", err)
		}
		iamReq.Logo = logoURL
	}
	if req.Address != nil {
		iamReq.Address = *req.Address
	}
	if req.Website != nil {
		iamReq.Website = *req.Website
	}

	if err := s.client.callWithAuth(ctx, "account.EditCompanyInfo", &iamReq, nil); err != nil {
		return nil, mapIAMError(err)
	}

	var updatedResp iamDetailPersonalCenterResponseBody
	if err := s.client.callWithAuth(ctx, "account.DetailPersonalCenter", nil, &updatedResp); err != nil {
		return nil, mapIAMError(err)
	}
	return mapIAMCompanyToOrg(&updatedResp.CompanyInfo), nil
}

func (s *org) DeleteOrg(ctx context.Context, publicID string) error {
	companyID, err := strconv.ParseUint(publicID, 10, 64)
	if err != nil {
		return accounterror.ErrInvalidArg
	}
	return s.client.callWithAuth(ctx, "account.DeleteCompany", &iamDeleteCompanyReq{CompanyID: uint(companyID)}, nil)
}

func (s *org) ListOrgs(ctx context.Context, req *account.ListOrgsInput) (*account.OrgList, error) {
	var resp iamListCompaniesResp
	if err := s.client.callWithAuth(ctx, "account.ListCompanies", &iamListCompaniesReq{
		Offset:  req.Offset,
		Limit:   req.Limit,
		Keyword: "",
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	items := make([]account.Org, 0, len(resp.Companies))
	for _, c := range resp.Companies {
		items = append(items, mapIAMCompanyPayloadToOrg(c))
	}

	return &account.OrgList{
		Total:  resp.Total,
		Offset: resp.Offset,
		Limit:  resp.Limit,
		Items:  items,
	}, nil
}

func (s *org) CreateOrgMember(ctx context.Context, req *account.CreateOrgMemberInput) (*account.OrgMember, error) {
	var resp iamCreateDepartmentEmployeeResponseBody
	if err := s.client.callWithAuth(ctx, "account.CreateDepartmentEmployee", &iamCreateDepartmentEmployeeReq{
		UserName: req.Name,
		Name:     req.Name,
		Phone:    req.Phone,
		Role:     "sys_employee",
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	if resp.Employee == nil {
		return nil, fmt.Errorf("employee not returned from IAM")
	}
	return &account.OrgMember{
		Uin:       resp.Employee.Uin,
		UserName:  resp.Employee.UserName,
		UserPhone: resp.Employee.Phone,
	}, nil
}

func (s *org) GetOrgMember(ctx context.Context, id uint, uin uint) (*account.OrgMember, error) {
	var resp iamGetCompanyMemberResp
	if err := s.client.callWithAuth(ctx, "account.GetCompanyMember", &iamGetCompanyMemberReq{
		Uin: uin,
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}
	return &account.OrgMember{
		Uin:       resp.Uin,
		UserName:  resp.UserName,
		UserPhone: resp.Phone,
	}, nil
}

func (s *org) UpdateOrgMember(ctx context.Context, id uint, req *account.UpdateOrgMemberInput) (*account.OrgMember, error) {
	var resp iamEditDepartmentEmployeeResponseBody
	editReq := iamEditDepartmentEmployeeReq{
		Uin:  id,
		Role: "sys_employee",
	}
	if req.Name != nil {
		editReq.Name = *req.Name
	}

	if err := s.client.callWithAuth(ctx, "account.EditDepartmentEmployee", &editReq, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	if resp.Employee == nil {
		return nil, fmt.Errorf("employee not returned from IAM")
	}
	return &account.OrgMember{
		Uin:       resp.Employee.Uin,
		UserName:  resp.Employee.UserName,
		UserPhone: resp.Employee.Phone,
	}, nil
}

func (s *org) ListOrgMembers(ctx context.Context, req *account.ListOrgMembersInput) (*account.OrgMemberList, error) {
	var resp iamDepartmentTreeResponseBody
	if err := s.client.callWithAuth(ctx, "account.GetDepartmentTree", &iamGetDepartmentTreeReq{
		IncludeEmployee: true,
		Offset:          req.Offset,
		Limit:           req.Limit,
	}, &resp); err != nil {
		return nil, mapIAMError(err)
	}

	items := make([]account.OrgMember, 0, len(resp.Employees))
	for _, emp := range resp.Employees {
		items = append(items, account.OrgMember{
			Uin:       emp.Uin,
			UserName:  emp.UserName,
			UserPhone: emp.Phone,
		})
	}

	return &account.OrgMemberList{
		Total:  resp.Total,
		Offset: resp.Offset,
		Limit:  resp.Limit,
		Items:  items,
	}, nil
}

func (s *org) resolveOrgLogo(ctx context.Context, logoURL string) (string, error) {
	if !account.IsFilePublicID(logoURL) {
		return logoURL, nil
	}

	caller, _ := localauth.FromContext(ctx)
	if caller == nil || caller.OrgID == 0 {
		return "", fmt.Errorf("unable to resolve org id from context")
	}

	reader, fileUpload, err := filestore.OpenFileByPublicID(ctx, s.db, caller.OrgID, logoURL)
	if err != nil {
		return "", fmt.Errorf("open logo file: %w", err)
	}
	defer reader.Close()

	iamURL, err := s.client.UploadFileByMultipart(ctx, "cu-image", fileUpload.OriginalName, reader, fileUpload.FileSize)
	if err != nil {
		return "", fmt.Errorf("upload logo to iam: %w", err)
	}

	return iamURL, nil
}
