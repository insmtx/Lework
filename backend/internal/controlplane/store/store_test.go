package store

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()

	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() failed: %v", err)
		}
	})
	return s
}

func TestOpenMigratesNodesTable(t *testing.T) {
	s := openTestStore(t)

	if !s.db.Migrator().HasTable(&Node{}) {
		t.Fatal("expected nodes table to be migrated")
	}
}

// TestStoreLoggerKeepsSQLParamsOutOfLogs 固化设计 §13 的要求：短码与凭证都在
// SQL 参数里，慢查询与唯一约束冲突会走 logger 的插值路径。参数化输出是这条
// 保证的唯一防线，退回到 logger.Default 就会把 5 位码写进 stdout。
func TestStoreLoggerKeepsSQLParamsOutOfLogs(t *testing.T) {
	filter, ok := controlPlaneLogger(logger.Warn).(gorm.ParamsFilter)
	if !ok {
		t.Fatal("store logger does not implement gorm.ParamsFilter")
	}

	sql, vars := filter.ParamsFilter(
		context.Background(), "SELECT * FROM node_claims WHERE code = ?", "AB234")
	if len(vars) != 0 {
		t.Fatalf("params = %v, want none", vars)
	}
	if strings.Contains(sql, "AB234") {
		t.Fatalf("sql %q leaks the claim code", sql)
	}
}

func TestUpsertLegacyNodeCreatesNodeWithGeneratedID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lastSeen := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	got, err := s.UpsertLegacyNode(ctx, Node{
		Name:         "dev-box",
		OS:           "linux",
		Arch:         "amd64",
		AgentVersion: "v1.0.0",
		Capabilities: []string{"shell", "file"},
		LastSeen:     lastSeen,
	})
	if err != nil {
		t.Fatalf("UpsertLegacyNode() failed: %v", err)
	}

	if !strings.HasPrefix(got.ID, "node_") {
		t.Errorf("ID = %q, want prefix %q", got.ID, "node_")
	}
	if len(got.ID) != len("node_")+16 {
		t.Errorf("ID = %q, want node_ followed by 16 hex chars", got.ID)
	}
	if got.Name != "dev-box" {
		t.Errorf("Name = %q, want %q", got.Name, "dev-box")
	}
	if got.AgentVersion != "v1.0.0" {
		t.Errorf("AgentVersion = %q, want %q", got.AgentVersion, "v1.0.0")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	nodes, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("ListNodes() returned %d nodes, want 1", len(nodes))
	}
	if nodes[0].ID != got.ID {
		t.Errorf("stored ID = %q, want %q", nodes[0].ID, got.ID)
	}
	if !slices.Equal(nodes[0].Capabilities, []string{"shell", "file"}) {
		t.Errorf("stored Capabilities = %v, want %v", nodes[0].Capabilities, []string{"shell", "file"})
	}
}

func TestUpsertLegacyNodeReusesIDForSameName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first, err := s.UpsertLegacyNode(ctx, Node{Name: "dev-box", AgentVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("first UpsertLegacyNode() failed: %v", err)
	}

	second, err := s.UpsertLegacyNode(ctx, Node{
		Name:         "dev-box",
		OS:           "linux",
		Arch:         "arm64",
		AgentVersion: "v1.1.0",
		Capabilities: []string{"shell"},
	})
	if err != nil {
		t.Fatalf("second UpsertLegacyNode() failed: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("second ID = %q, want it to reuse %q", second.ID, first.ID)
	}
	if second.AgentVersion != "v1.1.0" {
		t.Errorf("AgentVersion = %q, want %q", second.AgentVersion, "v1.1.0")
	}
	if second.Arch != "arm64" {
		t.Errorf("Arch = %q, want %q", second.Arch, "arm64")
	}

	nodes, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("ListNodes() returned %d nodes, want 1", len(nodes))
	}
	if nodes[0].AgentVersion != "v1.1.0" {
		t.Errorf("stored AgentVersion = %q, want %q", nodes[0].AgentVersion, "v1.1.0")
	}
}

