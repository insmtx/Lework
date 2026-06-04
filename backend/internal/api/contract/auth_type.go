package contract

type RegisterByEmailRequest struct {
	Email           string `json:"email" binding:"required"`
	Password        string `json:"password" binding:"required"`
	ConfirmPassword string `json:"confirm_password" binding:"required"`
	Name            string `json:"name,omitempty"`
}

type LoginByEmailRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type AuthTokenResponse struct {
	LoginStatus  string       `json:"login_status"`
	JwtToken     string       `json:"jwt_token"`
	RefreshToken string       `json:"refresh_token"`
	ExpiredAt    int64        `json:"expired_at"`
	Uin          uint         `json:"uin"`
	UserInfo     AuthUserInfo `json:"user_info"`
	Org          AuthOrgInfo  `json:"org"`
}

type AuthUserInfo struct {
	ID          uint   `json:"id"`
	PublicID    string `json:"public_id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	GithubLogin string `json:"github_login,omitempty"`
	AvatarURL   string `json:"avatar_url,omitempty"`
}

type AuthOrgInfo struct {
	ID       uint   `json:"id"`
	PublicID string `json:"public_id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
}
