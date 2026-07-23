//go:build enterprise

package enterprise

import (
	"strconv"
	"time"

	"github.com/insmtx/Leros/backend/internal/adapter/account"
)

// ─── IAM Request Types ─────────────────────────────────────────────────────────

type iamRegisterByEmailReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
	Issuer   string `json:"issuer"`
}

type iamLoginByPasswordReq struct {
	Account    string `json:"account"`
	Password   string `json:"password"`
	DomainName string `json:"domain_name"`
}

type iamSendSmsCodeReq struct {
	Phone string `json:"phone"`
	Scene string `json:"scene"`
}

type iamLoginByPhoneCodeReq struct {
	Phone      string `json:"phone"`
	Code       string `json:"code"`
	DomainName string `json:"domain_name"`
}

type iamChooseUinReq struct {
	RefreshToken string `json:"refresh_token"`
	UinID        uint   `json:"uin_id"`
	UserID       uint   `json:"user_id"`
	LoginWay     int    `json:"login_way"`
	Issuer       string `json:"issuer"`
}

type iamSwitchLoginReq struct {
	Uin      uint `json:"uin"`
	LoginWay int  `json:"login_way"`
}

type iamCreateCompanyReq struct {
	DomainName      string `json:"domain_name"`
	RefreshToken    string `json:"refresh_token"`
	UserID          uint   `json:"user_id"`
	CompanyName     string `json:"company_name"`
	UserDisplayName string `json:"user_display_name"`
}

type iamEditCompanyInfoReq struct {
	CompanyID   uint   `json:"company_id"`
	Name        string `json:"name"`
	Alias       string `json:"alias"`
	Description string `json:"description"`
	Logo        string `json:"logo"`
	Address     string `json:"address"`
	Tel         string `json:"tel"`
	Email       string `json:"email"`
	Website     string `json:"website"`
}

type iamUpdateUserInfoReq struct {
	Name      string  `json:"name"`
	AvatarURL string  `json:"avatar_url"`
	Email     *string `json:"email"`
}

type iamGetDepartmentTreeReq struct {
	IncludeEmployee bool   `json:"include_employee"`
	Offset          int    `json:"offset"`
	Limit           int    `json:"limit"`
	Keyword         string `json:"keyword"`
	DepartmentIDs   []uint `json:"department_ids,omitempty"`
}

type iamCreateDepartmentEmployeeReq struct {
	Uin           uint   `json:"uin"`
	UserName      string `json:"user_name"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	EmployeeID    uint   `json:"employee_id"`
	Role          string `json:"role"`
	DepartmentIDs []uint `json:"department_ids"`
}

type iamEditDepartmentEmployeeReq struct {
	Uin           uint   `json:"uin"`
	UserName      string `json:"user_name"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	EmployeeID    uint   `json:"employee_id"`
	Role          string `json:"role"`
	DepartmentIDs []uint `json:"department_ids"`
}

// ─── IAM Response Types ────────────────────────────────────────────────────────

type iamLoginThirdResponseBody struct {
	UserID       uint          `json:"user_id"`
	LoginStatus  string        `json:"login_status"`
	UserInfo     *iamUserInfo  `json:"user_info,omitempty"`
	JwtToken     string        `json:"jwt_token,omitempty"`
	FailedReason string        `json:"failed_reason,omitempty"`
	Uin          []iamLoginUin `json:"uin,omitempty"`
	Issuer       string        `json:"issuer,omitempty"`
	RefreshToken string        `json:"refresh_token,omitempty"`
	LoginWay     int           `json:"login_way"`
}

