package overview

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
)

func TestApplyTracksPendingPermissions(t *testing.T) {
	idx := NewIndex()
	data, _ := json.Marshal(map[string]string{"id": "perm_1"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: data})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: data})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 1 {
		t.Fatalf("snapshot = %+v", snap)
	}
	ses := snap[0].Sessions[0]
	if ses.State != StateWaitingPermission || ses.Pending != 1 {
		t.Fatalf("state = %q pending = %d, want waiting_permission/1", ses.State, ses.Pending)
	}

	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.replied", Data: data})
	ses = idx.Snapshot()[0].Sessions[0]
	if ses.State != StateRunning || ses.Pending != 0 {
		t.Fatalf("state = %q pending = %d, want running/0", ses.State, ses.Pending)
	}
}

func TestApplyTerminalStates(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.created"})
	if got := idx.Snapshot()[0].Sessions[0].State; got != StateRunning {
		t.Fatalf("state after created = %q, want running", got)
	}

	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.execution.failed"})
	if got := idx.Snapshot()[0].Sessions[0].State; got != StateError {
		t.Fatalf("state = %q, want error", got)
	}

	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.execution.succeeded"})
	if got := idx.Snapshot()[0].Sessions[0].State; got != StateIdle {
		t.Fatalf("state = %q, want idle", got)
	}
}

func TestApplyKeepsWaitingWhenMessageArrives(t *testing.T) {
	idx := NewIndex()
	data, _ := json.Marshal(map[string]string{"id": "perm_1"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: data})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "message.part.updated"})

	ses := idx.Snapshot()[0].Sessions[0]
	if ses.State != StateWaitingPermission || ses.Pending != 1 {
		t.Fatalf("state = %q pending = %d, want waiting_permission/1", ses.State, ses.Pending)
	}
}

func TestApplyResetsNodeOnResync(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.created"})
	idx.Apply(event.Event{NodeID: "node_2", SessionID: "ses_2", Type: "session.created"})

	idx.Apply(event.Event{NodeID: "node_1", Type: "agent.resync_required"})

	snap := idx.Snapshot()
	if len(snap) != 1 || snap[0].NodeID != "node_2" {
		t.Fatalf("snapshot = %+v, want only node_2", snap)
	}
}

func TestTitleTracksSessionEvents(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "session.updated",
		Data: json.RawMessage(`{"title":"审查移动端 P1"}`),
	})

	if got := idx.Title("node_1", "ses_1"); got != "审查移动端 P1" {
		t.Fatalf("Title = %q, want the projected title", got)
	}
	if got := idx.Title("node_1", "ses_missing"); got != "" {
		t.Fatalf("Title(unknown session) = %q, want empty", got)
	}
	if got := idx.Title("node_missing", "ses_1"); got != "" {
		t.Fatalf("Title(unknown node) = %q, want empty", got)
	}
}

func TestApplyIgnoresSessionlessEvents(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_1", Type: "node.heartbeat"})
	if snap := idx.Snapshot(); len(snap) != 0 {
		t.Fatalf("snapshot = %+v, want empty", snap)
	}
}

func TestSnapshotOrdersByRecentActivity(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_2", SessionID: "ses_a", Type: "session.created"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_b", Type: "session.created"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_c", Type: "session.created"})

	snap := idx.Snapshot()
	if len(snap) != 2 || snap[0].NodeID != "node_1" {
		t.Fatalf("snapshot = %+v, want node_1 first", snap)
	}
	if len(snap[0].Sessions) != 2 || snap[0].Sessions[0].SessionID != "ses_c" {
		t.Fatalf("sessions = %+v, want newest first", snap[0].Sessions)
	}
}

func TestApplyPrunesFinishedSessions(t *testing.T) {
	idx := NewIndex()
	old := time.Now().Add(-2 * time.Hour)

	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_old", Type: "session.execution.succeeded", Time: old,
	})
	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_new", Type: "session.created", Time: time.Now(),
	})

	// 下一条事件触发清理：已结束且长时间无活动的会话不应永久驻留。
	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_new", Type: "message.part.updated", Time: time.Now(),
	})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 1 || snap[0].Sessions[0].SessionID != "ses_new" {
		t.Fatalf("snapshot = %+v, want only ses_new", snap)
	}
}

