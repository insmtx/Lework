package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

// ListProjects 调用服务端 ListProjects API 并返回解析后的结果。
func ListProjects(ctx context.Context, serverAddr string, req *contract.ListProjectsRequest) (*contract.ProjectList, error) {
	var result contract.ProjectList
	if err := doListRequest(ctx, serverAddr, "ListProjects", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ListTasks 调用服务端 ListTasks API 并返回解析后的结果。
func ListTasks(ctx context.Context, serverAddr string, req *contract.ListTasksRequest) (*contract.TaskList, error) {
	var result contract.TaskList
	if err := doListRequest(ctx, serverAddr, "ListTasks", req, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// doListRequest 发送列表类 API 请求的通用封装。
func doListRequest(ctx context.Context, serverAddr, endpoint string, reqBody, target interface{}) error {
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{Timeout: defaultHTTPTimeout}
	url := fmt.Sprintf("http://%s/v1/%s", serverAddr, endpoint)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var apiResp apiResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	if apiResp.Code != dto.CodeSuccess {
		return fmt.Errorf("api error [%d]: %s", apiResp.Code, apiResp.Message)
	}

	if err := json.Unmarshal(apiResp.Data, target); err != nil {
		return fmt.Errorf("decode data: %w", err)
	}

	return nil
}
