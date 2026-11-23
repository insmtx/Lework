package store

import (
	"context"
	"testing"
)

// TestNodeColorIsScopedToAccount 固化「颜色只管自己」：同一台节点上，
// 每个账号读到的是自己设置的颜色，不会覆盖别人的视图。
func TestNodeColorIsScopedToAccount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	node, err := s.UpsertLegacyNode(ctx, Node{Name: "host-a"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}

	if err := s.SetNodeColorFor(ctx, "alice", node.ID, "  blue  "); err != nil {
		t.Fatalf("SetNodeColorFor: %v", err)
	}

	color, ok, err := s.GetNodeColorFor(ctx, "alice", node.ID)
	if err != nil {
		t.Fatalf("GetNodeColorFor: %v", err)
	}
	if !ok || color != "blue" {
		t.Fatalf("color = %q, ok = %v; want trimmed color for alice", color, ok)
	}

	if _, ok, err := s.GetNodeColorFor(ctx, "bob", node.ID); err != nil {
		t.Fatalf("GetNodeColorFor bob: %v", err)
	} else if ok {
		t.Fatal("bob must not see alice's node color")
	}

	colors, err := s.ListNodeColorsFor(ctx, "alice")
	if err != nil {
		t.Fatalf("ListNodeColorsFor: %v", err)
	}
	if colors[node.ID] != "blue" {
		t.Fatalf("colors = %v, want blue for node %s", colors, node.ID)
	}

	bobColors, err := s.ListNodeColorsFor(ctx, "bob")
	if err != nil {
		t.Fatalf("ListNodeColorsFor bob: %v", err)
	}
	if len(bobColors) != 0 {
		t.Fatalf("bob colors = %v, want none", bobColors)
	}
}

// TestNodeColorBlankClearsColor 保证清空后回退到「未设色」，
// 而不是留下一条空字符串让前端每次都要额外判断。
func TestNodeColorBlankClearsColor(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	node, err := s.UpsertLegacyNode(ctx, Node{Name: "host-a"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if err := s.SetNodeColorFor(ctx, "alice", node.ID, "violet"); err != nil {
		t.Fatalf("SetNodeColorFor: %v", err)
	}
	if err := s.SetNodeColorFor(ctx, "alice", node.ID, "   "); err != nil {
		t.Fatalf("clear color: %v", err)
	}

	if color, ok, err := s.GetNodeColorFor(ctx, "alice", node.ID); err != nil {
		t.Fatalf("GetNodeColorFor: %v", err)
	} else if ok || color != "" {
		t.Fatalf("color after clear = %q, ok = %v; want none", color, ok)
	}

	colors, err := s.ListNodeColorsFor(ctx, "alice")
	if err != nil {
		t.Fatalf("ListNodeColorsFor: %v", err)
	}
	if _, ok := colors[node.ID]; ok {
		t.Fatalf("colors after clear = %v, want no entry", colors)
	}
}
