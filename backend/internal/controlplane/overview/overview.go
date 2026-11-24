// Package overview 维护“所有节点上正在发生什么”的内存派生索引。
//
// 控制面不复制 OpenCode 的会话状态机：这里的每条记录都只是某个事件的
// 投影，进程重启或事件丢失后重建即可。任何写操作都不允许以这里的值
// 作为依据（审批永远以 permission_id 为准）。
package overview

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
)

// State 是会话在总览里的粗粒度状态。
type State string

const (
	// StateRunning 表示会话正在产出内容。
	StateRunning State = "running"
	// StateWaitingPermission 表示卡在需要用户处理的审批或表单上。
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

// SnapshotEvent 是 owner 周期性发布的整节点投影快照。
//
// payload 为 {"sessions":[]Session}，语义是整节点替换：新实例启动后靠它
// 在 10 秒内收敛到与 owner 一致的视图，不必重放历史事件。
//
// 实际事件类型定义在 event 包，保留这个别名兼容已有调用方。
const SnapshotEvent = event.EventTypeNodeSnapshot

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
	PendingForms int       `json:"pending_forms"`
	LastActivity time.Time `json:"last_activity"`
}

// Permission 是 permission.asked 的投影，只保留渲染审批卡所需的字段。
// 它不是审批事实：回复仍必须以 OpenCode 的 permission ID 为准。
type Permission struct {
	ID        string   `json:"id"`
	SessionID string   `json:"session_id"`
	Action    string   `json:"action"`
	Resources []string `json:"resources"`
	Message   string   `json:"message,omitempty"`
}

// Node 是一个节点及其在跑会话。PendingForms 是位置级（global）表单数量：
// 这类表单不挂在任何会话上，只能按节点计数。
type Node struct {
	NodeID       string    `json:"node_id"`
	PendingForms int       `json:"pending_forms"`
	Sessions     []Session `json:"sessions"`
}

type sessionState struct {
	sessionID    string
	title        string
	state        State
	pending      map[string]Permission
	lastActivity time.Time

	// pendingCount 记录快照给出的待审批数量。
	//
	// 快照只有计数、没有 permission ID；为了不伪造 ID，快照建立的会话把
	// 计数单独存在这里，等实时事件到达后再以真实 pending 集合为准。
	pendingCount int

	// forms 记录待回答表单的 ID；快照只有计数、没有 ID，因此快照建立的
	// 会话把计数单独放在 pendingFormCount。
	forms            map[string]struct{}
	pendingFormCount int
}

// watermark 记录某节点最近一次被接受的状态事件纪元与序号。
//
// owner 区分节点重新连到哪个实例；seq 只在同一 owner 内可比。没有 watermark
// 时不限制，保持单测与旧事件的原有行为。
type watermark struct {
	owner string
	seq   int64
}

// Index 是并发安全的事件投影。写只在 Apply，读只在 Snapshot。
type Index struct {
	mu    sync.Mutex
	nodes map[string]map[string]*sessionState
	// watermarks 与 nodes 同锁保护，避免“检查序号”和“应用事件”之间插入
	// 另一条消息导致旧快照覆盖新状态。
	watermarks map[string]watermark
	// globalForms 是位置级表单按节点的 ID 集合；globalFormCounts 保存快照
	// 给出的计数，用于没有 ID 时仍能显示徽标。
	globalForms      map[string]map[string]struct{}
	globalFormCounts map[string]int
}

// NewIndex 创建空索引。
func NewIndex() *Index {
	return &Index{
		nodes:            make(map[string]map[string]*sessionState),
		watermarks:       make(map[string]watermark),
		globalForms:      make(map[string]map[string]struct{}),
		globalFormCounts: make(map[string]int),
	}
}

