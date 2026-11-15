package messaging

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	// StreamNameControlPlane 是控制面节点事件与状态快照所在的 JetStream stream。
	StreamNameControlPlane = "CONTROLPLANE_EVENT_STREAM"

	// ControlPlanePushConsumer 是跨实例推送审批通知的持久化 pull consumer。
	// 每个控制面实例都运行它，同一条消息只会被集群内一个实例取走。
	ControlPlanePushConsumer = "controlplane-push-consumer"
)

const (
	controlPlaneEventPrefix   = "cp.events.node."
	controlPlaneRequestPrefix = "cp.node."
	controlPlanePTYPrefix     = "cp.pty."
	controlPlaneLivePrefix    = "cp.live.node."
)

// ControlPlaneEventSubject 构建 agent -> 控制面的节点事件 subject。
//
// 格式：cp.events.node.<node_id>
//
// 该 subject 走 JetStream，是控制面唯一进入 stream 的 cp subject；
// 节点状态事件与周期性快照都发到这里。
func ControlPlaneEventSubject(nodeID string) (string, error) {
	token, err := sanitizeSubjectToken(nodeID, "node id")
	if err != nil {
		return "", err
	}
	return controlPlaneEventPrefix + token, nil
}

// ControlPlaneEventWildcard 返回订阅所有节点事件的 wildcard subject。
//
// 格式：cp.events.node.>
func ControlPlaneEventWildcard() string {
	return "cp.events.node.>"
}

// ControlPlaneNodeRequestSubject 构建跨实例转发节点请求的 subject。
//
// 格式：cp.node.<node_id>.request
//
// 该 subject 走 Core NATS request/reply，永远不会被写进 stream：
// 请求需要的是低延迟应答，而不是可重放的历史。
func ControlPlaneNodeRequestSubject(nodeID string) (string, error) {
	token, err := sanitizeSubjectToken(nodeID, "node id")
	if err != nil {
		return "", err
	}
	return controlPlaneRequestPrefix + token + ".request", nil
}

// ControlPlanePTYSubject 构建终端输出广播的 subject。
//
// 格式：cp.pty.<node_id>.<pty_id>
//
// 该 subject 走 Core NATS，同样不进入任何 stream：终端输出是易失的，
// 只有当前连着浏览器的实例需要接收。
func ControlPlanePTYSubject(nodeID, ptyID string) (string, error) {
	nodeToken, err := sanitizeSubjectToken(nodeID, "node id")
	if err != nil {
		return "", err
	}
	ptyToken, err := sanitizeSubjectToken(ptyID, "pty id")
	if err != nil {
		return "", err
	}
	return controlPlanePTYPrefix + nodeToken + "." + ptyToken, nil
}

// ControlPlaneLiveSubject 构建增量事件广播的 subject。
//
// 格式：cp.live.node.<node_id>
//
// 该 subject 走 Core NATS，不进入任何 stream：token 级增量没有 durable
// 语义，只有当前正在收听的实例需要它。
func ControlPlaneLiveSubject(nodeID string) (string, error) {
	token, err := sanitizeSubjectToken(nodeID, "node id")
	if err != nil {
		return "", err
	}
	return controlPlaneLivePrefix + token, nil
}

// ControlPlaneLiveWildcard 返回订阅所有节点增量事件的 wildcard subject。
//
// 格式：cp.live.>
func ControlPlaneLiveWildcard() string {
	return "cp.live.>"
}

// sanitizeSubjectToken 保证一个标识只占一个 NATS subject token。
//
// 节点 id 与 pty id 来自 agent，可能带点号或通配符；直接拼接会让恶意或
// 手误的 id 覆盖其他节点甚至伪造出 wildcard 订阅。替换规则集中在
// 一个函数里，事件、请求、PTY 三类 subject 因而得到一致的 token。
func sanitizeSubjectToken(value, field string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", field)
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if r == '.' || r == '*' || r == '>' || unicode.IsSpace(r) {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
	}
	return b.String(), nil
}
