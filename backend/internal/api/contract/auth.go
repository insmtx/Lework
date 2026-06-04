package contract

import "context"

// AuthService defines local account authentication capabilities.
type AuthService interface {
	RegisterByEmail(ctx context.Context, req *RegisterByEmailRequest) (*AuthTokenResponse, error)
	LoginByEmail(ctx context.Context, req *LoginByEmailRequest) (*AuthTokenResponse, error)
	RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*AuthTokenResponse, error)
}
