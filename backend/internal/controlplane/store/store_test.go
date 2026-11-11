package store

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
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

func TestUpsertNodeCreatesNodeWithGeneratedID(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()
	lastSeen := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)

	got, err := s.UpsertNode(ctx, Node{
		Name:         "dev-box",
		OS:           "linux",
		Arch:         "amd64",
		AgentVersion: "v1.0.0",
		Capabilities: []string{"shell", "file"},
		LastSeen:     lastSeen,
	})
	if err != nil {
		t.Fatalf("UpsertNode() failed: %v", err)
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

func TestUpsertNodeReusesIDForSameName(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first, err := s.UpsertNode(ctx, Node{Name: "dev-box", AgentVersion: "v1.0.0"})
	if err != nil {
		t.Fatalf("first UpsertNode() failed: %v", err)
	}

	second, err := s.UpsertNode(ctx, Node{
		Name:         "dev-box",
		OS:           "linux",
		Arch:         "arm64",
		AgentVersion: "v1.1.0",
		Capabilities: []string{"shell"},
	})
	if err != nil {
		t.Fatalf("second UpsertNode() failed: %v", err)
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
		if _, err := s.UpsertNode(ctx, Node{Name: name}); err != nil {
			t.Fatalf("UpsertNode(%q) failed: %v", name, err)
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

	node, err := s.UpsertNode(ctx, Node{Name: "dev-box"})
	if err != nil {
		t.Fatalf("UpsertNode() failed: %v", err)
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

// TestUpsertNodeIgnoresSuppliedIDOnInsert 防止调用方传入的 ID 覆盖已有节点的行：
// 名称不存在时必须生成新 ID，而不是沿用请求里的 ID。
func TestUpsertNodeIgnoresSuppliedIDOnInsert(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	existing, err := s.UpsertNode(ctx, Node{Name: "alpha", AgentVersion: "v1"})
	if err != nil {
		t.Fatalf("first UpsertNode() failed: %v", err)
	}

	inserted, err := s.UpsertNode(ctx, Node{ID: existing.ID, Name: "beta", AgentVersion: "v2"})
	if err != nil {
		t.Fatalf("second UpsertNode() failed: %v", err)
	}
	if inserted.ID == existing.ID {
		t.Fatalf("UpsertNode() reused caller-supplied ID %q", inserted.ID)
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
