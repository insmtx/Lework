package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/overview"
)

func TestOverviewIncludesConnectedFlag(t *testing.T) {
	env := newTestEnv(t, time.Second)
	nodeRec, _, _ := env.addNode("dev-box")

	env.index.Apply(event.Event{NodeID: nodeRec.ID, SessionID: "ses_1", Type: "session.created"})

	rec := env.do(http.MethodGet, "/api/overview", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Nodes []struct {
			NodeID    string             `json:"node_id"`
			Name      string             `json:"name"`
			Connected bool               `json:"connected"`
			Sessions  []overview.Session `json:"sessions"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Nodes) != 1 || !body.Nodes[0].Connected || body.Nodes[0].Name != "dev-box" {
		t.Fatalf("nodes = %+v", body.Nodes)
	}
	if len(body.Nodes[0].Sessions) != 1 || body.Nodes[0].Sessions[0].State != overview.StateRunning {
		t.Fatalf("sessions = %+v", body.Nodes[0].Sessions)
	}
}

func TestOverviewKeepsDisconnectedNodes(t *testing.T) {
	env := newTestEnv(t, time.Second)
	nodeRec, _, conn := env.addNode("dev-box")
	env.index.Apply(event.Event{NodeID: nodeRec.ID, SessionID: "ses_1", Type: "session.created"})
	env.registry.Unregister(nodeRec.ID, conn)

	rec := env.do(http.MethodGet, "/api/overview", "")
	if !strings.Contains(rec.Body.String(), `"connected":false`) {
		t.Fatalf("body = %s, want connected:false", rec.Body.String())
	}
	// 断线节点的旧会话不能再报 running，否则用户会等一个不存在的进度。
	if !strings.Contains(rec.Body.String(), `"state":"offline"`) {
		t.Fatalf("body = %s, want state offline for a disconnected node", rec.Body.String())
	}
}

func TestOverviewReturnsEmptyArray(t *testing.T) {
	env := newTestEnv(t, time.Second)
	rec := env.do(http.MethodGet, "/api/overview", "")
	if !strings.Contains(rec.Body.String(), `"nodes":[]`) {
		t.Fatalf("body = %s, want empty array", rec.Body.String())
	}
}
