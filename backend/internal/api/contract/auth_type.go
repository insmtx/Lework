package contract

import "github.com/insmtx/Leros/backend/internal/adapter/account"

type RegisterByEmailRequest struct {
	account.RegisterByEmailInput
}

type LoginByEmailRequest struct {
	account.LoginByEmailInput
}

type SendPhoneLoginCodeRequest struct {
	account.SendPhoneLoginCodeInput
}

type SendPhoneLoginCodeResponse struct {
	account.SendPhoneLoginCodeOutput
}

type LoginByPhoneCodeRequest struct {
	account.LoginByPhoneCodeInput
}

type RefreshTokenRequest struct {
	account.RefreshTokenInput
}

type ChooseUinRequest struct {
	account.ChooseUinInput
}

type SwitchOrganizationRequest struct {
	account.SwitchOrganizationInput
}

type CreateOrganizationRequest struct {
	account.CreateOrganizationInput
}

type SwitchOrganizationResponse struct {
	JwtToken string `json:"jwt_token"`
}

type AuthSessionResponse struct {
	account.AuthSessionOutput
}

type AuthUserInfo struct {
	account.AuthUserInfo
}

type AuthOrgInfo struct {
	account.AuthOrgInfo
}
