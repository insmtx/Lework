package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/bytedance/sonic"
	"github.com/ygpkg/yg-go/logs"
)

// rpcRequest 是 JSON-RPC 2.0 请求。
type rpcRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int64                  `json:"id,omitempty"`
	Method  string                 `json:"method,omitempty"`
	Params  sonic.NoCopyRawMessage `json:"params,omitempty"`
}

// rpcResponse 是 JSON-RPC 2.0 响应。
type rpcResponse struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int64                  `json:"id"`
	Result  sonic.NoCopyRawMessage `json:"result,omitempty"`
	Error   *rpcError              `json:"error,omitempty"`
}

// rpcError 是 JSON-RPC 2.0 错误对象。
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error code=%d: %s", e.Code, e.Message)
}

// rpcMessage 用于解析收到的消息（统一入口）。
type rpcMessage struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      sonic.NoCopyRawMessage `json:"id,omitempty"`
	Method  string                 `json:"method,omitempty"`
	Params  sonic.NoCopyRawMessage `json:"params,omitempty"`
	Result  sonic.NoCopyRawMessage `json:"result,omitempty"`
	Error   *rpcError              `json:"error,omitempty"`
}

// ServerRequest 表示来自 Codex app-server 的请求（需要客户端响应）。
type ServerRequest struct {
	ID     sonic.NoCopyRawMessage
	Method string
	Params sonic.NoCopyRawMessage
}

// Client 是 JSON-RPC 2.0 客户端，基于独立的 reader/writer 进行行分隔的 JSON 通信。
type Client struct {
	w      io.Writer
	reader *bufio.Scanner

	writeMu      sync.Mutex
	nextID       atomic.Int64
	mu           sync.Mutex
	pending      map[int64]chan *rpcResponse
	closed       bool
	bufferCalled bool // scanner.Buffer() 是否已调用过（防止 panic）

	// 回调：由上层设置
	OnNotification  func(method string, params sonic.NoCopyRawMessage)
	OnServerRequest func(req ServerRequest)
}

// NewClient 创建 JSON-RPC 客户端。
// reader 对应 stdout（接收响应/通知），writer 对应 stdin（发送请求）。
func NewClient(reader io.Reader, writer io.Writer) *Client {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	return &Client{
		w:            writer,
		reader:       scanner,
		bufferCalled: true,
		pending:      make(map[int64]chan *rpcResponse),
	}
}

// NewClientWithScanner 使用已有的 scanner 创建客户端。
// 用于 initialize 阶段已通过独立 scanner 读取首行响应后，
// 继续复用同一个 scanner（避免 bufio 内部缓冲区数据丢失）。
func NewClientWithScanner(scanner *bufio.Scanner, writer io.Writer) *Client {
	return &Client{
		w:            writer,
		reader:       scanner,
		bufferCalled: true, // initScanner 已调用过 Buffer
		pending:      make(map[int64]chan *rpcResponse),
	}
}

// Call 发送 JSON-RPC 请求并阻塞等待响应。
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	if c == nil {
		return errors.New("jsonrpc client is nil")
	}
	paramsRaw, err := sonic.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params for %s: %w", method, err)
	}

	id := c.nextID.Add(1)
	req := rpcRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  paramsRaw,
	}
	reqBytes, err := sonic.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal request %s: %w", method, err)
	}

	respCh := make(chan *rpcResponse, 1)

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("jsonrpc client is closed")
	}
	c.pending[id] = respCh
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	logs.Infof("JSON-RPC call: method=%s id=%d params=%s", method, id, string(paramsRaw))

	if err := c.write(reqBytes); err != nil {
		return fmt.Errorf("write request %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case resp := <-respCh:
		if resp.Error != nil {
			return resp.Error
		}
		if result != nil && len(resp.Result) > 0 {
			if err := sonic.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("unmarshal result for %s: %w", method, err)
			}
		}
		return nil
	}
}

// Notify 发送 JSON-RPC 通知（无需等待响应）。
func (c *Client) Notify(method string, params any) error {
	if c == nil {
		return errors.New("jsonrpc client is nil")
	}
	paramsRaw, err := sonic.Marshal(params)
	if err != nil {
		return fmt.Errorf("marshal params for notify %s: %w", method, err)
	}

	req := rpcRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsRaw,
	}
	reqBytes, err := sonic.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal notify %s: %w", method, err)
	}

	return c.write(reqBytes)
}

