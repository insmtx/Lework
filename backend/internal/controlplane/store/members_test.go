package store

import (
	"context"
	"slices"
	"testing"
	"time"
)

func TestClaimNodeToUserIsOneShot(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.UpsertLegacyNode(ctx, Node{Name: "work-mac"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if err := s.ClaimNodeToUser(ctx, n.ID, "u1"); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.ClaimNodeToUser(ctx, n.ID, "u2"); err == nil {
		t.Fatal("second claim accepted")
	}
	if role, ok, _ := s.NodeRole(ctx, n.ID, "u1"); !ok || role != NodeRoleOwner {
		t.Fatalf("role = %q, %v", role, ok)
	}

	var stored Node
	if err := s.db.WithContext(ctx).Where("id = ?", n.ID).Take(&stored).Error; err != nil {
		t.Fatalf("reload node: %v", err)
	}
	if stored.ClaimedAt.IsZero() {
		t.Fatal("claimed_at not recorded")
	}
}

func TestUpsertLegacyNodeNeverTouchesClaimedNode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, _ := s.UpsertLegacyNode(ctx, Node{Name: "alice-mac"})
	_ = s.ClaimNodeToUser(ctx, n.ID, "u1")
	_ = s.SetNodeCredential(ctx, n.ID, "hash-a")

	again, err := s.UpsertLegacyNode(ctx, Node{Name: "alice-mac"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if again.ID == n.ID {
		t.Fatal("legacy upsert reused a claimed node")
	}
}

func TestNodeNameIsNoLongerUnique(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertLegacyNode(ctx, Node{Name: "work-mac"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	m := s.db.Migrator()
	if m.HasIndex(&Node{}, "idx_nodes_name") {
		t.Fatal("idx_nodes_name still present")
	}
}

// 旧令牌路径即使面对「有凭证但还没有成员行」的节点也不能按名复用：
// 凭证一旦下发，节点身份就由凭证决定，名字只用于展示。
func TestUpsertLegacyNodeSkipsCredentialedNode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.UpsertLegacyNode(ctx, Node{Name: "paired"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if err := s.SetNodeCredential(ctx, n.ID, "hash-a"); err != nil {
		t.Fatalf("SetNodeCredential: %v", err)
	}

	again, err := s.UpsertLegacyNode(ctx, Node{Name: "paired"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if again.ID == n.ID {
		t.Fatal("legacy upsert reused a node that already has a credential")
	}
}

func TestClaimNodeToUserRefusesSharedOrMissingNode(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.UpsertLegacyNode(ctx, Node{Name: "shared"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	// 只要有任何成员行（哪怕只是 member）就说明节点已有归属。
	if err := s.AddNodeMember(ctx, NodeMember{NodeID: n.ID, UserID: "u0", Role: NodeRoleMember}); err != nil {
		t.Fatalf("AddNodeMember: %v", err)
	}
	if err := s.ClaimNodeToUser(ctx, n.ID, "u1"); err == nil {
		t.Fatal("claim of a node with an existing member row was accepted")
	}
	if err := s.ClaimNodeToUser(ctx, "node_missing", "u1"); err == nil {
		t.Fatal("claim of a missing node was accepted")
	}
}

func TestNodeMembershipRoundTripAndListings(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	claimed, err := s.UpsertLegacyNode(ctx, Node{Name: "mine"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	idle, err := s.UpsertLegacyNode(ctx, Node{Name: "idle"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}

	if err := s.AddNodeMember(ctx, NodeMember{
		NodeID: claimed.ID, UserID: "u1", Role: NodeRoleOwner, GrantedBy: "u1",
	}); err != nil {
		t.Fatalf("AddNodeMember: %v", err)
	}
	// 重复添加同一成员必须被忽略：重复点击「共享」不能报错，也不能把 owner 降级。
	if err := s.AddNodeMember(ctx, NodeMember{
		NodeID: claimed.ID, UserID: "u1", Role: NodeRoleMember,
	}); err != nil {
		t.Fatalf("AddNodeMember repeat: %v", err)
	}
	if err := s.AddNodeMember(ctx, NodeMember{
		NodeID: claimed.ID, UserID: "u2", Role: NodeRoleMember,
	}); err != nil {
		t.Fatalf("AddNodeMember member: %v", err)
	}

	if role, ok, err := s.NodeRole(ctx, claimed.ID, "u1"); err != nil || !ok || role != NodeRoleOwner {
		t.Fatalf("owner role = %q, %v, %v", role, ok, err)
	}
	if role, ok, err := s.NodeRole(ctx, claimed.ID, "u2"); err != nil || !ok || role != NodeRoleMember {
		t.Fatalf("member role = %q, %v, %v", role, ok, err)
	}
	if _, ok, err := s.NodeRole(ctx, claimed.ID, "nobody"); err != nil || ok {
		t.Fatalf("unknown member role lookup = %v, %v", ok, err)
	}

	mine, err := s.ListNodesForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListNodesForUser: %v", err)
	}
	if len(mine) != 1 || mine[0].ID != claimed.ID {
		t.Fatalf("u1 nodes = %+v", mine)
	}
	shared, err := s.ListNodesForUser(ctx, "u2")
	if err != nil {
		t.Fatalf("ListNodesForUser: %v", err)
	}
	if len(shared) != 1 || shared[0].ID != claimed.ID {
		t.Fatalf("u2 nodes = %+v", shared)
	}
	// 空 userID 不能被解释成「所有节点」。
	if none, err := s.ListNodesForUser(ctx, " "); err != nil || len(none) != 0 {
		t.Fatalf("blank user nodes = %+v, %v", none, err)
	}

	unclaimed, err := s.ListUnclaimedNodes(ctx)
	if err != nil {
		t.Fatalf("ListUnclaimedNodes: %v", err)
	}
	if len(unclaimed) != 1 || unclaimed[0].ID != idle.ID {
		t.Fatalf("unclaimed = %+v", unclaimed)
	}

	if err := s.RemoveNodeMember(ctx, claimed.ID, "u2"); err != nil {
		t.Fatalf("RemoveNodeMember: %v", err)
	}
	// 撤销是幂等的：重复撤销不该制造 404。
	if err := s.RemoveNodeMember(ctx, claimed.ID, "u2"); err != nil {
		t.Fatalf("RemoveNodeMember repeat: %v", err)
	}
	if _, ok, _ := s.NodeRole(ctx, claimed.ID, "u2"); ok {
		t.Fatal("revoked member still has a role")
	}
	if after, _ := s.ListNodesForUser(ctx, "u2"); len(after) != 0 {
		t.Fatalf("u2 still sees nodes: %+v", after)
	}
	if after, _ := s.ListNodesForUser(ctx, "u1"); len(after) != 1 {
		t.Fatalf("u1 lost the node: %+v", after)
	}
}

func TestAddNodeMemberValidatesInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.AddNodeMember(ctx, NodeMember{UserID: "u1"}); err == nil {
		t.Error("AddNodeMember without node id: want error")
	}
	if err := s.AddNodeMember(ctx, NodeMember{NodeID: "n1"}); err == nil {
		t.Error("AddNodeMember without user id: want error")
	}
	if err := s.AddNodeMember(ctx, NodeMember{NodeID: "n1", UserID: "u1", Role: "viewer"}); err == nil {
		t.Error("AddNodeMember with unknown role: want error")
	}

	// 省略角色时按最小权限落库，避免出现空角色行。
	if err := s.AddNodeMember(ctx, NodeMember{NodeID: "n1", UserID: "u1"}); err != nil {
		t.Fatalf("AddNodeMember default role: %v", err)
	}
	if role, ok, _ := s.NodeRole(ctx, "n1", "u1"); !ok || role != NodeRoleMember {
		t.Fatalf("default role = %q, %v", role, ok)
	}
}

func TestLookupNodeByCredentialRequiresNonEmptyHash(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.UpsertLegacyNode(ctx, Node{Name: "box"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	// 未激活的节点 credential_hash 为空，空哈希绝不能把它们全部匹配出来。
	if _, ok, err := s.LookupNodeByCredential(ctx, ""); err != nil || ok {
		t.Fatalf("empty hash lookup = %v, %v", ok, err)
	}
	if _, ok, err := s.LookupNodeByCredential(ctx, "hash-a"); err != nil || ok {
		t.Fatalf("lookup before credential = %v, %v", ok, err)
	}

	if err := s.SetNodeCredential(ctx, n.ID, "hash-a"); err != nil {
		t.Fatalf("SetNodeCredential: %v", err)
	}
	got, ok, err := s.LookupNodeByCredential(ctx, "hash-a")
	if err != nil || !ok || got.ID != n.ID {
		t.Fatalf("lookup by credential = %+v, %v, %v", got, ok, err)
	}
	if _, ok, _ := s.LookupNodeByCredential(ctx, "hash-b"); ok {
		t.Fatal("unknown hash resolved a node")
	}

	if err := s.SetNodeCredential(ctx, n.ID, " "); err == nil {
		t.Error("SetNodeCredential with blank hash: want error")
	}
	if err := s.SetNodeCredential(ctx, "node_missing", "hash-c"); err == nil {
		t.Error("SetNodeCredential on missing node: want error")
	}
}

// UpdateNodeMeta 是凭证路径唯一允许写节点的方式：只改展示字段，
// 不能顺手改掉凭证、最后心跳或创建时间。
func TestUpdateNodeMetaOnlyTouchesDisplayFields(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	n, err := s.UpsertLegacyNode(ctx, Node{Name: "old-box", OS: "linux", Arch: "amd64", AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if err := s.SetNodeCredential(ctx, n.ID, "hash-a"); err != nil {
		t.Fatalf("SetNodeCredential: %v", err)
	}
	touched := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	if err := s.TouchNode(ctx, n.ID, touched); err != nil {
		t.Fatalf("TouchNode: %v", err)
	}

	if err := s.UpdateNodeMeta(ctx, n.ID, Node{
		Name:           "new-box",
		OS:             "darwin",
		Arch:           "arm64",
		AgentVersion:   "v2",
		Capabilities:   []string{"shell"},
		WorkspaceRoots: []string{"/w"},
	}); err != nil {
		t.Fatalf("UpdateNodeMeta: %v", err)
	}

	got := reloadNode(t, s, n.ID)
	if got.Name != "new-box" || got.OS != "darwin" || got.Arch != "arm64" || got.AgentVersion != "v2" {
		t.Fatalf("display fields not updated: %+v", got)
	}
	if !slices.Equal(got.Capabilities, []string{"shell"}) ||
		!slices.Equal(got.WorkspaceRoots, []string{"/w"}) {
		t.Fatalf("lists not updated: %+v", got)
	}
	if got.CredentialHash != "hash-a" {
		t.Fatalf("credential hash changed by meta update: %q", got.CredentialHash)
	}
	if !got.LastSeen.Equal(touched) {
		t.Fatalf("LastSeen = %v, want %v", got.LastSeen, touched)
	}
	if got.CreatedAt.IsZero() || got.UpdatedAt.Before(got.CreatedAt) {
		t.Fatalf("timestamps damaged: %+v", got)
	}

	// 空名字表示「这一轮没上报展示名」，必须保留原值而不是把名字清空。
	if err := s.UpdateNodeMeta(ctx, n.ID, Node{OS: "windows"}); err != nil {
		t.Fatalf("UpdateNodeMeta without name: %v", err)
	}
	if got := reloadNode(t, s, n.ID); got.Name != "new-box" || got.OS != "windows" {
		t.Fatalf("meta update without name = %+v", got)
	}

	if err := s.UpdateNodeMeta(ctx, "node_missing", Node{Name: "x"}); err == nil {
		t.Error("UpdateNodeMeta on missing node: want error")
	}
}

// 升级前的库只有 nodes.name 唯一索引；AutoMigrate 不会删索引，
// 必须由 migrateControlPlane 显式 DropIndex，否则同名节点永远插不进去。
func TestMigrateControlPlaneDropsLegacyNameUniqueIndex(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.db.Exec("CREATE UNIQUE INDEX idx_nodes_name ON nodes (name)").Error; err != nil {
		t.Fatalf("create legacy unique index: %v", err)
	}
	if !s.db.Migrator().HasIndex(&Node{}, "idx_nodes_name") {
		t.Fatal("precondition failed: legacy unique index missing")
	}

	if err := migrateControlPlane(s.db); err != nil {
		t.Fatalf("migrateControlPlane: %v", err)
	}
	if s.db.Migrator().HasIndex(&Node{}, "idx_nodes_name") {
		t.Fatal("idx_nodes_name survived migration")
	}
	if !s.db.Migrator().HasIndex(&Node{}, "idx_nodes_name_lookup") {
		t.Fatal("display-name index missing after migration")
	}
	if !s.db.Migrator().HasTable(&NodeMember{}) {
		t.Fatal("node_members table missing after migration")
	}
	// 按用户列节点每次请求都要走这条索引，丢了会让节点列表退化成全表扫描。
	if !s.db.Migrator().HasIndex(&NodeMember{}, "idx_node_member_user") {
		t.Fatal("idx_node_member_user missing after migration")
	}

	// 名字唯一性已交给归属在应用层保证，数据库必须允许同名共存。
	for i := 0; i < 2; i++ {
		id, err := newNodeID()
		if err != nil {
			t.Fatalf("newNodeID: %v", err)
		}
		if err := s.db.WithContext(ctx).Create(&Node{ID: id, Name: "work-mac"}).Error; err != nil {
			t.Fatalf("insert duplicate name %d: %v", i, err)
		}
	}
}

// reloadNode 直接从库里重读节点，避免用手里的入参自我验证。
func reloadNode(t *testing.T, s *Store, id string) Node {
	t.Helper()
	var n Node
	if err := s.db.Where("id = ?", id).Take(&n).Error; err != nil {
		t.Fatalf("reload node %q: %v", id, err)
	}
	return n
}