func TestApplyKeepsRecentIdleSessions(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "session.execution.succeeded", Time: time.Now(),
	})
	if snap := idx.Snapshot(); len(snap) != 1 || snap[0].Sessions[0].State != StateIdle {
		t.Fatalf("snapshot = %+v, want a recent idle session", snap)
	}
}

// TestIndexAppliesNodeSnapshot 证明 node.snapshot 是整节点替换：payload 中
// 不存在的会话必须消失，不存在的节点也必须被清空。
func TestIndexAppliesNodeSnapshot(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.execution.started"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_2", Type: "session.execution.started"})

	at := time.Now()
	payload, err := json.Marshal(struct {
		Sessions []Session `json:"sessions"`
	}{Sessions: []Session{{
		SessionID:    "ses_2",
		State:        StateWaitingPermission,
		Pending:      2,
		LastActivity: at,
	}}})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	idx.Apply(event.Event{NodeID: "node_1", Type: SnapshotEvent, Data: payload, Time: at})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 1 || snap[0].Sessions[0].SessionID != "ses_2" {
		t.Fatalf("snapshot = %+v, want only ses_2", snap)
	}
	// 快照不携带 permission ID；计数必须保留在快照里，徽标才不会归零。
	if got := snap[0].Sessions[0].Pending; got != 2 {
		t.Fatalf("pending = %d, want 2 (preserved from snapshot)", got)
	}
}

// TestIndexSnapshotClearsNodeWhenEmpty 证明空 payload 会清掉该节点的投影。
func TestIndexSnapshotClearsNodeWhenEmpty(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.created"})

	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Data: json.RawMessage(`{"sessions":[]}`), Time: time.Now(),
	})

	if snap := idx.Snapshot(); len(snap) != 0 {
		t.Fatalf("snapshot = %+v, want empty after an empty snapshot", snap)
	}
}

// TestIndexIgnoresMalformedSnapshot 证明坏负载不会 panic，也不会丢掉旧会话。
func TestIndexIgnoresMalformedSnapshot(t *testing.T) {
	idx := NewIndex()
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "session.created"})

	for _, data := range []json.RawMessage{
		json.RawMessage(`{"sessions":`),
		json.RawMessage(``),
		json.RawMessage(`null`),
	} {
		idx.Apply(event.Event{NodeID: "node_1", Type: SnapshotEvent, Data: data})

		snap := idx.Snapshot()
		if len(snap) != 1 || len(snap[0].Sessions) != 1 || snap[0].Sessions[0].SessionID != "ses_1" {
			t.Fatalf("snapshot = %+v after malformed payload %q, want ses_1 kept", snap, data)
		}
	}
}

// TestSnapshotNodeReturnsSortedSessions 证明快照按最近活动降序返回指定节点。
func TestSnapshotNodeReturnsSortedSessions(t *testing.T) {
	idx := NewIndex()
	old := time.Now().Add(-time.Minute)
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_old", Type: "session.created", Time: old})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_new", Type: "session.created", Time: time.Now()})
	idx.Apply(event.Event{NodeID: "node_2", SessionID: "ses_other", Type: "session.created", Time: time.Now()})

	got := idx.SnapshotNode("node_1")
	if len(got) != 2 || got[0].SessionID != "ses_new" || got[1].SessionID != "ses_old" {
		t.Fatalf("SnapshotNode = %+v, want ses_new before ses_old", got)
	}
	if got := idx.SnapshotNode("node_missing"); len(got) != 0 {
		t.Fatalf("SnapshotNode(unknown) = %+v, want empty", got)
	}
}

// TestLiveEventsResetSnapshotPendingCount 证明实时审批事件以真实待办集合为准。
func TestLiveEventsResetSnapshotPendingCount(t *testing.T) {
	idx := NewIndex()
	payload, err := json.Marshal(struct {
		Sessions []Session `json:"sessions"`
	}{Sessions: []Session{{SessionID: "ses_1", State: StateIdle, Pending: 2, LastActivity: time.Now()}}})
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	idx.Apply(event.Event{NodeID: "node_1", Type: SnapshotEvent, Data: payload})

	data, _ := json.Marshal(map[string]string{"id": "perm_1", "action": "bash"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: data})
	if got := idx.Snapshot()[0].Sessions[0].Pending; got != 1 {
		t.Fatalf("pending after live asked = %d, want 1", got)
	}

	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.replied", Data: data})
	if got := idx.Snapshot()[0].Sessions[0].Pending; got != 0 {
		t.Fatalf("pending after live replied = %d, want 0", got)
	}
}

