//go:build enterprise

package enterprise

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/Leros/backend/config"
	"github.com/insmtx/Leros/backend/internal/adapter/account"
	localauth "github.com/insmtx/Leros/backend/internal/api/auth"
	"github.com/ygpkg/yg-go/logs"
)

const iamAPIVersion = "v1.0.0-301"

type IAMClient = iamClient

type iamClient struct {
	baseURL    string
	env        string
	domainName string
	httpClient *http.Client
}

func NewIAMClient(cfg *config.IAMConfig, env string) *IAMClient {
	return newIAMClient(cfg, env)
}

func newIAMClient(cfg *config.IAMConfig, env string) *iamClient {
	baseURL := ""
	domainName := ""
	if cfg != nil {
		baseURL = strings.TrimRight(cfg.BaseURL, "/")
		domainName = strings.TrimSpace(cfg.DomainName)
	}
	return &iamClient{
		baseURL:    baseURL,
		env:        env,
		domainName: domainName,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

type iamRequest struct {
	Cmd     string `json:"cmd"`
	Env     string `json:"env"`
	Version string `json:"version"`
	Request any    `json:"request"`
}

type iamResponse struct {
	Code     int             `json:"code"`
	Message  string          `json:"message"`
	Response json.RawMessage `json:"Response"`
}

type createAPIKeyRequest struct {
	Name         string `json:"name"`
	Purpose      string `json:"purpose"`
	ResourceType string `json:"resource_type"`
	ResourceID   uint   `json:"resource_id"`
	ExpireHours  int    `json:"expire_hours"`
}

type createAPIKeyResponse struct {
	APIKey string `json:"api_key"`
	ID     uint   `json:"id"`
}

// CreateAPIKey requests an opaque API key owned by the currently authenticated IAM user.
func (c *iamClient) CreateAPIKey(ctx context.Context, input account.CreateAPIKeyInput) (*account.CreatedAPIKey, error) {
	if strings.TrimSpace(extractAuthToken(ctx)) == "" {
		return nil, fmt.Errorf("auth token not found in context")
	}
	request := createAPIKeyRequest{
		Name:         strings.TrimSpace(input.Name),
		Purpose:      strings.TrimSpace(input.Purpose),
		ResourceType: strings.TrimSpace(input.ResourceType),
		ResourceID:   input.ResourceID,
		ExpireHours:  input.ExpireHours,
	}
	var response createAPIKeyResponse
	if err := c.callWithAuth(ctx, "account.CreateAPIKey", request, &response); err != nil {
		return nil, err
	}
	if strings.TrimSpace(response.APIKey) == "" {
		return nil, fmt.Errorf("iam response missing api key")
	}
	return &account.CreatedAPIKey{ID: response.ID, APIKey: response.APIKey}, nil
}

func (c *iamClient) call(ctx context.Context, action string, reqBody, respBody any) error {
	return c.doCall(ctx, action, reqBody, respBody, "")
}

func (c *iamClient) callWithAuth(ctx context.Context, action string, reqBody, respBody any) error {
	token := extractAuthToken(ctx)
	return c.doCall(ctx, action, reqBody, respBody, token)
}

func (c *iamClient) doCall(ctx context.Context, action string, reqBody, respBody any, authToken string) error {
	var lastErr error
	for i := 0; i < 3; i++ {
		err := c.doCallOnce(ctx, action, reqBody, respBody, authToken)
		if err == nil {
			return nil
		}
		var iamErr *iamError
		if errors.As(err, &iamErr) {
			return err
		}
		lastErr = err
		if i < 2 {
			time.Sleep(time.Duration(i*200) * time.Millisecond)
		}
	}
	return lastErr
}

func (c *iamClient) doCallOnce(ctx context.Context, action string, reqBody, respBody any, authToken string) error {
	url := fmt.Sprintf("%s/v5/%s", c.baseURL, action)

	var body io.Reader
	payload := iamRequest{
		Cmd:     action,
		Env:     c.env,
		Version: iamAPIVersion,
		Request: reqBody,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal iam request: %w", err)
	}
	body = bytes.NewReader(data)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return fmt.Errorf("create iam request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("iam request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read iam response: %w", err)
	}

	var envelope iamResponse
	if err := json.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("decode iam response: %w", err)
	}

	if envelope.Code != 0 {
		logs.WarnContextf(ctx, "IAM %s failed: code=%d message=%s", action, envelope.Code, envelope.Message)
		return &iamError{Code: envelope.Code, Message: envelope.Message}
	}

	if respBody != nil && len(envelope.Response) > 0 {
		if err := json.Unmarshal(envelope.Response, respBody); err != nil {
			return fmt.Errorf("decode iam response body: %w", err)
		}
	}
	return nil
}

type iamError struct {
	Code    int
	Message string
}

func (e *iamError) Error() string {
	return fmt.Sprintf("iam error: code=%d message=%s", e.Code, e.Message)
}

func extractAuthToken(ctx context.Context) string {
	return localauth.BearerTokenFromContext(ctx)
}

type iamUploadFileResponse struct {
	Code     int                       `json:"code"`
	Message  string                    `json:"message"`
	Response iamUploadFileResponseBody `json:"Response"`
}

type iamUploadFileResponseBody struct {
	FileID      uint   `json:"file_id"`
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
	FileExt     string `json:"file_ext"`
	StoragePath string `json:"storage_path"`
}

func (c *iamClient) UploadFileByMultipart(ctx context.Context, purpose, filename string, fileData io.Reader, fileSize int64) (string, error) {
	token := extractAuthToken(ctx)
	if token == "" {
		return "", fmt.Errorf("auth token not found in context")
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if err := writer.WriteField("purpose", purpose); err != nil {
		return "", fmt.Errorf("write purpose field: %w", err)
	}

	filePart, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(filePart, fileData); err != nil {
		return "", fmt.Errorf("copy file data: %w", err)
	}
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	url := fmt.Sprintf("%s/v5/account.UploadFile", c.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return "", fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("iam upload request failed: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read iam upload response: %w", err)
	}

	var result iamUploadFileResponse
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return "", fmt.Errorf("decode iam upload response: %w", err)
	}
	if result.Code != 0 {
		logs.WarnContextf(ctx, "IAM UploadFile failed: code=%d message=%s", result.Code, result.Message)
		return "", &iamError{Code: result.Code, Message: result.Message}
	}

	return result.Response.URL, nil
}
