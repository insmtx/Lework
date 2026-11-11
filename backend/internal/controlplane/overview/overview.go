// Package overview 维护“所有节点上正在发生什么”的内存派生索引。
//
// 控制面不复制 OpenCode 的会话状态机：这里的每条记录都只是某个事件的
// 投影，进程重启或事件丢失后重建即可。任何写操作都不允许以这里的值
// 作为依据（审批永远以 permission_id 为准）。
package overview

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
)

// State 是会话在总览里的粗粒度状态。
type State string

const (
	// StateRunning 表示会话正在产出内容。
	StateRunning State = "running"
	// StateWaitingPermission 表示卡在权限请求上，需要人介入。
	StateWaitingPermission State = "waiting_permission"
	// StateIdle 表示本轮已结束。
	StateIdle State = "idle"
	// StateError 表示本轮以失败告终。
	StateError State = "error"
	// StateOffline 表示节点已断线，最后一次已知状态不再可信。
	// 它只由 API 层在节点断开时覆盖，索引本身不产生这个值。
	StateOffline State = "offline"
)

// resyncEvent 由节点在重连后发出，表示它的事件流可能断档，派生状态必须丢弃重来。
const resyncEvent = "agent.resync_required"

// pruneAfter 是已结束会话的保留时长。总览只回答“现在在跑什么”，
// 超过这个窗口还停留在 idle/error 的会话不再有人关心，必须丢弃，
// 否则索引会随事件流无限增长。等待审批的会话不在此列。
const pruneAfter = 30 * time.Minute

// Session 是总览里的一行。
type Session struct {
	SessionID    string    `json:"session_id"`
	Title        string    `json:"title,omitempty"`
	State        State     `json:"state"`
	Pending      int       `json:"pending_permissions"`
	LastActivity time.Time `json:"last_activity"`
}

// Node 是一个节点及其在跑会话。
type Node struct {
	NodeID   string    `json:"node_id"`
	Sessions []Session `json:"sessions"`
}

type sessionState struct {
	sessionID    string
	title        string
	state        State
	pending      map[string]struct{}
	lastActivity time.Time
}

// Index 是并发安全的事件投影。写只在 Apply，读只在 Snapshot。
type Index struct {
	mu    sync.Mutex
	nodes map[string]map[string]*sessionState
}

// NewIndex 创建空索引。
func NewIndex() *Index {
	return &Index{nodes: make(map[string]map[string]*sessionState)}
}

// Apply 把一条事件投影到索引。未知事件只刷新活跃时间，不影响状态判定。
func (i *Index) Apply(ev event.Event) {
	if ev.Type == resyncEvent {
		i.ResetNode(ev.NodeID)
		return
	}
	if ev.SessionID == "" {
		return
	}

	at := ev.Time
	if at.IsZero() {
		at = time.Now()
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	if i.nodes == nil {
		i.nodes = make(map[string]map[string]*sessionState)
	}
	sessions := i.nodes[ev.NodeID]
	if sessions == nil {
		sessions = make(map[string]*sessionState)
		i.nodes[ev.NodeID] = sessions
	}
	sess := sessions[ev.SessionID]
	if sess == nil {
		sess = &sessionState{
			sessionID: ev.SessionID,
			pending:   make(map[string]struct{}),
		}
		sessions[ev.SessionID] = sess
	}
	sess.lastActivity = at

	switch ev.Type {
	case "permission.asked":
		if id := permissionID(ev.Data); id != "" {
			sess.pending[id] = struct{}{}
		}
		sess.state = StateWaitingPermission

	case "permission.replied", "permission.resolved":
		if id := permissionID(ev.Data); id != "" {
			delete(sess.pending, id)
		}
		if len(sess.pending) == 0 {
			sess.state = StateRunning
		}

	case "session.execution.failed":
		sess.state = StateError
		sess.pending = make(map[string]struct{})

	case "session.execution.succeeded", "session.execution.interrupted":
		sess.state = StateIdle
		sess.pending = make(map[string]struct{})

	case "session.created", "session.updated":
		if sess.state == "" {
			sess.state = StateRunning
		}
		if title := titleFrom(ev.Data); title != "" {
			sess.title = title
		}

	default:
		// 消息、工具调用等只说明“还在动”。等审批时不能被这些事件覆盖，
		// 否则用户会以为已经不需要处理了。
		if sess.state == "" {
			sess.state = StateRunning
		}
	}

	i.pruneLocked(at)
}

// pruneLocked 丢弃已结束且长时间无活动的会话，并清理空节点。
// 调用方必须持有 i.mu。
//
// now 用事件时间而不是 time.Now()：这样重放历史事件时不会因为“测试里的
// 旧时间戳”而误删刚建立的会话，行为也更可预测。
func (i *Index) pruneLocked(now time.Time) {
	cutoff := now.Add(-pruneAfter)
	for nodeID, sessions := range i.nodes {
		for sessionID, sess := range sessions {
			if sess.state != StateIdle && sess.state != StateError {
				continue
			}
			if sess.lastActivity.Before(cutoff) {
				delete(sessions, sessionID)
			}
		}
		if len(sessions) == 0 {
			delete(i.nodes, nodeID)
		}
	}
}

// Snapshot 返回一份可序列化的拷贝，按节点 ID 升序、会话按最近活动降序。
func (i *Index) Snapshot() []Node {
	i.mu.Lock()
	defer i.mu.Unlock()

	out := make([]Node, 0, len(i.nodes))
	for nodeID, sessions := range i.nodes {
		node := Node{
			NodeID:   nodeID,
			Sessions: make([]Session, 0, len(sessions)),
		}
		for _, sess := range sessions {
			node.Sessions = append(node.Sessions, Session{
				SessionID:    sess.sessionID,
				Title:        sess.title,
				State:        sess.state,
				Pending:      len(sess.pending),
				LastActivity: sess.lastActivity,
			})
		}
		sort.Slice(node.Sessions, func(a, b int) bool {
			return node.Sessions[a].LastActivity.After(node.Sessions[b].LastActivity)
		})
		out = append(out, node)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].NodeID < out[b].NodeID })
	return out
}

// ResetNode 丢弃一个节点的全部投影。节点重连后事件从断点继续，旧值不再可信。
func (i *Index) ResetNode(nodeID string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.nodes, nodeID)
}

// permissionID 从事件负载里取权限 ID；负载不合法时返回空串。
func permissionID(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.ID
}

// titleFrom 从会话事件的负载里取标题；没有就保持原值。
func titleFrom(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload struct {
		Title string `json:"title"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	return payload.Title
}
