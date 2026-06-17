package gitea

import (
	"context"
	"fmt"
	"net/url"
)

func (c *Client) GenerateAccessToken(ctx context.Context, username, name string, scopes []string) (*GeneratedToken, error) {
	apiPath := fmt.Sprintf("/users/%s/tokens", url.PathEscape(username))
	req := GenerateTokenRequest{
		Name:   name,
		Scopes: scopes,
	}
	var result TokenResponse
	if err := c.doJSON(ctx, "POST", apiPath, req, &result); err != nil {
		return nil, fmt.Errorf("generate token: %w", err)
	}
	return &GeneratedToken{
		Token: result.Token,
		Name:  result.Name,
		ID:    result.ID,
	}, nil
}

func (c *Client) DeleteAccessToken(ctx context.Context, username string, tokenID int64) error {
	apiPath := fmt.Sprintf("/users/%s/tokens/%d", url.PathEscape(username), tokenID)
	if err := c.doJSON(ctx, "DELETE", apiPath, nil, nil); err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return nil
}