func TestListNodesReturnsEmptySlice(t *testing.T) {
	s := openTestStore(t)

	nodes, err := s.ListNodes(context.Background())
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}
	if nodes == nil {
		t.Fatal("ListNodes() returned nil, want empty slice")
	}
	if len(nodes) != 0 {
		t.Fatalf("ListNodes() returned %d nodes, want 0", len(nodes))
	}
}

func TestListNodesOrdersByName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	for _, name := range []string{"web-2", "alpha", "beta"} {
		if _, err := s.UpsertLegacyNode(ctx, Node{Name: name}); err != nil {
			t.Fatalf("UpsertLegacyNode(%q) failed: %v", name, err)
		}
	}

	nodes, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}

	want := []string{"alpha", "beta", "web-2"}
	if len(nodes) != len(want) {
		t.Fatalf("ListNodes() returned %d nodes, want %d", len(nodes), len(want))
	}
	for i, name := range want {
		if nodes[i].Name != name {
			t.Errorf("nodes[%d].Name = %q, want %q", i, nodes[i].Name, name)
		}
	}
}

func TestTouchNodeUpdatesLastSeen(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	node, err := s.UpsertLegacyNode(ctx, Node{Name: "dev-box"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode() failed: %v", err)
	}

	at := time.Date(2026, 11, 1, 12, 30, 0, 0, time.UTC)
	if err := s.TouchNode(ctx, node.ID, at); err != nil {
		t.Fatalf("TouchNode() failed: %v", err)
	}

	nodes, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("ListNodes() returned %d nodes, want 1", len(nodes))
	}
	if !nodes[0].LastSeen.Equal(at) {
		t.Errorf("LastSeen = %v, want %v", nodes[0].LastSeen, at)
	}
}

func TestTouchNodeUnknownIDReturnsError(t *testing.T) {
	s := openTestStore(t)

	err := s.TouchNode(context.Background(), "node_missing", time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("TouchNode() with unknown ID should fail")
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open() failed: %v", err)
	}

	if err := s.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close() failed: %v", err)
	}
}

func TestOpenCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "controlplane.db")

	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open() with missing parent directory failed: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() failed: %v", err)
		}
	})

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected database file at %q: %v", path, err)
	}
}

// TestUpsertLegacyNodeIgnoresSuppliedIDOnInsert 防止调用方传入的 ID 覆盖已有节点的行：
// 名称不存在时必须生成新 ID，而不是沿用请求里的 ID。
func TestUpsertLegacyNodeIgnoresSuppliedIDOnInsert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	existing, err := s.UpsertLegacyNode(ctx, Node{Name: "alpha", AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("first UpsertLegacyNode() failed: %v", err)
	}

	inserted, err := s.UpsertLegacyNode(ctx, Node{ID: existing.ID, Name: "beta", AgentVersion: "v2"})
	if err != nil {
		t.Fatalf("second UpsertLegacyNode() failed: %v", err)
	}
	if inserted.ID == existing.ID {
		t.Fatalf("UpsertLegacyNode() reused caller-supplied ID %q", inserted.ID)
	}
	if !strings.HasPrefix(inserted.ID, "node_") {
		t.Errorf("ID = %q, want prefix %q", inserted.ID, "node_")
	}
	if len(inserted.ID) != len("node_")+16 {
		t.Errorf("ID = %q, want node_ followed by 16 hex chars", inserted.ID)
	}

	nodes, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}
	if len(nodes) != 2 {
		t.Fatalf("ListNodes() returned %d nodes, want 2", len(nodes))
	}

	byID := make(map[string]Node, len(nodes))
	for _, n := range nodes {
		byID[n.ID] = n
	}
	if byID[existing.ID].Name != "alpha" || byID[existing.ID].AgentVersion != "v1" {
		t.Errorf("existing node was modified: %+v", byID[existing.ID])
	}
	if byID[inserted.ID].Name != "beta" {
		t.Errorf("inserted node = %+v, want name beta", byID[inserted.ID])
	}
}

func TestOpenMigratesDevicesTable(t *testing.T) {
	s := openTestStore(t)

	if !s.db.Migrator().HasTable(&Device{}) {
		t.Fatal("expected devices table to be migrated")
	}
}

func TestUpsertDeviceReusesRowForSameToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first, err := s.UpsertDevice(ctx, Device{Name: "pixel", Platform: "android", PushToken: "tok-a"})
	if err != nil {
		t.Fatalf("UpsertDevice() failed: %v", err)
	}
	if !strings.HasPrefix(first.ID, "device_") || len(first.ID) != len("device_")+16 {
		t.Fatalf("ID = %q, want device_ + 16 hex chars", first.ID)
	}

	second, err := s.UpsertDevice(ctx, Device{Name: "pixel2", Platform: "android", PushToken: "tok-a"})
	if err != nil {
		t.Fatalf("UpsertDevice() second failed: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("ID = %q, want %q (same push token)", second.ID, first.ID)
	}
	if second.Name != "pixel2" {
		t.Errorf("Name = %q, want pixel2", second.Name)
	}

	all, err := s.ListDevices(ctx)
	if err != nil {
		t.Fatalf("ListDevices() failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("len(devices) = %d, want 1", len(all))
	}
}

func TestUpsertDeviceRequiresTokenAndPlatform(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertDevice(ctx, Device{Name: "x", Platform: "android"}); err == nil {
		t.Error("UpsertDevice() without token: want error")
	}
	if _, err := s.UpsertDevice(ctx, Device{Name: "x", Platform: "windows", PushToken: "t"}); err == nil {
		t.Error("UpsertDevice() with unknown platform: want error")
	}
	if _, err := s.UpsertDevice(ctx, Device{Name: " ", Platform: "android", PushToken: "t"}); err == nil {
		t.Error("UpsertDevice() without name: want error")
	}
}

func TestDeleteDeviceReportsMissingRow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.DeleteDevice(ctx, "device_missing"); err == nil {
		t.Error("DeleteDevice() on missing row: want error")
	}

	device, err := s.UpsertDevice(ctx, Device{Name: "pixel", Platform: "android", PushToken: "tok-a"})
	if err != nil {
		t.Fatalf("UpsertDevice() failed: %v", err)
	}
	if err := s.DeleteDevice(ctx, device.ID); err != nil {
		t.Fatalf("DeleteDevice() failed: %v", err)
	}
	if all, _ := s.ListDevices(ctx); len(all) != 0 {
		t.Fatalf("len(devices) = %d, want 0", len(all))
	}
}

func TestUpsertProjectIndexIsIdempotent(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	first := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	second := first.Add(time.Hour)

	rows := []ProjectIndex{{
		ProjectKey: "abc",
		NodeID:     "node_1",
		ProjectID:  "abc",
		Canonical:  "/w/repo",
		VCS:        "git",
		LastSeen:   first,
	}}
	if err := s.UpsertProjectIndex(ctx, rows); err != nil {
		t.Fatal(err)
	}
	rows[0].Canonical = "/w/repo2"
	rows[0].LastSeen = second
	if err := s.UpsertProjectIndex(ctx, rows); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListProjectIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("rows = %#v, want one row", got)
	}
	if got[0].Canonical != "/w/repo2" || !got[0].LastSeen.Equal(second) {
		t.Fatalf("row = %#v, want the second write", got[0])
	}
}

func TestListProjectIndexKeepsOfflineNodes(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	rows := []ProjectIndex{
		{ProjectKey: "abc", NodeID: "node_1", ProjectID: "abc", Canonical: "/w/repo"},
		{ProjectKey: "abc", NodeID: "node_2", ProjectID: "abc", Canonical: "/w/repo"},
	}
	if err := s.UpsertProjectIndex(ctx, rows); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListProjectIndex(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("rows = %#v, want two rows", got)
	}
}

func TestSetProjectPrimaryNodeCreatesRow(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.SetProjectPrimaryNode(ctx, "abc", "node_1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectPrimaryNode(ctx, "abc", "node_2"); err != nil {
		t.Fatal(err)
	}

	prefs, err := s.ListProjectPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs) != 1 || prefs[0].PrimaryNodeID != "node_2" {
		t.Fatalf("preferences = %#v, want one row pointing at node_2", prefs)
	}
}

