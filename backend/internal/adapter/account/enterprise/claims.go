//go:build enterprise

package enterprise

import (
	"context"
	"fmt"
)

type iamTokenClaims struct {
	Uin    uint `json:"uin"`
	UserID uint `json:"user_id"`
	OrgID  uint `json:"company_id"`
}

func (c *iamClient) verifyToken(ctx context.Context, tokenStr string) (*iamTokenClaims, error) {
	var claims iamTokenClaims
	if err := c.doCall(ctx, "account.VerifyToken", nil, &claims, tokenStr); err != nil {
		return nil, fmt.Errorf("verify token: %w", err)
	}
	if claims.Uin == 0 {
		return nil, fmt.Errorf("verify token: invalid uin in response")
	}
	return &claims, nil
}
