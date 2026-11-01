package store

import (
	"context"
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
