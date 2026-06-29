package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/Leros/backend/agent"
	"github.com/insmtx/Leros/backend/agent/runtime/events"
	"github.com/insmtx/Leros/backend/internal/api/contract"
	"github.com/insmtx/Leros/backend/internal/api/dto"
)

const (
	defaultHTTPTimeout = 30 * time.Second
	sseTimeout         = 10 * time.Minute
)

// apiResponse 用于反序列化 dto.Response 包装的响应体。
type apiResponse struct {
	dto.BaseResponse
	Data json.RawMessage `json:"data,omitempty"`
}

// Chat 启动交互式聊天会话，连接至指定的 Leros 服务端。
// 如果 initialMessage 为非空字符串，则将其作为首条消息发送，否则提示用户输入。
func Chat(ctx context.Context, serverAddr, authToken, initialMessage string) error {
	httpClient := &http.Client{Timeout: defaultHTTPTimeout}

	var projectID, taskID string

	sendAndStream := func(content string) (string, string, error) {
		resp, err := sendNewMessage(ctx, httpClient, serverAddr, authToken, content, projectID, taskID)
		if err != nil {
			return projectID, taskID, err
		}
		if projectID == "" {
			projectID = resp.ProjectID
		}
		if taskID == "" {
			taskID = resp.TaskID
		}
		fmt.Println() // blank line before streaming
		if err := streamSessionEvents(ctx, httpClient, serverAddr, authToken, resp.SessionID); err != nil {
			return projectID, taskID, err
		}
		return projectID, taskID, nil
	}

	if initialMessage != "" {
		_, _, err := sendAndStream(initialMessage)
		if err != nil {
			return fmt.Errorf("initial message failed: %w", err)
		}
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("\n> ")
		if !scanner.Scan() {
			fmt.Println()
			break
		}
		input := strings.TrimSpace(scanner.Text())
		if input == "" {
			continue
		}
		if input == "/exit" || input == "/quit" {
			break
		}

		_, _, err := sendAndStream(input)
		if err != nil {
			logs.Errorf("message failed: %v", err)
		}
	}

	return nil
}

// sendNewMessage 向服务端发送 NewMessage 请求并返回解析后的响应。
func sendNewMessage(
	ctx context.Context,
	client *http.Client,
	serverAddr, authToken string,
	content string,
	projectID string,
	taskID string,
) (*contract.NewMessageResponse, error) {
	reqBody := contract.NewMessageRequest{
		Content:   content,
		ProjectID: projectID,
		TaskID:    taskID,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/v1/NewMessage", serverAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
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

	var result contract.NewMessageResponse
	if err := json.Unmarshal(apiResp.Data, &result); err != nil {
		return nil, fmt.Errorf("decode data: %w", err)
	}

	return &result, nil
}

// streamSessionEvents 连接 SSE 端点并实时输出会话事件。
func streamSessionEvents(
	ctx context.Context,
	client *http.Client,
	serverAddr, authToken string,
	sessionID string,
) error {
	reqBody := map[string]string{
		"session_id": sessionID,
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("http://%s/v1/SessionEvents", serverAddr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create SSE request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	// SSE 连接使用独立的 client，拥有更长的超时时间
	sseClient := &http.Client{Timeout: sseTimeout}
	resp, err := sseClient.Do(req)
	if err != nil {
		return fmt.Errorf("connect SSE: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("SSE unexpected status %d: %s", resp.StatusCode, string(body))
	}

	return parseSSE(resp.Body)
}

// sseEvent 表示从 SSE 流中解析出的单条事件。
type sseEvent struct {
	Event string
	Data  string
}

// parseSSE 解析 SSE 流并实时输出事件内容。
func parseSSE(reader io.Reader) error {
	scanner := bufio.NewScanner(reader)
	var current sseEvent

	printInline := false

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if current.Event != "" || current.Data != "" {
				done := handleSSEEvent(current, &printInline)
				current = sseEvent{}
				if done {
					return nil
				}
			}
			continue
		}

		if strings.HasPrefix(line, "event:") {
			current.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			rawData := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var decoded string
			if err := json.Unmarshal([]byte(rawData), &decoded); err == nil {
				current.Data = decoded
			} else {
				current.Data = rawData
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if strings.Contains(err.Error(), "use of closed network connection") {
			return nil
		}
		return fmt.Errorf("SSE scan error: %w", err)
	}

	if printInline {
		fmt.Println()
	}
	return nil
}

// handleSSEEvent 根据事件类型向终端输出内容，返回 true 表示流已结束。
func handleSSEEvent(e sseEvent, printInline *bool) bool {
	switch agent.EventType(e.Event) {
	case events.EventMessageDelta:
		if e.Data != "" {
			fmt.Print(e.Data)
			*printInline = true
		}
	case events.EventReasoningDelta:
		if e.Data != "" {
			fmt.Printf("\033[2m%s\033[0m", e.Data)
			*printInline = true
		}
	case events.EventToolCallStarted:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		fmt.Printf("[tool_call] %s\n", e.Data)
	case events.EventToolCallCompleted:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		fmt.Printf("[tool_call/ok] %s\n", e.Data)
	case events.EventToolCallFailed:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		fmt.Printf("[tool_call/err] %s\n", e.Data)
	case events.EventResult:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		if e.Data != "" {
			fmt.Println(e.Data)
		}
	case events.EventTodoSnapshot:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		fmt.Printf("[todo] %s\n", e.Data)
	case events.EventTodoUpdated:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		fmt.Printf("[todo/updated] %s\n", e.Data)
	case events.EventFailed:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		fmt.Printf("[run/failed] %s\n", e.Data)
		return true
	case events.EventCompleted:
		if *printInline {
			fmt.Println()
			*printInline = false
		}
		return true
	case events.EventStarted:
	case events.EventCancelled:
		fmt.Println("[run/cancelled]")
		return true
	}
	return false
}