func TestPreferencesRoundTrip(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.SetPreference(ctx, "inbox.dir.node_1", "/w/inbox"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPreference(ctx, "inbox.dir.node_1", "/w/other"); err != nil {
		t.Fatal(err)
	}

	value, ok, err := s.GetPreference(ctx, "inbox.dir.node_1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || value != "/w/other" {
		t.Fatalf("value = %q, ok = %v; want the last write", value, ok)
	}

	all, err := s.ListPreferences(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all["inbox.dir.node_1"] != "/w/other" {
		t.Fatalf("preferences = %#v", all)
	}

	if _, ok, err := s.GetPreference(ctx, "missing"); err != nil || ok {
		t.Fatalf("missing preference: ok = %v, err = %v", ok, err)
	}
}

// TestPreferencesAreScopedPerUser 固化账号命名空间：同一个 key 在不同账号下
// 互不可见，列表返回的是剥掉前缀后的裸 key。
func TestPreferencesAreScopedPerUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.SetPreferenceFor(ctx, "u1", "last_dir", "/a"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetPreferenceFor(ctx, "u2", "last_dir", "/b"); err != nil {
		t.Fatalf("set: %v", err)
	}
	v1, _, _ := s.GetPreferenceFor(ctx, "u1", "last_dir")
	v2, _, _ := s.GetPreferenceFor(ctx, "u2", "last_dir")
	if v1 != "/a" || v2 != "/b" {
		t.Fatalf("v1 = %q, v2 = %q", v1, v2)
	}
	if all, _ := s.ListPreferencesFor(ctx, "u1"); all["last_dir"] != "/a" || len(all) != 1 {
		t.Fatalf("all = %v", all)
	}
}

// TestPreferenceLegacyNamespaceIsIsolatedFromAccounts 固化空 userID 的语义：
// local / admin_token 继续使用未加前缀的历史键，但绝不能看到任何账号的偏好。
func TestPreferenceLegacyNamespaceIsIsolatedFromAccounts(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetPreferenceFor(ctx, "u1", "last_dir", "/a"); err != nil {
		t.Fatalf("set account preference: %v", err)
	}
	if err := s.SetPreference(ctx, "legacy_dir", "/legacy"); err != nil {
		t.Fatalf("set legacy preference: %v", err)
	}

	legacy, err := s.ListPreferencesFor(ctx, "")
	if err != nil {
		t.Fatalf("list legacy: %v", err)
	}
	if len(legacy) != 1 || legacy["legacy_dir"] != "/legacy" {
		t.Fatalf("legacy preferences = %#v, want only the unprefixed key", legacy)
	}

	account, err := s.ListPreferencesFor(ctx, "u1")
	if err != nil {
		t.Fatalf("list account: %v", err)
	}
	if len(account) != 1 || account["last_dir"] != "/a" {
		t.Fatalf("account preferences = %#v, want only the account key", account)
	}

	// 读写都按命名空间精确匹配，不会跨命名空间命中。
	if _, ok, err := s.GetPreferenceFor(ctx, "", "last_dir"); err != nil || ok {
		t.Fatalf("legacy read of an account key: ok = %v, err = %v", ok, err)
	}
	if _, ok, err := s.GetPreferenceFor(ctx, "u1", "legacy_dir"); err != nil || ok {
		t.Fatalf("account read of a legacy key: ok = %v, err = %v", ok, err)
	}

	// 不改主键的代价是键名本身带上前缀：直接看底层行，防止实现退化成
	// 「只在读接口里过滤、写接口仍写裸 key」。
	var stored []Preference
	if err := s.db.WithContext(ctx).Where("key = ?", "u/u1/last_dir").Find(&stored).Error; err != nil {
		t.Fatalf("load stored rows: %v", err)
	}
	if len(stored) != 1 || stored[0].Value != "/a" {
		t.Fatalf("stored rows = %#v, want one prefixed row", stored)
	}
}

// TestListPreferencesForDoesNotBlendSimilarUserIDs 覆盖前缀边界：u1 与 u11
// 的前缀只差一个字符，末尾的斜杠必须把它们分开。
func TestListPreferencesForDoesNotBlendSimilarUserIDs(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetPreferenceFor(ctx, "u1", "last_dir", "/a"); err != nil {
		t.Fatalf("set u1: %v", err)
	}
	if err := s.SetPreferenceFor(ctx, "u11", "last_dir", "/b"); err != nil {
		t.Fatalf("set u11: %v", err)
	}

	first, err := s.ListPreferencesFor(ctx, "u1")
	if err != nil {
		t.Fatalf("list u1: %v", err)
	}
	if len(first) != 1 || first["last_dir"] != "/a" {
		t.Fatalf("u1 preferences = %#v", first)
	}

	second, err := s.ListPreferencesFor(ctx, "u11")
	if err != nil {
		t.Fatalf("list u11: %v", err)
	}
	if len(second) != 1 || second["last_dir"] != "/b" {
		t.Fatalf("u11 preferences = %#v", second)
	}
}

// TestProjectPreferencesAreScopedPerUser 固化项目偏好同样按账号隔离，
// 返回行的 ProjectKey 已剥掉前缀。
func TestProjectPreferencesAreScopedPerUser(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetProjectPrimaryNodeFor(ctx, "u1", "abc", "node_1"); err != nil {
		t.Fatalf("set u1: %v", err)
	}
	if err := s.SetProjectPrimaryNodeFor(ctx, "u2", "abc", "node_2"); err != nil {
		t.Fatalf("set u2: %v", err)
	}
	if err := s.SetProjectPrimaryNodeFor(ctx, "u1", "abc", "node_3"); err != nil {
		t.Fatalf("overwrite u1: %v", err)
	}

	first, err := s.ListProjectPreferencesFor(ctx, "u1")
	if err != nil {
		t.Fatalf("list u1: %v", err)
	}
	if len(first) != 1 || first[0].ProjectKey != "abc" || first[0].PrimaryNodeID != "node_3" {
		t.Fatalf("u1 preferences = %#v, want abc -> node_3", first)
	}

	second, err := s.ListProjectPreferencesFor(ctx, "u2")
	if err != nil {
		t.Fatalf("list u2: %v", err)
	}
	if len(second) != 1 || second[0].ProjectKey != "abc" || second[0].PrimaryNodeID != "node_2" {
		t.Fatalf("u2 preferences = %#v, want abc -> node_2", second)
	}

	// 历史命名空间里没有账号写入的行。
	legacy, err := s.ListProjectPreferences(ctx)
	if err != nil {
		t.Fatalf("list legacy: %v", err)
	}
	if len(legacy) != 0 {
		t.Fatalf("legacy project preferences = %#v, want none", legacy)
	}
}

// TestDeprecatedPreferenceWrappersUseLegacyNamespace 固化旧方法只是
// userID == "" 的包装：行为与改造前一致，且不触碰任何账号命名空间。
func TestDeprecatedPreferenceWrappersUseLegacyNamespace(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SetPreference(ctx, "last_dir", "/legacy"); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	if value, ok, err := s.GetPreferenceFor(ctx, "", "last_dir"); err != nil || !ok || value != "/legacy" {
		t.Fatalf("legacy read through For = %q, %v, %v", value, ok, err)
	}

	all, err := s.ListPreferences(ctx)
	if err != nil {
		t.Fatalf("list preferences: %v", err)
	}
	if len(all) != 1 || all["last_dir"] != "/legacy" {
		t.Fatalf("preferences = %#v", all)
	}

	if err := s.SetProjectPrimaryNode(ctx, "abc", "node_1"); err != nil {
		t.Fatalf("set primary node: %v", err)
	}
	prefs, err := s.ListProjectPreferencesFor(ctx, "")
	if err != nil {
		t.Fatalf("list project preferences: %v", err)
	}
	if len(prefs) != 1 || prefs[0].ProjectKey != "abc" || prefs[0].PrimaryNodeID != "node_1" {
		t.Fatalf("project preferences = %#v", prefs)
	}

	account, err := s.ListPreferencesFor(ctx, "u1")
	if err != nil {
		t.Fatalf("list account preferences: %v", err)
	}
	if len(account) != 0 {
		t.Fatalf("account preferences = %#v, want none", account)
	}
}
