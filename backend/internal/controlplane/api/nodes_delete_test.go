package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

// closeRecorder 记录底层连接是否被关闭：Connection.Close 的可见效果就是它。
type closeRecorder struct{ closed atomic.Bool }

func (w *closeRecorder) Write(context.Context, protocol.Frame) error { return nil }
func (w *closeRecorder) Close() error                                { w.closed.Store(true); return nil }

// ownedNode 建一个已归属 owner 的节点，并返回它的存储记录。
func ownedNode(t *testing.T, env *testEnv, ownerID, name string) store.Node {
	t.Helper()

	rec, _, _ := env.addNode(name)
	if err := env.store.AddNodeMember(context.Background(), store.NodeMember{
		NodeID: rec.ID, UserID: ownerID, Role: store.NodeRoleOwner,
	}); err != nil {
		t.Fatalf("AddNodeMember: %v", err)
	}
	return rec
}

func userPrincipal(id string) authz.Principal {
	return authz.Principal{Kind: authz.KindUser, UserID: id, Role: "user"}
}

func TestDeleteNodeOwnerSucceeds(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, "user", "active")
	rec := ownedNode(t, env, owner.ID, "box")

	if err := env.store.UpsertProjectIndex(context.Background(), []store.ProjectIndex{{
		ProjectKey: "p1", NodeID: rec.ID, ProjectID: "p1", Canonical: "/work/p1", LastSeen: time.Now().UTC(),
	}}); err != nil {
		t.Fatalf("UpsertProjectIndex: %v", err)
	}

	p := userPrincipal(owner.ID)
	if res := env.doAs(p, http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204; body=%s", res.Code, res.Body.String())
	}

	var nodes []nodeResponse
	res := env.doAs(p, http.MethodGet, "/api/nodes", "")
	if err := json.Unmarshal(res.Body.Bytes(), &nodes); err != nil {
		t.Fatalf("decode nodes: %v", err)
	}
	if len(nodes) != 0 {
		t.Fatalf("nodes = %+v, want none after delete", nodes)
	}

	var projects struct {
		Projects []struct {
			Key string `json:"key"`
		} `json:"projects"`
	}
	res = env.doAs(p, http.MethodGet, "/api/projects", "")
	if err := json.Unmarshal(res.Body.Bytes(), &projects); err != nil {
		t.Fatalf("decode projects: %v", err)
	}
	if len(projects.Projects) != 0 {
		t.Fatalf("projects = %+v, want none after delete", projects.Projects)
	}

	if res := env.doAs(p, http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", res.Code)
	}
}

func TestDeleteNodeRejectsNonOwner(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, "user", "active")
	member := createTestUser(t, env, "member@example.com", testLoginPassword, "user", "active")
	stranger := createTestUser(t, env, "stranger@example.com", testLoginPassword, "user", "active")
	rec := ownedNode(t, env, owner.ID, "box")
	if err := env.store.AddNodeMember(context.Background(), store.NodeMember{
		NodeID: rec.ID, UserID: member.ID, Role: store.NodeRoleMember,
	}); err != nil {
		t.Fatalf("AddNodeMember member: %v", err)
	}

	if res := env.doAs(userPrincipal(member.ID), http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusForbidden {
		t.Fatalf("member delete status = %d, want 403", res.Code)
	}
	if res := env.doAs(authz.Principal{Kind: authz.KindAdminToken}, http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusForbidden {
		t.Fatalf("admin token delete status = %d, want 403", res.Code)
	}
	if res := env.doAs(userPrincipal(stranger.ID), http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusNotFound {
		t.Fatalf("stranger delete status = %d, want 404", res.Code)
	}

	if _, found, _ := env.store.NodeByID(context.Background(), rec.ID); !found {
		t.Fatal("node must survive rejected deletes")
	}
}

func TestDeleteNodeClosesLiveConnection(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, "user", "active")
	rec := ownedNode(t, env, owner.ID, "box")

	writer := &closeRecorder{}
	env.registry.Register(node.Info{ID: rec.ID}, node.NewConnection(rec.ID, writer))

	if res := env.doAs(userPrincipal(owner.ID), http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", res.Code)
	}
	if !writer.closed.Load() {
		t.Fatal("live connection was not closed")
	}
}

func TestDeleteNodePublishesRevokedEvent(t *testing.T) {
	env := newTestEnv(t, time.Second)
	owner := createTestUser(t, env, "owner@example.com", testLoginPassword, "user", "active")
	rec := ownedNode(t, env, owner.ID, "box")

	events, unsubscribe := env.hub.Subscribe(4)
	defer unsubscribe()

	if res := env.doAs(userPrincipal(owner.ID), http.MethodDelete, "/api/nodes/"+rec.ID, ""); res.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", res.Code)
	}

	select {
	case got := <-events:
		if got.Type != "node.member.revoked" || got.NodeID != rec.ID {
			t.Fatalf("event = %+v, want node.member.revoked for %q", got, rec.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no revoked event reached the hub")
	}

	var closeSubject string
	for _, subject := range env.corePub.snapshot() {
		if subject == "cp.node."+rec.ID+".close" {
			closeSubject = subject
		}
	}
	if closeSubject == "" {
		t.Fatalf("published subjects = %v, want the node close subject", env.corePub.snapshot())
	}
}
