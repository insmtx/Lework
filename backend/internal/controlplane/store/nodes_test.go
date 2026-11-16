package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func createTestUser(t *testing.T, st *Store, email string) User {
	t.Helper()
	u, err := st.CreateUser(context.Background(), User{Email: email, PasswordHash: "x", Role: "admin", Status: "active"})
	if err != nil {
		t.Fatalf("CreateUser(%q): %v", email, err)
	}
	return u
}

func createTestNode(t *testing.T, st *Store, name string) Node {
	t.Helper()
	n, err := st.UpsertLegacyNode(context.Background(), Node{Name: name})
	if err != nil {
		t.Fatalf("UpsertLegacyNode(%q): %v", name, err)
	}
	return n
}

func TestDeleteNodeForOwnerCascades(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	owner := createTestUser(t, st, "owner@example.com")
	other := createTestUser(t, st, "other@example.com")

	node := createTestNode(t, st, "box")
	if err := st.AddNodeMember(ctx, NodeMember{NodeID: node.ID, UserID: owner.ID, Role: NodeRoleOwner}); err != nil {
		t.Fatalf("AddNodeMember: %v", err)
	}
	// 衍生数据：项目索引、主节点偏好 + 别名、目录偏好。
	if err := st.UpsertProjectIndex(ctx, []ProjectIndex{
		{ProjectKey: "p1", NodeID: node.ID, ProjectID: "p1", Canonical: "/work/p1", LastSeen: time.Now().UTC()},
	}); err != nil {
		t.Fatalf("UpsertProjectIndex: %v", err)
	}
	if err := st.SetProjectPrimaryNodeFor(ctx, owner.ID, "p1", node.ID); err != nil {
		t.Fatalf("SetProjectPrimaryNodeFor: %v", err)
	}
	if err := st.SetPreferenceFor(ctx, owner.ID, "inbox.dir."+node.ID, "/work"); err != nil {
		t.Fatalf("SetPreferenceFor: %v", err)
	}
	if err := st.SetNodeAliasFor(ctx, owner.ID, node.ID, "客厅的电脑"); err != nil {
		t.Fatalf("SetNodeAliasFor: %v", err)
	}
	// 另一账号的目录偏好不能被误删。
	if err := st.SetPreferenceFor(ctx, other.ID, "inbox.dir."+node.ID, "/other"); err != nil {
		t.Fatalf("SetPreferenceFor other: %v", err)
	}
	// 认领记录。
	if _, err := st.CreateClaim(ctx, NodeClaim{
		PairKey: "pk", Code: "ZZ234", NodeName: "box", NodeID: node.ID,
		Status: ClaimStatusClaimed, ExpiresAt: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	if err := st.DeleteNodeForOwner(ctx, node.ID, owner.ID); err != nil {
		t.Fatalf("DeleteNodeForOwner: %v", err)
	}

	if _, found, _ := st.NodeByID(ctx, node.ID); found {
		t.Fatal("node row still present")
	}
	if role, ok, _ := st.NodeRole(ctx, node.ID, owner.ID); ok {
		t.Fatalf("member row still present with role %q", role)
	}
	var claimCount int64
	st.db.Model(&NodeClaim{}).Where("node_id = ?", node.ID).Count(&claimCount)
	if claimCount != 0 {
		t.Fatalf("node_claims rows = %d, want 0", claimCount)
	}
	var indexCount int64
	st.db.Model(&ProjectIndex{}).Where("node_id = ?", node.ID).Count(&indexCount)
	if indexCount != 0 {
		t.Fatalf("project_index rows = %d, want 0", indexCount)
	}
	prefs, _ := st.ListProjectPreferencesFor(ctx, owner.ID)
	if len(prefs) != 1 {
		t.Fatalf("project preferences = %+v, want the row kept", prefs)
	}
	if prefs[0].ProjectKey != "p1" || prefs[0].PrimaryNodeID != "" {
		t.Fatalf("project preference = %+v, want key p1 with primary cleared", prefs[0])
	}
	// 行还在 = 别名（Alias）也还在；删除行会连带丢掉用户起的名字。
	var rows int64
	st.db.Model(&ProjectPreference{}).Where("primary_node_id = ?", node.ID).Count(&rows)
	if rows != 0 {
		t.Fatalf("remaining rows with primary_node_id = %d, want 0", rows)
	}
	if v, ok, _ := st.GetPreferenceFor(ctx, owner.ID, "inbox.dir."+node.ID); ok {
		t.Fatalf("owner inbox preference = %q, want deleted", v)
	}
	if _, ok, _ := st.GetNodeAliasFor(ctx, owner.ID, node.ID); ok {
		t.Fatal("owner node alias must be deleted with the node")
	}
	if _, ok, _ := st.GetPreferenceFor(ctx, other.ID, "inbox.dir."+node.ID); !ok {
		t.Fatal("other user's preference must be untouched")
	}
}

func TestDeleteNodeForOwnerRejectsNonOwner(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	owner := createTestUser(t, st, "owner@example.com")
	member := createTestUser(t, st, "member@example.com")
	node := createTestNode(t, st, "box")
	_ = st.AddNodeMember(ctx, NodeMember{NodeID: node.ID, UserID: owner.ID, Role: NodeRoleOwner})
	_ = st.AddNodeMember(ctx, NodeMember{NodeID: node.ID, UserID: member.ID, Role: NodeRoleMember})

	if err := st.DeleteNodeForOwner(ctx, node.ID, member.ID); !errors.Is(err, ErrNodeNotOwner) {
		t.Fatalf("err = %v, want ErrNodeNotOwner", err)
	}
	if _, found, _ := st.NodeByID(ctx, node.ID); !found {
		t.Fatal("node must survive a rejected delete")
	}
	// 不存在与重复删除都返回 ErrNodeNotFound。
	if err := st.DeleteNodeForOwner(ctx, node.ID, member.ID); !errors.Is(err, ErrNodeNotOwner) {
		t.Fatalf("second err = %v, want ErrNodeNotOwner", err)
	}
	if err := st.DeleteNodeForOwner(ctx, "node_missing", owner.ID); !errors.Is(err, ErrNodeNotFound) {
		t.Fatalf("missing err = %v, want ErrNodeNotFound", err)
	}
}