type iamUserInfo struct {
	Identify  string `json:"identify"`
	AvatarURL string `json:"avatar_url"`
	Bio       string `json:"bio"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
	Name      string `json:"name"`
	ID        uint   `json:"id"`
	Uin       uint   `json:"uin"`
}

type iamLoginUin struct {
	Uin            iamUinInfo `json:"uin"`
	Name           string     `json:"name,omitempty"`
	CompanyLogo    string     `json:"company_logo,omitempty"`
	CompanyName    string     `json:"company_name,omitempty"`
	Role           string     `json:"role,omitempty"`
	CompanyStatus  string     `json:"company_status,omitempty"`
	LastLoginAt    *time.Time `json:"last_login_at,omitempty"`
	CompanyUserID  uint       `json:"company_user_id,omitempty"`
	CreatedByUin   uint       `json:"created_by_uin,omitempty"`
	CreatedByUserID uint      `json:"created_by_user_id,omitempty"`
}

type iamUinInfo struct {
	ID          uint      `json:"ID"`
	CreatedAt   time.Time `json:"CreatedAt"`
	UserID      uint      `json:"user_id"`
	SubjectType string    `json:"subject_type"`
	SubjectID   uint      `json:"subject_id"`
	UinStatus   string    `json:"uin_status"`
	Issuer      string    `json:"issuer"`
	Name        string    `json:"name"`
	LastLoginAt *time.Time `json:"last_login_at"`
}

type iamSendSmsCodeResponseBody struct {
	Phone       string `json:"phone"`
	ExpiresIn   int64  `json:"expires_in"`
	ResendAfter int64  `json:"resend_after"`
}

type iamChooseUinResponseBody struct {
	JWTToken          string    `json:"jwt_token"`
	AccessTokenExpire time.Time `json:"access_token_expire"`
	UIN               uint      `json:"uin"`
}

type iamSwitchLoginResponseBody struct {
	JWTToken string `json:"jwt_token"`
}

type iamCreateCompanyResponseBody struct {
	Uin           iamUinInfo `json:"uin"`
	Name          string     `json:"name"`
	CompanyLogo   string     `json:"company_logo"`
	CompanyName   string     `json:"company_name"`
	Role          string     `json:"role"`
	CompanyStatus string     `json:"company_status"`
	LastLoginAt   *time.Time `json:"last_login_at,omitempty"`
	CompanyUserID uint       `json:"company_user_id"`
}

type iamDetailPersonalCenterResponseBody struct {
	UserInfo       iamUserInfo     `json:"user_info"`
	CompanyInfo    iamCompanyInfo  `json:"company_info"`
	EmployeeDetail iamEmployeeInfo `json:"employee_detail"`
}

type iamCompanyInfo struct {
	ID              uint   `json:"id"`
	Name            string `json:"name"`
	Alias           string `json:"alias"`
	Description     string `json:"description"`
	Logo            string `json:"logo"`
	Address         string `json:"address"`
	Tel             string `json:"tel"`
	Email           string `json:"email"`
	Website         string `json:"website"`
	CompanyStatus   string `json:"company_status"`
	UserID          uint   `json:"user_id"`
	CreatedByUin    uint   `json:"created_by_uin"`
	CreatedByUserID uint   `json:"created_by_user_id"`
}

type iamEmployeeInfo struct {
	CompanyID uint   `json:"company_id"`
	UserID    uint   `json:"user_id"`
	Uin       uint   `json:"uin"`
	SysRole   string `json:"sys_role"`
	UserName  string `json:"user_name"`
	RealName  string `json:"real_name"`
	Phone     string `json:"phone"`
	Email     string `json:"email"`
}

type iamDepartmentTreeEmployee struct {
	Uin           uint      `json:"uin"`
	CreatedAt     time.Time `json:"created_at"`
	UserName      string    `json:"user_name"`
	Name          string    `json:"name"`
	Email         string    `json:"email"`
	Phone         string    `json:"phone"`
	EmployeeID    uint      `json:"employee_id"`
	UserID        uint      `json:"user_id"`
	Role          string    `json:"role"`
	DepartmentIDs []uint    `json:"department_ids"`
}

type iamDepartmentTreeResponseBody struct {
	Employees []iamDepartmentTreeEmployee `json:"employees,omitempty"`
	Total     int64                       `json:"total"`
	Offset    int                         `json:"offset"`
	Limit     int                         `json:"limit"`
}

type iamListUinResponseBody struct {
	Uin []iamLoginUin `json:"uin"`
}

type iamCreateDepartmentEmployeeResponseBody struct {
	Employee *iamDepartmentTreeEmployee `json:"employee,omitempty"`
}

type iamEditDepartmentEmployeeResponseBody struct {
	Employee *iamDepartmentTreeEmployee `json:"employee,omitempty"`
}

// ─── Mapping Functions ─────────────────────────────────────────────────────────

func mapLoginThirdToAuthTokenResponse(resp *iamLoginThirdResponseBody) (*account.AuthTokens, error) {
	result := &account.AuthTokens{
		LoginStatus:  resp.LoginStatus,
		JwtToken:     resp.JwtToken,
		RefreshToken: resp.RefreshToken,
		LoginWay:     resp.LoginWay,
	}
	result.UserID = resp.UserID
	if len(resp.Uin) > 0 {
		result.Uin = resp.Uin[0].Uin.ID
	}
	if resp.UserInfo != nil {
		result.UserInfo = mapIAMUserInfoToAuthUserInfo(resp.UserInfo)
	}
	if len(resp.Uin) > 0 {
		result.Organizations = mapUinListToAuthOrgInfos(resp.Uin)
		if len(result.Organizations) > 0 {
			result.Org = result.Organizations[0]
		}
	} else {
		result.Organizations = make([]account.AuthOrgInfo, 0)
	}
	return result, nil
}

func mapIAMUserInfoToAuthUserInfo(info *iamUserInfo) account.AuthUserInfo {
	if info == nil {
		return account.AuthUserInfo{}
	}
	return account.AuthUserInfo{
		ID:        info.ID,
		Name:      info.Name,
		Email:     info.Email,
		Phone:     info.Phone,
		AvatarURL: info.AvatarURL,
	}
}

func mapUinListToAuthOrgInfos(uins []iamLoginUin) []account.AuthOrgInfo {
	infos := make([]account.AuthOrgInfo, 0, len(uins))
	for _, uin := range uins {
		infos = append(infos, account.AuthOrgInfo{
			ID:              uin.Uin.SubjectID,
			Code:            uin.CompanyName,
			Name:            uin.CompanyName,
			Logo:            uin.CompanyLogo,
			Uin:             uin.Uin.ID,
			CreatedByUin:    uin.CreatedByUin,
			CreatedByUserID: uin.CreatedByUserID,
		})
	}
	return infos
}

func mapDetailPersonalCenterToUserInfo(resp *iamDetailPersonalCenterResponseBody) *account.UserInfo {
	return &account.UserInfo{
		ID:        resp.UserInfo.ID,
		PublicID:  strconv.FormatUint(uint64(resp.UserInfo.ID), 10),
		Name:      resp.UserInfo.Name,
		Email:     resp.UserInfo.Email,
		Phone:     resp.UserInfo.Phone,
		AvatarURL: resp.UserInfo.AvatarURL,
	}
}

func mapDepartmentTreeEmployeeToUserInfo(emp iamDepartmentTreeEmployee) account.UserInfo {
	return account.UserInfo{
		ID:       emp.UserID,
		PublicID: strconv.FormatUint(uint64(emp.UserID), 10),
		Uin:      emp.Uin,
		Name:     emp.Name,
		Email:    emp.Email,
		Phone:    emp.Phone,
	}
}

func mapIAMCompanyToOrg(company *iamCompanyInfo) *account.Org {
	if company == nil {
		return nil
	}
	return &account.Org{
		PublicID:    strconv.FormatUint(uint64(company.ID), 10),
		Type:        "company",
		Code:        company.Name,
		Name:        company.Name,
		Status:      mapCompanyStatus(company.CompanyStatus),
		Description: company.Description,
		Logo:        company.Logo,
		Address:     company.Address,
		Website:     company.Website,
	}
}

func mapCompanyStatus(status string) string {
	switch status {
	case "passed":
		return "active"
	default:
		return status
	}
}

// ─── Department IAM Types ──────────────────────────────────────────────────────

type iamDepartmentPayload struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	ParentID  uint   `json:"parent_id"`
	Sort      uint   `json:"sort"`
	CompanyID uint   `json:"company_id"`
}

type iamCreateDepartmentReq struct {
	Name     string `json:"name"`
	ParentID uint   `json:"parent_id"`
	Sort     uint   `json:"sort"`
}

type iamCreateDepartmentResp struct {
	Department iamDepartmentPayload `json:"department"`
}

type iamGetDepartmentReq struct {
	ID uint `json:"id"`
}

type iamGetDepartmentResp struct {
	Department iamDepartmentPayload `json:"department"`
}

type iamUpdateDepartmentReq struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	ParentID uint   `json:"parent_id"`
	Sort     uint   `json:"sort"`
}

type iamUpdateDepartmentResp struct {
	Department iamDepartmentPayload `json:"department"`
}

type iamDeleteDepartmentReq struct {
	ID uint `json:"id"`
}

// ─── New IAM Types (Phase 2-4) ────────────────────────────────────────────────

type iamGetOrCreateUserReq struct {
	Phone string `json:"phone"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

type iamGetOrCreateUserResp struct {
	UserID uint   `json:"user_id"`
	Name   string `json:"name"`
	Email  string `json:"email"`
	Phone  string `json:"phone"`
	IsNew  bool   `json:"is_new"`
}

type iamDeleteUserReq struct {
	UserID uint `json:"user_id"`
}

type iamDeleteCompanyReq struct {
	CompanyID uint `json:"company_id"`
}

type iamListCompaniesReq struct {
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
	Keyword string `json:"keyword"`
	Status  string `json:"status"`
}

type iamCompanyPayload struct {
	ID            uint   `json:"id"`
	Name          string `json:"name"`
	Alias         string `json:"alias"`
	CompanyStatus string `json:"company_status"`
	UserID        uint   `json:"user_id"`
}

type iamListCompaniesResp struct {
	Companies []iamCompanyPayload `json:"companies"`
	Total     int64               `json:"total"`
	Offset    int                 `json:"offset"`
	Limit     int                 `json:"limit"`
}

type iamGetCompanyMemberReq struct {
	Uin uint `json:"uin"`
}

type iamGetCompanyMemberResp struct {
	Uin           uint   `json:"uin"`
	UserName      string `json:"user_name"`
	Name          string `json:"name"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	EmployeeID    uint   `json:"employee_id"`
	UserID        uint   `json:"user_id"`
	Role          string `json:"role"`
	DepartmentIDs []uint `json:"department_ids"`
}

type iamAuthSessionResp struct {
	UserInfo       iamUserInfo     `json:"user_info"`
	CompanyInfo    iamCompanyInfo  `json:"company_info"`
	EmployeeDetail iamEmployeeInfo `json:"employee_detail"`
	UinList        []iamLoginUin   `json:"uin_list"`
}

type iamListDepartmentReq struct {
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
	Keyword string `json:"keyword"`
}

type iamListDepartmentResp struct {
	Departments []iamDepartmentPayloadWithPaths `json:"departments"`
	Total       int64                           `json:"total"`
	Offset      int                             `json:"offset"`
	Limit       int                             `json:"limit"`
}

type iamDepartmentPayloadWithPaths struct {
	ID        uint      `json:"id"`
	Name      string    `json:"name"`
	ParentID  uint      `json:"parent_id"`
	ParentIDs []uint    `json:"parent_ids"`
	Sort      uint      `json:"sort"`
	CompanyID uint      `json:"company_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ─── Department Mapping Functions ──────────────────────────────────────────────

func mapIAMDepartmentToContract(dept iamDepartmentPayload) account.Department {
	return account.Department{
		ID:       dept.ID,
		Name:     dept.Name,
		ParentID: dept.ParentID,
		Sort:     dept.Sort,
		OrgID:    dept.CompanyID,
	}
}

func mapIAMCompanyPayloadToOrg(c iamCompanyPayload) account.Org {
	return account.Org{
		PublicID: strconv.FormatUint(uint64(c.ID), 10),
		Type:     "company",
		Name:     c.Name,
		Status:   mapCompanyStatus(c.CompanyStatus),
	}
}

func mapListDepartmentToContract(dept iamDepartmentPayloadWithPaths) account.Department {
	return account.Department{
		ID:        dept.ID,
		Name:      dept.Name,
		ParentID:  dept.ParentID,
		ParentIDs: dept.ParentIDs,
		Sort:      dept.Sort,
		OrgID:     dept.CompanyID,
		CreatedAt: dept.CreatedAt,
		UpdatedAt: dept.UpdatedAt,
	}
}
