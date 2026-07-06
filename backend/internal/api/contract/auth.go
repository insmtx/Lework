package contract

import "context"

// AuthService defines local account authentication capabilities.
type AuthService interface {
	RegisterByEmail(ctx context.Context, req *RegisterByEmailRequest) (*AuthTokenResponse, error)
	LoginByEmail(ctx context.Context, req *LoginByEmailRequest) (*AuthTokenResponse, error)
	SendPhoneLoginCode(ctx context.Context, req *SendPhoneLoginCodeRequest) (*SendPhoneLoginCodeResponse, error)
	LoginByPhoneCode(ctx context.Context, req *LoginByPhoneCodeRequest) (*AuthTokenResponse, error)
	RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*AuthTokenResponse, error)
	SwitchOrganization(ctx context.Context, req *SwitchOrganizationRequest) (*AuthTokenResponse, error)
	CreateOrganization(ctx context.Context, req *CreateOrganizationRequest) (*AuthTokenResponse, error)
	AuthSession(ctx context.Context) (*AuthSessionResponse, error)
}
