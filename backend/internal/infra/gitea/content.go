package gitea

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
)

func (c *Client) ListContents(ctx context.Context, owner, repo, dirPath, ref string) ([]DirEntry, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(repo), dirPath)
	if ref != "" {
		apiPath += "?ref=" + url.QueryEscape(ref)
	}
	var entries []DirEntry
	if err := c.doJSON(ctx, "GET", apiPath, nil, &entries); err != nil {
		return nil, fmt.Errorf("list contents %s: %w", dirPath, err)
	}
	return entries, nil
}

func (c *Client) GetRawFile(ctx context.Context, owner, repo, ref, filePath string) (io.ReadCloser, error) {
	apiPath := fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(owner), url.PathEscape(repo), filePath)
	if ref != "" {
		apiPath += "?ref=" + url.QueryEscape(ref)
	}

	resp, err := c.do(ctx, "GET", apiPath, nil)
	if err != nil {
		return nil, fmt.Errorf("get file %s: %w", filePath, err)
	}

	var fc FileContent
	if err := json.NewDecoder(resp.Body).Decode(&fc); err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("decode file content: %w", err)
	}
	resp.Body.Close()

	if fc.Encoding != "base64" {
		return nil, fmt.Errorf("unexpected encoding %q", fc.Encoding)
	}

	decoded, err := base64.StdEncoding.DecodeString(fc.Content)
	if err != nil {
		return nil, fmt.Errorf("decode base64 content: %w", err)
	}

	return io.NopCloser(bytes.NewReader(decoded)), nil
}
