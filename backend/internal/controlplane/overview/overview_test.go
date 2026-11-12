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
