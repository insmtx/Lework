package node

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/insmtx/lework/backend/internal/infra/mq"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

// Forwarder 是 Router 依赖的跨实例请求面，由 mq.CoreRequesterBytes 满足。
//
// 单独声明一个最小接口，让 node 包不必依赖具体 NATS 实现，测试也能用
// 假实现验证路由决策。
type Forwarder interface {
	RequestBytes(ctx context.Context, subject string, request []byte) ([]byte, error)
}

// Router 为一次节点方法调用选择执行者。
//
// 优先走本实例的连接：节点连在当前进程上时不需要经过消息总线，延迟最低。
// 否则把请求转发到 cp.node.<id>.request，由持有该连接的实例代为执行；
// 没有任何实例应答时等价于节点离线。
type Router struct {
	reg     *Registry
	forward Forwarder
}

// NewRouter 组装请求路由器。forward 可以为 nil：此时没有本地连接的节点
// 一律视为离线，便于只测本地路径或单进程部署。
func NewRouter(reg *Registry, forward Forwarder) *Router {
	return &Router{reg: reg, forward: forward}
}

// Request 向 nodeID 发起一次方法调用并返回原始结果。
//
// 错误语义与 Registry.Request 保持一致：本地连接存在时沿用连接自己的错误；
// 转发确认无人应答时返回包着 ErrNodeOffline 的错误；超时等其它错误原样
// 包装返回，调用方按 errors.Is 分类。
func (r *Router) Request(ctx context.Context, nodeID, method string, params json.RawMessage) (json.RawMessage, error) {
	if r != nil && r.reg != nil {
		if conn, ok := r.reg.Connection(nodeID); ok {
			return conn.Request(ctx, method, params)
		}
	}
	if r == nil || r.forward == nil {
		return nil, fmt.Errorf("controlplane: node %s: %w", nodeID, ErrNodeOffline)
	}

	subject, err := messaging.ControlPlaneNodeRequestSubject(nodeID)
	if err != nil {
		return nil, fmt.Errorf("controlplane: route %s: %w", method, err)
	}
	payload, err := json.Marshal(ForwardRequest{Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("controlplane: encode forward request %s: %w", method, err)
	}

	raw, err := r.forward.RequestBytes(ctx, subject, payload)
	if err != nil {
		if errors.Is(err, mq.ErrNoResponders) {
			return nil, fmt.Errorf("controlplane: node %s: %w", nodeID, ErrNodeOffline)
		}
		return nil, fmt.Errorf("controlplane: forward %s to node %s: %w", method, nodeID, err)
	}

	var resp ForwardResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		// 不应回显负载：转发请求与应答都可能带用户内容。
		return nil, fmt.Errorf("controlplane: decode forward response for %s: %w", method, err)
	}
	if resp.Error != nil {
		return nil, &RemoteError{Code: resp.Error.Code, Message: resp.Error.Message}
	}
	return resp.Result, nil
}