func TestPendingPermissionsKeepsDetails(t *testing.T) {
	idx := NewIndex()
	data, _ := json.Marshal(map[string]any{
		"id": "perm_1", "action": "bash", "resources": []string{"rm -rf build"},
		"message": "run build",
	})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: data})

	got := idx.PendingPermissions("node_1", "ses_1")
	if len(got) != 1 || got[0].ID != "perm_1" || got[0].Action != "bash" || got[0].Message != "run build" {
		t.Fatalf("permissions = %+v", got)
	}
	if len(got[0].Resources) != 1 || got[0].Resources[0] != "rm -rf build" {
		t.Fatalf("resources = %+v", got[0].Resources)
	}
}

func TestPendingPermissionsDropsAfterReply(t *testing.T) {
	idx := NewIndex()
	data, _ := json.Marshal(map[string]string{"id": "perm_1", "action": "bash"})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.asked", Data: data})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_1", Type: "permission.replied", Data: data})

	if got := idx.PendingPermissions("node_1", "ses_1"); len(got) != 0 {
		t.Fatalf("permissions = %+v, want empty", got)
	}
}

func TestPendingPermissionsUnknownSessionIsEmpty(t *testing.T) {
	idx := NewIndex()
	if got := idx.PendingPermissions("node_x", "ses_x"); len(got) != 0 {
		t.Fatalf("permissions = %+v, want empty", got)
	}
}

// snapshotData 构造 node.snapshot 的 payload，避免每个用例重复 marshal。
func snapshotData(t *testing.T, sessions ...Session) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(struct {
		Sessions []Session `json:"sessions"`
	}{Sessions: sessions})
	if err != nil {
		t.Fatalf("marshal snapshot payload: %v", err)
	}
	return data
}

// TestOldSnapshotDoesNotOverwriteNewerEvent 证明 owner 已经应用了更新的事件后，
// 迟到的旧快照不能把投影回滚到旧状态。
func TestOldSnapshotDoesNotOverwriteNewerEvent(t *testing.T) {
	idx := NewIndex()
	at := time.Now()

	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_new", Type: "session.created",
		Owner: "owner-a", ClusterSeq: 5, Time: at,
	})
	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 3, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_old", State: StateIdle, LastActivity: at}),
	})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 1 || snap[0].Sessions[0].SessionID != "ses_new" {
		t.Fatalf("snapshot = %+v, want the newer event to survive", snap)
	}
}

// TestSnapshotFromFreshOwnerInitializesNode 证明没有水位时，带 owner 的快照
// 可以直接初始化一个全新节点的投影：全新实例正是靠这条路径在快照周期内
// 收敛，而不是等 owner 的下一条实时事件。
func TestSnapshotFromFreshOwnerInitializesNode(t *testing.T) {
	idx := NewIndex()
	at := time.Now()

	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 4, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_1", State: StateRunning, LastActivity: at}),
	})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 1 || snap[0].Sessions[0].SessionID != "ses_1" {
		t.Fatalf("snapshot = %+v, want the stamped snapshot applied on an empty index", snap)
	}
	// 快照建立的水位必须与快照一致，后续旧序号事件才会被拒绝。
	if _, owner, seq := idx.SnapshotNodeWithWatermark("node_1"); owner != "owner-a" || seq != 4 {
		t.Fatalf("watermark = %q/%d, want owner-a/4", owner, seq)
	}

	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_2", Type: "session.created",
		Owner: "owner-a", ClusterSeq: 3, Time: at,
	})
	if snap := idx.Snapshot(); len(snap) != 1 || len(snap[0].Sessions) != 1 {
		t.Fatalf("snapshot = %+v, want the older stamped event dropped", snap)
	}
}

