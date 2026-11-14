package node

import "encoding/json"

// ForwardRequest 是控制面实例之间转发的一次节点方法调用。
//
// 请求方只关心方法与参数，不需要知道节点实际连在哪个实例上；连接身份由
// cp.node.<node_id>.request 的 subject 决定，因此这里不携带节点 ID。
type ForwardRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params,omitempty"`
}

// ForwardResponse 是转发请求的应答。Result 与 Error 互斥：成功时只带
// Result，失败时只带 Error。
type ForwardResponse struct {
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ForwardError   `json:"error,omitempty"`
}

// ForwardError 是跨实例传递的错误。Code 稳定可编程处理，Message 面向人；
// 两者都来自连接层，不包含注册令牌等机密信息。
type ForwardError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
