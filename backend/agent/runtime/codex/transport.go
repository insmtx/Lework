package codex

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/bytedance/sonic"
	"github.com/insmtx/Leros/backend/agent"
	"github.com/ygpkg/yg-go/logs"
)

// ============================================================================
// JSON-RPC 传输层 — AppServer 的通信方法
// ============================================================================

// readLoop 持续从 stdout scanner 读取 JSON-RPC 消息并路由。
func (s *AppServer) readLoop(ctx context.Context) {
	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rpcMessage
		if err := sonic.Unmarshal(line, &msg); err != nil {
			logs.Warnf("Codex app-server parse error: %v line=%s", err, string(line))
			continue
		}

		switch {
		case len(msg.ID) > 0 && msg.Method != "":
			req := ServerRequest{ID: msg.ID, Method: msg.Method, Params: msg.Params}
			s.mu.Lock()
			cb := s.onServerRequest
			s.mu.Unlock()
			if cb != nil {
				cb(req)
			}

		case len(msg.ID) > 0 && msg.Method == "":
			var id int64
			if err := sonic.Unmarshal(msg.ID, &id); err != nil {
				continue
			}
			s.mu.Lock()
			ch, ok := s.pending[id]
			s.mu.Unlock()
			if ok {
				ch <- &rpcResponse{ID: id, Result: msg.Result, Error: msg.Error}
			}

		case len(msg.ID) == 0 && msg.Method != "":
			s.mu.Lock()
			cb := s.onNotification
			s.mu.Unlock()
			if cb != nil {
				cb(msg.Method, msg.Params)
			}
		}
	}
}

func (s *AppServer) call(ctx context.Context, method string, params any, result any) error {
	paramsRaw, err := sonic.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal %s params: %w", method, err)
	}

	id := atomic.AddInt64(&s.nextRPCID, 1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}
	reqBytes, err := sonic.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal %s: %w", method, err)
	}

	respCh := make(chan *rpcResponse, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("app-server closed")
	}
	s.pending[id] = respCh
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
	}()

	logs.Infof("Codex app-server call >> method=%s id=%d params=%s", method, id, string(paramsRaw))
	if err := s.writeLine(reqBytes); err != nil {
		return fmt.Errorf("write %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			return fmt.Errorf("%s: %s (code=%d)", method, resp.Error.Message, resp.Error.Code)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := sonic.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("unmarshal %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (s *AppServer) notify(method string, params any) error {
	paramsRaw, err := sonic.Marshal(params)
	if err != nil {
		return err
	}
	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}
	reqBytes, _ := sonic.Marshal(req)
	return s.writeLine(reqBytes)
}

func (s *AppServer) respond(id sonic.NoCopyRawMessage, result any) error {
	var rawID any
	sonic.Unmarshal(id, &rawID)
	respBytes, _ := sonic.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      rawID,
		"result":  result,
	})
	return s.writeLine(respBytes)
}

func (s *AppServer) writeLine(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.closed {
		return fmt.Errorf("app-server closed")
	}
	_, err := s.stdin.Write(append(data, '\n'))
	return err
}

// ============================================================================
// Thread / Turn 操作
// ============================================================================

func (s *AppServer) StartThread(ctx context.Context, modelCfg agent.ModelConfig, systemPrompt string) (string, error) {
	params := s.threadParams(modelCfg, systemPrompt)
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := s.call(ctx, "thread/start", params, &resp); err != nil {
		return "", fmt.Errorf("thread/start: %w", err)
	}
	threadID := strings.TrimSpace(resp.Thread.ID)
	if threadID == "" {
		return "", fmt.Errorf("thread/start returned empty thread id")
	}
	s.mu.Lock()
	s.threadID = threadID
	s.mu.Unlock()
	return threadID, nil
}

func (s *AppServer) ResumeThread(ctx context.Context, threadID string, modelCfg agent.ModelConfig, systemPrompt string) error {
	params := s.resumeThreadParams(threadID, modelCfg, systemPrompt)
	var resp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := s.call(ctx, "thread/resume", params, &resp); err != nil {
		return fmt.Errorf("thread/resume: %w", err)
	}
	s.mu.Lock()
	s.threadID = threadID
	s.mu.Unlock()
	return nil
}

func (s *AppServer) StartTurn(ctx context.Context, threadID string, prompt string) (string, error) {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{{
			"type": "text",
			"text": prompt,
		}},
	}
	var resp struct {
		Turn struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if err := s.call(ctx, "turn/start", params, &resp); err != nil {
		return "", fmt.Errorf("turn/start: %w", err)
	}
	turnID := strings.TrimSpace(resp.Turn.ID)
	s.mu.Lock()
	s.turnID = turnID
	s.mu.Unlock()
	return turnID, nil
}

func (s *AppServer) threadParams(modelCfg agent.ModelConfig, systemPrompt string) map[string]any {
	params := map[string]any{
		"cwd":                   s.workDir,
		"serviceName":           "Leros",
		"approvalPolicy":        "on-request",
		"approvalsReviewer":     "user",
		"sandbox":               "danger-full-access",
		"experimentalRawEvents": false,
	}
	if modelCfg.Model != "" {
		params["model"] = modelCfg.Model
	}
	if strings.TrimSpace(systemPrompt) != "" {
		params["developerInstructions"] = systemPrompt
	}
	return params
}

func (s *AppServer) resumeThreadParams(threadID string, modelCfg agent.ModelConfig, systemPrompt string) map[string]any {
	params := map[string]any{
		"threadId": threadID,
		"cwd":      s.workDir,
	}
	if modelCfg.Model != "" {
		params["model"] = modelCfg.Model
	}
	if strings.TrimSpace(systemPrompt) != "" {
		params["developerInstructions"] = systemPrompt
	}
	return params
}

// SetThreadID 设置当前 thread ID（供 invoker 使用）。
func (s *AppServer) SetThreadID(threadID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.threadID = threadID
}

// SetTurnID 设置当前 turn ID（供 invoker 使用）。
func (s *AppServer) SetTurnID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnID = id
}