// TestSnapshotFromOtherOwnerIsDropped 证明快照不能切换 epoch：只有真实事件
// 可以声明新 owner；旧 owner 的迟到快照必须被丢弃。
func TestSnapshotFromOtherOwnerIsDropped(t *testing.T) {
	idx := NewIndex()
	at := time.Now()

	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_a", Type: "session.created",
		Owner: "owner-a", ClusterSeq: 1, Time: at,
	})
	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-b", ClusterSeq: 1, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_b", State: StateRunning, LastActivity: at}),
	})

	if snap := idx.Snapshot(); len(snap) != 1 || len(snap[0].Sessions) != 1 ||
		snap[0].Sessions[0].SessionID != "ses_a" {
		t.Fatalf("snapshot = %+v, want owner-b snapshot dropped", snap)
	}

	// 新 owner 的第一条真实事件建立新 epoch。
	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_b", Type: "session.created",
		Owner: "owner-b", ClusterSeq: 2, Time: at,
	})
	// 旧 owner 的更大序号不能凭序号“复活”，跨 owner 的快照一律丢弃。
	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 9, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_c", State: StateRunning, LastActivity: at}),
	})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 2 {
		t.Fatalf("snapshot = %+v, want ses_a and ses_b", snap)
	}
	for _, ses := range snap[0].Sessions {
		if ses.SessionID == "ses_c" {
			t.Fatalf("stale owner snapshot was applied: %+v", snap)
		}
	}
}

// TestSnapshotAtOrAfterWatermarkReplacesNode 证明同一 owner 的快照在序号相等
// 时允许替换（快照是权威整节点视图），更旧的快照则必须忽略。
func TestSnapshotAtOrAfterWatermarkReplacesNode(t *testing.T) {
	idx := NewIndex()
	at := time.Now()

	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "session.created",
		Owner: "owner-a", ClusterSeq: 1, Time: at,
	})
	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 1, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_1", State: StateWaitingPermission, Pending: 1, LastActivity: at}),
	})
	if snap := idx.Snapshot(); len(snap) != 1 || snap[0].Sessions[0].State != StateWaitingPermission {
		t.Fatalf("snapshot = %+v, want the equal-seq snapshot to replace state", snap)
	}

	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 2, Time: at,
		Data: snapshotData(t),
	})
	if snap := idx.Snapshot(); len(snap) != 0 {
		t.Fatalf("snapshot = %+v, want the newer empty snapshot to clear the node", snap)
	}

	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 1, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_1", State: StateRunning, LastActivity: at}),
	})
	if snap := idx.Snapshot(); len(snap) != 0 {
		t.Fatalf("snapshot = %+v, want the older snapshot ignored after the node was cleared", snap)
	}
}

// TestUnstampedEventsBypassAndPreserveWatermark 证明没有 owner 的旧事件/快照
// 保持无条件语义，同时不会污染或推进 owner 水位。
func TestUnstampedEventsBypassAndPreserveWatermark(t *testing.T) {
	idx := NewIndex()
	at := time.Now()

	idx.Apply(event.Event{
		NodeID: "node_1", SessionID: "ses_1", Type: "session.created",
		Owner: "owner-a", ClusterSeq: 7, Time: at,
	})
	idx.Apply(event.Event{NodeID: "node_1", SessionID: "ses_2", Type: "session.created", Time: at})

	if _, owner, seq := idx.SnapshotNodeWithWatermark("node_1"); owner != "owner-a" || seq != 7 {
		t.Fatalf("watermark = %q/%d after an unstamped event, want owner-a/7", owner, seq)
	}

	// 旧 owner 的较早快照仍然会被水位拒绝。
	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent,
		Owner: "owner-a", ClusterSeq: 6, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_stale", State: StateRunning, LastActivity: at}),
	})
	// 无 owner 的快照保持无条件替换。
	idx.Apply(event.Event{
		NodeID: "node_1", Type: SnapshotEvent, Time: at,
		Data: snapshotData(t, Session{SessionID: "ses_3", State: StateIdle, LastActivity: at}),
	})

	snap := idx.Snapshot()
	if len(snap) != 1 || len(snap[0].Sessions) != 1 || snap[0].Sessions[0].SessionID != "ses_3" {
		t.Fatalf("snapshot = %+v, want the unstamped snapshot applied and the stale one ignored", snap)
	}
	if _, owner, seq := idx.SnapshotNodeWithWatermark("node_1"); owner != "owner-a" || seq != 7 {
		t.Fatalf("watermark = %q/%d after an unstamped snapshot, want owner-a/7", owner, seq)
	}
	if _, owner, seq := idx.SnapshotNodeWithWatermark("node_missing"); owner != "" || seq != 0 {
		t.Fatalf("watermark for unknown node = %q/%d, want zero", owner, seq)
	}
}
