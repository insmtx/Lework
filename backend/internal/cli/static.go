package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// StorageConfig represents storage configuration returned by the server.
type StorageConfig struct {
	Scheme string `json:"scheme"`
	Bucket string `json:"bucket"`
}

// doGetRaw sends a GET request to the given full URL and returns the raw response body bytes.
func doGetRaw(ctx context.Context, serverAddr, authToken, fullURL string) ([]byte, error) {
	client := &http.Client{Timeout: defaultHTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("not found (404)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return io.ReadAll(io.LimitReader(resp.Body, 100_000_000))
}

// DownloadSkillCache fetches a skill cache zip from the server's download endpoint.
func DownloadSkillCache(ctx context.Context, serverAddr, authToken, skillID, source, version string) ([]byte, error) {
	baseURL := fmt.Sprintf("http://%s/v1/skill-marketplace/skills/%s/download", serverAddr, url.PathEscape(skillID))
	reqURL := fmt.Sprintf("%s?source=%s&version=%s", baseURL, url.QueryEscape(source), url.QueryEscape(version))
	return doGetRaw(ctx, serverAddr, authToken, reqURL)
}

// GetStorageConfig fetches the storage configuration from the server.
func GetStorageConfig(ctx context.Context, serverAddr, authToken string) (*StorageConfig, error) {
	client := &http.Client{Timeout: defaultHTTPTimeout}
	url := fmt.Sprintf("http://%s/v1/static/storage-config", serverAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if apiResp.Code != dto.CodeSuccess {
		return nil, fmt.Errorf("api error [%d]: %s", apiResp.Code, apiResp.Message)
	}

	var result StorageConfig
	if err := json.Unmarshal(apiResp.Data, &result); err != nil {
		return nil, fmt.Errorf("decode data: %w", err)
	}

	return &result, nil
}

// GetPresignUploadURL fetches a presigned upload URL for the given bucket and key.
func GetPresignUploadURL(ctx context.Context, serverAddr, authToken, bucket, key string) (string, error) {
	reqURL := fmt.Sprintf("http://%s/v1/static/%s/%s?operation=upload", serverAddr, url.PathEscape(bucket), key)
	data, err := doGetRaw(ctx, serverAddr, authToken, reqURL)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
