package account

const (
	LoginStatusSuccess           = "success"
	LoginStatusNeedCreateCompany = "need_create_company"

	EditionOSS        = "oss"
	EditionEnterprise = "enterprise"
)

type RegisterByEmailInput struct {
	Email           string `json:"email"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Name            string `json:"name,omitempty"`
}

type LoginByPasswordInput struct {
	Account  string `json:"account"`
	Password string `json:"password"`
}

type LoginByPasswordOutput struct {
	UserID        uint          `json:"user_id"`
	RefreshToken  string        `json:"refresh_token"`
	Organizations []AuthOrgInfo `json:"organizations"`
	UserInfo      AuthUserInfo  `json:"user_info"`
	LoginWay      int           `json:"login_way"`
}

type SendPhoneLoginCodeInput struct {
	Phone string `json:"phone"`
}

type SendPhoneLoginCodeOutput struct {
	Phone       string `json:"phone"`
	ExpiresIn   int64  `json:"expires_in"`
	ResendAfter int64  `json:"resend_after"`
}

type LoginByPhoneCodeInput struct {
	Phone string `json:"phone"`
	Code  string `json:"code"`
}

type RefreshTokenInput struct {
	RefreshToken string `json:"refresh_token"`
	UserID       uint   `json:"user_id,omitempty"`
	UinID        uint   `json:"uin_id,omitempty"`
	LoginWay     int    `json:"login_way,omitempty"`
}

type ChooseUinInput struct {
	RefreshToken string `json:"refresh_token"`
	Uin          uint   `json:"uin"`
	UserID       uint   `json:"user_id,omitempty"`
	LoginWay     int    `json:"login_way,omitempty"`
}

type SwitchOrganizationInput struct {
	Uin      uint `json:"uin"`
	LoginWay int  `json:"login_way"`
}

type CreateOrganizationInput struct {
	Name            string `json:"name"`
	RefreshToken    string `json:"refresh_token,omitempty"`
	UserDisplayName string `json:"user_display_name,omitempty"`
	UserID          uint   `json:"user_id,omitempty"`
}

type AuthTokens struct {
	LoginStatus   string        `json:"login_status,omitempty"`
	JwtToken      string        `json:"jwt_token,omitempty"`
	RefreshToken  string        `json:"refresh_token,omitempty"`
	ExpiredAt     int64         `json:"expired_at,omitempty"`
	Uin           uint          `json:"uin"`
	UserID        uint          `json:"user_id,omitempty"`
	UserInfo      AuthUserInfo  `json:"user_info,omitempty"`
	Org           AuthOrgInfo   `json:"org"`
	Organizations []AuthOrgInfo `json:"organizations,omitempty"`
	LoginWay      int           `json:"login_way,omitempty"`
	Edition       string        `json:"edition,omitempty"`
}

type AuthSessionOutput struct {
	UserInfo      AuthUserInfo  `json:"user_info"`
	Org           AuthOrgInfo   `json:"org"`
	Organizations []AuthOrgInfo `json:"organizations"`
}

type AuthUserInfo struct {
	ID        uint   `json:"id"`
	PublicID  string `json:"public_id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Phone     string `json:"phone,omitempty"`
	AvatarURL string `json:"avatar_url,omitempty"`
	UinName   string `json:"uin_name"`
}

type AuthOrgInfo struct {
	ID              uint   `json:"id"`
	PublicID        string `json:"public_id,omitempty"`
	Code            string `json:"code,omitempty"`
	Name            string `json:"name"`
	Logo            string `json:"logo,omitempty"`
	IsDefault       bool   `json:"is_default,omitempty"`
	CreatedByUin    uint   `json:"created_by_uin,omitempty"`
	CreatedByUserID uint   `json:"created_by_user_id,omitempty"`
	Uin             uint   `json:"uin,omitempty"`
	UserName        string `json:"uin_name"`
}
