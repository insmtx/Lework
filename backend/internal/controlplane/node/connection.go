// Package node 维护控制面与节点之间的实时连接。
//
// Connection 把一次请求的 ID 与节点返回的响应关联起来，Registry 则跟踪
// 每个节点当前活跃的连接。两者都只处理进程内状态，不负责 WebSocket
// 读写或进程生命周期。
package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
)

// ErrNodeOffline 表示目标节点没有活跃连接。
var ErrNodeOffline = errors.New("controlplane: node offline")

// ErrConnectionClosed 表示连接已关闭，未完成的请求随之失败。
var ErrConnectionClosed = errors.New("controlplane: connection closed")

// Writer 是 Connection 依赖的帧写入端，agentd/transport.Client 满足该接口。
// Close 由 Connection 负责调用一次，避免上层重复关闭底层连接。
type Writer interface {
	Write(ctx context.Context, f protocol.Frame) error
	Close() error
}

// RemoteError 是节点返回的请求失败。Code 稳定可编程处理，Message 面向人，
// 由协议侧保证不含机密信息。
type RemoteError struct {
	Code    string
	Message string
}

// Error 同时带上错误码与说明，便于日志和 API 错误映射。
func (e *RemoteError) Error() string {
	if e == nil {
		return "controlplane: remote error"
	}
	if e.Code == "" {
		return fmt.Sprintf("controlplane: remote error: %s", e.Message)
	}
	return fmt.Sprintf("controlplane: remote error %s: %s", e.Code, e.Message)
}

// pendingResponse 是 Deliver 交给等待方的结果，result 与 err 互斥。
type pendingResponse struct {
	result json.RawMessage
	err    error
}

// Connection 是一条控制面到节点的实时连接。
//
// 写入由 writeMu 串行化（传输层只允许一个并发写者），请求表由 mu 保护。
// Request 与 Deliver 可以在不同 goroutine 上并发调用。
type Connection struct {
	nodeID string
	writer Writer

	// writeMu 保证同一时刻只有一个帧被写入传输层。
	writeMu sync.Mutex

	// mu 保护 pending 与 done 的关闭状态：注册待处理项时在同一把锁下
	// 检查 done，避免 Fail 与 Request 竞争导致等待方永久阻塞。
	mu      sync.Mutex
	pending map[string]chan pendingResponse

	// seq 单调递增，构成连接内唯一的请求 ID。
	seq atomic.Uint64

	// done 关闭表示连接已失败或已关闭，所有等待方立即返回。
	done     chan struct{}
	failOnce sync.Once

	closeOnce sync.Once
	closeErr  error
}

// NewConnection 为 nodeID 创建连接。writer 不能为 nil。
func NewConnection(nodeID string, w Writer) *Connection {
	return &Connection{
		nodeID:  nodeID,
		writer:  w,
		pending: make(map[string]chan pendingResponse),
		done:    make(chan struct{}),
	}
}

// NodeID 返回连接所属的节点 ID。
func (c *Connection) NodeID() string {
	return c.nodeID
}

// Request 向节点发送一次方法调用并等待响应。
//
// 请求参数只会写进帧，不会出现在返回的错误里，避免提示词等内容进入日志。
// 上下文取消时返回包装后的 ctx.Err()，连接失效时返回 ErrConnectionClosed。
func (c *Connection) Request(ctx context.Context, method string, params json.RawMessage) (json.RawMessage, error) {
	if method == "" {
		return nil, errors.New("controlplane: request method is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("controlplane: request %s: %w", method, err)
	}
	if c.writer == nil {
		return nil, errors.New("controlplane: connection has no writer")
	}

	id := fmt.Sprintf("req-%d", c.seq.Add(1))
	respCh := make(chan pendingResponse, 1)

	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return nil, ErrConnectionClosed
	default:
	}
	c.pending[id] = respCh
	c.mu.Unlock()

	// 无论成功、超时还是连接失败都要移除待处理项，避免请求表无限增长。
	defer func() {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
	}()

	c.writeMu.Lock()
	err := c.writer.Write(ctx, protocol.Frame{
		Type:   protocol.FrameRequest,
		ID:     id,
		Method: method,
		Params: params,
	})
	c.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("controlplane: write request %s: %w", method, err)
	}

	select {
	case resp := <-respCh:
		if resp.err != nil {
			return nil, resp.err
		}
		return resp.result, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("controlplane: request %s: %w", method, ctx.Err())
	case <-c.done:
		return nil, ErrConnectionClosed
	}
}

// Deliver 把节点的响应帧按请求 ID 交给等待中的请求。
//
// 只有已知 ID 的帧能被投递；未知 ID、重复响应或空 ID 返回 false，
// 让调用方可以忽略迟到或伪造的帧而不阻塞读循环。
func (c *Connection) Deliver(f protocol.Frame) bool {
	if f.ID == "" {
		return false
	}

	resp := pendingResponse{result: f.Result}
	if f.Error != nil {
		resp = pendingResponse{err: &RemoteError{Code: f.Error.Code, Message: f.Error.Message}}
	}

	// 通道有 1 个缓冲，发送不会阻塞，因此可以在锁内完成查找与投递，
	// 避免请求在查找之后、发送之前被取消导致响应被静默丢弃。
	c.mu.Lock()
	defer c.mu.Unlock()

	ch, ok := c.pending[f.ID]
	if !ok {
		return false
	}
	select {
	case ch <- resp:
		// 投递后立即移除：请求方已经有结果，重复响应应被当作未知 ID 丢弃。
		delete(c.pending, f.ID)
		return true
	default:
		// 同一请求已经收到过响应，重复帧直接丢弃。
		return false
	}
}

// Fail 把连接标记为失效：关闭 done 一次并清空请求表。
//
// 传输层可以传入自己的错误用于诊断，但等待方一律收到 ErrConnectionClosed，
// 避免传输细节泄漏到上层 API。Fail 可以重复调用。
func (c *Connection) Fail(_ error) {
	c.failOnce.Do(func() {
		c.mu.Lock()
		close(c.done)
		clear(c.pending)
		c.mu.Unlock()
	})
}

// Close 关闭连接并释放底层写入端，可重复调用。
// 底层关闭错误只在第一次关闭时返回。
func (c *Connection) Close() error {
	c.Fail(ErrConnectionClosed)

	c.closeOnce.Do(func() {
		if c.writer == nil {
			return
		}
		if err := c.writer.Close(); err != nil {
			c.closeErr = fmt.Errorf("controlplane: close connection %q: %w", c.nodeID, err)
		}
	})
	return c.closeErr
}