// Respond 响应来自 app-server 的请求（如审批决策）。
func (c *Client) Respond(id sonic.NoCopyRawMessage, result any) error {
	if c == nil {
		return errors.New("jsonrpc client is nil")
	}
	if len(id) == 0 {
		return errors.New("respond requires a non-empty id")
	}

	resultRaw, err := sonic.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal respond result: %w", err)
	}

	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  sonic.NoCopyRawMessage(resultRaw),
	}
	respBytes, err := sonic.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	// id 本身是 sonic.NoCopyRawMessage（带引号的字符串），需要做一次解析才能正确序列化
	var rawID any
	if err := sonic.Unmarshal(id, &rawID); err != nil {
		return fmt.Errorf("unmarshal response id: %w", err)
	}
	resp = map[string]any{
		"jsonrpc": "2.0",
		"id":      rawID,
		"result":  sonic.NoCopyRawMessage(resultRaw),
	}
	respBytes, err = sonic.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	logs.Infof("JSON-RPC respond: id=%s result=%s", string(id), string(resultRaw))
	return c.write(respBytes)
}

// RespondError 响应给 app-server 返回错误。
func (c *Client) RespondError(id sonic.NoCopyRawMessage, code int, message string) error {
	if c == nil {
		return errors.New("jsonrpc client is nil")
	}
	if len(id) == 0 {
		return errors.New("respond error requires a non-empty id")
	}

	var rawID any
	if err := sonic.Unmarshal(id, &rawID); err != nil {
		return fmt.Errorf("unmarshal error response id: %w", err)
	}
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      rawID,
		"error": map[string]any{
			"code":    code,
			"message": message,
		},
	}
	respBytes, err := sonic.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal error response: %w", err)
	}

	return c.write(respBytes)
}

// ReadLoop 持续从 stdout 读取 JSON-RPC 消息并路由。
// 在独立的 goroutine 中调用。读到 EOF 或错误时返回。
func (c *Client) ReadLoop(ctx context.Context) error {
	if c == nil {
		return errors.New("jsonrpc client is nil")
	}

	// 注意：当通过 NewClientWithScanner 复用时，scanner 可能已执行过 Scan()
	// 此时不能调用 Buffer()（会 panic）。复用 scanner 的 buffer 已在创建时配好。
	for c.reader.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := c.reader.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg rpcMessage
		if err := sonic.Unmarshal(line, &msg); err != nil {
			logs.Warnf("JSON-RPC parse error: %v, line=%s", err, string(line))
			continue
		}

		if msg.JSONRPC != "2.0" {
			continue
		}

		switch {
		case len(msg.ID) > 0 && msg.Method != "":
			// 服务器请求（需响应）：有 id + method
			if c.OnServerRequest != nil {
				c.OnServerRequest(ServerRequest{
					ID:     msg.ID,
					Method: msg.Method,
					Params: msg.Params,
				})
			}

		case len(msg.ID) > 0 && msg.Method == "":
			// 客户端响应：有 id，无 method（包含 result 或 error）
			var id int64
			if err := sonic.Unmarshal(msg.ID, &id); err != nil {
				logs.Warnf("JSON-RPC unmarshal response id: %v", err)
				continue
			}
			c.mu.Lock()
			ch, ok := c.pending[id]
			c.mu.Unlock()
			if ok {
				select {
				case ch <- &rpcResponse{
					JSONRPC: msg.JSONRPC,
					ID:      id,
					Result:  msg.Result,
					Error:   msg.Error,
				}:
				default:
				}
			}

		case len(msg.ID) == 0 && msg.Method != "":
			// 通知：无 id，有 method
			if c.OnNotification != nil {
				c.OnNotification(msg.Method, msg.Params)
			}
		}
	}

	if err := c.reader.Err(); err != nil {
		return fmt.Errorf("jsonrpc read loop: %w", err)
	}
	return io.EOF
}

// Close 关闭客户端，取消所有 pending 请求。
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
}

func (c *Client) write(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if c.closed {
		return errors.New("jsonrpc client is closed")
	}
	if _, err := c.w.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("jsonrpc write: %w", err)
	}
	return nil
}
