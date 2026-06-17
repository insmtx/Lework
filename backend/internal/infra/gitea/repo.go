package gitea

import (
	"context"
	"fmt"
	"net/url"
)

func (c *Client) CreateRepo(ctx context.Context, org string, req CreateRepoRequest) (*RepoInfo, error) {
	apiPath := fmt.Sprintf("/orgs/%s/repos", url.PathEscape(org))
	var result RepoInfo
	if err := c.doJSON(ctx, "POST", apiPath, req, &result); err != nil {
		return nil, fmt.Errorf("create repo: %w", err)
	}
	return &result, nil
}

func (c *Client) GetRepo(ctx context.Context, owner, repo string) (*RepoInfo, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	var result RepoInfo
	if err := c.doJSON(ctx, "GET", apiPath, nil, &result); err != nil {
		return nil, fmt.Errorf("get repo: %w", err)
	}
	return &result, nil
}

func (c *Client) DeleteRepo(ctx context.Context, owner, repo string) error {
	apiPath := fmt.Sprintf("/repos/%s/%s", url.PathEscape(owner), url.PathEscape(repo))
	if err := c.doJSON(ctx, "DELETE", apiPath, nil, nil); err != nil {
		return fmt.Errorf("delete repo: %w", err)
	}
	return nil
}

func (c *Client) CreateFile(ctx context.Context, owner, repo, filePath, content, message string) error {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(repo), filePath)
	req := CreateFileRequest{
		Content: content,
		Message: message,
	}
	if err := c.doJSON(ctx, "POST", apiPath, req, nil); err != nil {
		return fmt.Errorf("create file %s: %w", filePath, err)
	}
	return nil
}