// Apply 把一条事件投影到索引。未知事件只刷新活跃时间，不影响状态判定。
func (i *Index) Apply(ev event.Event) {
	// 快照没有 session_id，必须在下面的早退之前处理；它也优先于 resync：
	// 快照本身就是一份完整状态，直接整节点替换即可。
	if ev.Type == SnapshotEvent {
		i.applySnapshot(ev)
		return
	}

	at := ev.Time
	if at.IsZero() {
		at = time.Now()
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	// 先做水位裁决：过期事件既不能改投影，也不能推进水位。
	if !i.acceptLocked(ev) {
		return
	}

	if ev.Type == resyncEvent {
		delete(i.nodes, ev.NodeID)
		delete(i.globalForms, ev.NodeID)
		delete(i.globalFormCounts, ev.NodeID)
		return
	}
	if ev.SessionID == "" {
		return
	}
	if ev.SessionID == "global" && strings.HasPrefix(ev.Type, "form.") {
		i.applyGlobalFormLocked(ev)
		i.pruneLocked(at)
		return
	}

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
			pending:   make(map[string]Permission),
			forms:     make(map[string]struct{}),
		}
		sessions[ev.SessionID] = sess
	}
	sess.lastActivity = at

	switch ev.Type {
	case "permission.asked":
		if permission, ok := permissionFrom(ev.Data, ev.SessionID); ok {
			sess.pending[permission.ID] = permission
		}
		sess.pendingCount = len(sess.pending)
		sess.state = StateWaitingPermission

	case "permission.replied", "permission.resolved":
		if id := permissionID(ev.Data); id != "" {
			delete(sess.pending, id)
		}
		sess.pendingCount = len(sess.pending)
		if len(sess.pending) == 0 && sess.pendingFormCount == 0 {
			sess.state = StateRunning
		}

	case "form.created":
		if id := formIDFrom(ev.Data); id != "" {
			sess.forms[id] = struct{}{}
		}
		sess.pendingFormCount = len(sess.forms)
		sess.state = StateWaitingPermission

	case "form.replied", "form.cancelled":
		if id := formIDFrom(ev.Data); id != "" {
			delete(sess.forms, id)
		}
		sess.pendingFormCount = len(sess.forms)
		if len(sess.pending) == 0 && sess.pendingFormCount == 0 {
			sess.state = StateRunning
		}

	case "session.execution.failed":
		sess.state = StateError
		sess.pending = make(map[string]Permission)
		sess.pendingCount = 0
		sess.forms = make(map[string]struct{})
		sess.pendingFormCount = 0

	case "session.execution.succeeded", "session.execution.interrupted":
		sess.state = StateIdle
		sess.pending = make(map[string]Permission)
		sess.pendingCount = 0
		sess.forms = make(map[string]struct{})
		sess.pendingFormCount = 0

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

// applySnapshot 用 owner 的整节点快照替换该节点的全部投影。
//
// 坏负载被静默忽略：快照只是派生状态的纠偏，下一次 tick 会重发，绝不能
// 因为一条坏消息清空用户正在看的会话。空 sessions 表示该节点确实没有
// 会话，因此删除整个节点条目。
func (i *Index) applySnapshot(ev event.Event) {
	if ev.NodeID == "" {
		return
	}

	at := ev.Time
	if at.IsZero() {
		at = time.Now()
	}

	var payload *struct {
		Sessions     []Session `json:"sessions"`
		PendingForms int       `json:"pending_forms"`
	}
	if err := json.Unmarshal(ev.Data, &payload); err != nil || payload == nil {
		return
	}

	sessions := make(map[string]*sessionState, len(payload.Sessions))
	for _, sess := range payload.Sessions {
		if sess.SessionID == "" {
			continue
		}
		lastActivity := sess.LastActivity
		if lastActivity.IsZero() {
			lastActivity = at
		}
		state := sess.State
		if state == "" {
			state = StateRunning
		}
		pendingCount := sess.Pending
		if pendingCount < 0 {
			pendingCount = 0
		}
		pendingForms := sess.PendingForms
		if pendingForms < 0 {
			pendingForms = 0
		}
		sessions[sess.SessionID] = &sessionState{
			sessionID:        sess.SessionID,
			title:            sess.Title,
			state:            state,
			pending:          make(map[string]Permission),
			lastActivity:     lastActivity,
			pendingCount:     pendingCount,
			forms:            make(map[string]struct{}),
			pendingFormCount: pendingForms,
		}
	}

	i.mu.Lock()
	defer i.mu.Unlock()

	// 已有水位时，旧 owner 的快照可能在新 owner 的第一条真实事件之后到达；
	// 快照不能切换 epoch，只有真实事件可以。没有水位时仍允许快照初始化一个
	// 全新实例的投影，否则新实例要等真实事件才能显示节点。
	if !i.acceptLocked(ev) {
		return
	}

	if i.nodes == nil {
		i.nodes = make(map[string]map[string]*sessionState)
	}
	if i.globalForms == nil {
		i.globalForms = make(map[string]map[string]struct{})
	}
	if i.globalFormCounts == nil {
		i.globalFormCounts = make(map[string]int)
	}
	i.globalForms[ev.NodeID] = make(map[string]struct{})
	globalCount := payload.PendingForms
	if globalCount < 0 {
		globalCount = 0
	}
	i.globalFormCounts[ev.NodeID] = globalCount

	if len(sessions) == 0 && globalCount == 0 {
		delete(i.nodes, ev.NodeID)
		delete(i.globalForms, ev.NodeID)
		delete(i.globalFormCounts, ev.NodeID)
		return
	}
	if len(sessions) == 0 {
		delete(i.nodes, ev.NodeID)
		return
	}
	i.nodes[ev.NodeID] = sessions
}

// acceptLocked 按 owner + 序号决定是否接受一条事件，并推进水位。
// 调用方必须持有 i.mu。
//
// 实时事件可以切换 owner（节点重连到新实例），快照则不允许：快照只是纠偏，
// 未知 epoch 必须等真实事件先建立水位。
func (i *Index) acceptLocked(ev event.Event) bool {
	if ev.Owner == "" {
		// 无 owner 的事件/快照来自单测或旧版本，保持原来的无条件语义，
		// 且不污染水位。
		return true
	}
	if i.watermarks == nil {
		i.watermarks = make(map[string]watermark)
	}

	current, ok := i.watermarks[ev.NodeID]
	if !ok {
		i.watermarks[ev.NodeID] = watermark{owner: ev.Owner, seq: ev.ClusterSeq}
		return true
	}
	if current.owner != ev.Owner {
		if ev.Type == SnapshotEvent {
			return false
		}
		i.watermarks[ev.NodeID] = watermark{owner: ev.Owner, seq: ev.ClusterSeq}
		return true
	}
	if ev.ClusterSeq < current.seq {
		return false
	}
	current.seq = ev.ClusterSeq
	i.watermarks[ev.NodeID] = current
	return true
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
	// 只有 global 表单的节点不在 i.nodes 里，必须单独判断，否则清理空节点时
	// 会把仍在等用户处理的表单一起丢掉。
	for nodeID := range i.globalForms {
		if len(i.nodes[nodeID]) == 0 && i.globalPendingTotalLocked(nodeID) == 0 {
			delete(i.globalForms, nodeID)
			delete(i.globalFormCounts, nodeID)
		}
	}
}

// Snapshot 返回一份可序列化的拷贝，按节点 ID 升序、会话按最近活动降序。
func (i *Index) Snapshot() []Node {
	i.mu.Lock()
	defer i.mu.Unlock()

	nodeIDs := make(map[string]struct{}, len(i.nodes)+len(i.globalForms))
	for nodeID := range i.nodes {
		nodeIDs[nodeID] = struct{}{}
	}
	for nodeID := range i.globalForms {
		nodeIDs[nodeID] = struct{}{}
	}

	out := make([]Node, 0, len(nodeIDs))
	for nodeID := range nodeIDs {
		sessions := i.nodes[nodeID]
		node := Node{
			NodeID:       nodeID,
			PendingForms: i.globalPendingTotalLocked(nodeID),
			Sessions:     make([]Session, 0, len(sessions)),
		}
		for _, sess := range sessions {
			node.Sessions = append(node.Sessions, Session{
				SessionID:    sess.sessionID,
				Title:        sess.title,
				State:        sess.state,
				Pending:      pendingTotal(sess),
				PendingForms: pendingFormTotal(sess),
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

// SnapshotNode 返回单个节点在跑的会话副本，按最近活动降序。
//
// owner 用它生成 node.snapshot；未知节点返回空切片而不是 nil，便于直接
// 序列化成 []。
func (i *Index) SnapshotNode(nodeID string) []Session {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.snapshotNodeLocked(nodeID)
}

// SnapshotNodeWithWatermark 返回节点投影及其 watermark，供 owner 发布快照。
//
// 快照必须带上生成时的 owner + seq，接收方才能判断它是否已被实时事件超越。
// 未知节点返回空切片与零值 watermark。
func (i *Index) SnapshotNodeWithWatermark(nodeID string) ([]Session, int, string, int64) {
	i.mu.Lock()
	defer i.mu.Unlock()

	wm := i.watermarks[nodeID]
	return i.snapshotNodeLocked(nodeID), i.globalPendingTotalLocked(nodeID), wm.owner, wm.seq
}

// snapshotNodeLocked 构造节点投影副本；调用方必须持有 i.mu。
func (i *Index) snapshotNodeLocked(nodeID string) []Session {
	sessions := i.nodes[nodeID]
	out := make([]Session, 0, len(sessions))
	for _, sess := range sessions {
		out = append(out, Session{
			SessionID:    sess.sessionID,
			Title:        sess.title,
			State:        sess.state,
			Pending:      pendingTotal(sess),
			PendingForms: pendingFormTotal(sess),
			LastActivity: sess.lastActivity,
		})
	}
	sort.Slice(out, func(a, b int) bool {
		return out[a].LastActivity.After(out[b].LastActivity)
	})
	return out
}

// pendingTotal 返回会话的待审批数量。
//
// 有真实 permission 投影时以集合大小为准；只有快照计数时退回计数，避免
// 为了一个徽标伪造 permission ID。
func pendingTotal(sess *sessionState) int {
	if n := len(sess.pending); n > 0 {
		return n
	}
	return sess.pendingCount
}

// pendingFormTotal 与会话审批同样的快照兜底：有真实 ID 时以集合为准。
func pendingFormTotal(sess *sessionState) int {
	if n := len(sess.forms); n > 0 {
		return n
	}
	return sess.pendingFormCount
}

// globalPendingTotalLocked 返回节点级表单数量；调用方必须持有 i.mu。
func (i *Index) globalPendingTotalLocked(nodeID string) int {
	if n := len(i.globalForms[nodeID]); n > 0 {
		return n
	}
	return i.globalFormCounts[nodeID]
}

// applyGlobalFormLocked 把位置级表单投影到节点级集合；global 表单没有会话。
// 调用方必须持有 i.mu。
func (i *Index) applyGlobalFormLocked(ev event.Event) {
	forms := i.globalForms[ev.NodeID]
	if forms == nil {
		forms = make(map[string]struct{})
		i.globalForms[ev.NodeID] = forms
	}
	if id := formIDFrom(ev.Data); id != "" {
		switch ev.Type {
		case "form.created":
			forms[id] = struct{}{}
		case "form.replied", "form.cancelled":
			delete(forms, id)
		}
	}
	i.globalFormCounts[ev.NodeID] = len(forms)
}

// formIDFrom 解析表单 ID：form.created 在 data.form.id，其余事件在 data.id。
func formIDFrom(data json.RawMessage) string {
	if len(data) == 0 {
		return ""
	}
	var payload struct {
		ID   string `json:"id"`
		Form struct {
			ID string `json:"id"`
		} `json:"form"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return ""
	}
	if payload.Form.ID != "" {
		return payload.Form.ID
	}
	return payload.ID
}

// Title 返回会话标题；未知节点或会话返回空串。
//
// 推送标题要在不查 OpenCode 的前提下写出“哪条会话”，因此复用事件流里
// 已经投影好的标题，避免为一条通知再打一次节点请求。
func (i *Index) Title(nodeID, sessionID string) string {
	i.mu.Lock()
	defer i.mu.Unlock()

	sess := i.nodes[nodeID][sessionID]
	if sess == nil {
		return ""
	}
	return sess.title
}

// PendingPermissions 返回某会话当前待审批项的拷贝，按 ID 升序。
// 未知节点或会话返回空切片；调用方把它当作可丢弃的提示即可。
func (i *Index) PendingPermissions(nodeID, sessionID string) []Permission {
	i.mu.Lock()
	defer i.mu.Unlock()

	sessions := i.nodes[nodeID]
	if sessions == nil {
		return []Permission{}
	}
	sess := sessions[sessionID]
	if sess == nil {
		return []Permission{}
	}

	out := make([]Permission, 0, len(sess.pending))
	for _, permission := range sess.pending {
		out = append(out, permission)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out
}

// ResetNode 丢弃一个节点的全部投影。节点重连后事件从断点继续，旧值不再可信。
func (i *Index) ResetNode(nodeID string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	delete(i.nodes, nodeID)
	delete(i.globalForms, nodeID)
	delete(i.globalFormCounts, nodeID)
}

// permissionFrom 解析 permission.asked 负载；缺少 ID 或 action 时视为不可用。
func permissionFrom(data json.RawMessage, sessionID string) (Permission, bool) {
	if len(data) == 0 {
		return Permission{}, false
	}
	var payload struct {
		ID        string   `json:"id"`
		Action    string   `json:"action"`
		Resources []string `json:"resources"`
		Message   string   `json:"message"`
	}
	if err := json.Unmarshal(data, &payload); err != nil || payload.ID == "" {
		return Permission{}, false
	}
	resources := payload.Resources
	if resources == nil {
		resources = []string{}
	}
	return Permission{
		ID:        payload.ID,
		SessionID: sessionID,
		Action:    payload.Action,
		Resources: resources,
		Message:   payload.Message,
	}, true
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
