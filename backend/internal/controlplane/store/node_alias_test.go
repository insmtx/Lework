package store

import (
	"context"
	"testing"
)

// TestNodeAliasIsScopedToAccount 固化「别名只管自己」的语义：同一台节点上，
// 每个账号读到的是自己设置的名字，不会覆盖别人的视图。
func TestNodeAliasIsScopedToAccount(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	node, err := s.UpsertLegacyNode(ctx, Node{Name: "host-a"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}

	if err := s.SetNodeAliasFor(ctx, "alice", node.ID, "  客厅的电脑  "); err != nil {
		t.Fatalf("SetNodeAliasFor: %v", err)
	}

	alias, ok, err := s.GetNodeAliasFor(ctx, "alice", node.ID)
	if err != nil {
		t.Fatalf("GetNodeAliasFor: %v", err)
	}
	if !ok || alias != "客厅的电脑" {
		t.Fatalf("alias = %q, ok = %v; want trimmed name for alice", alias, ok)
	}

	if _, ok, err := s.GetNodeAliasFor(ctx, "bob", node.ID); err != nil {
		t.Fatalf("GetNodeAliasFor bob: %v", err)
	} else if ok {
		t.Fatal("bob must not see alice's node alias")
	}

	aliases, err := s.ListNodeAliasesFor(ctx, "alice")
	if err != nil {
		t.Fatalf("ListNodeAliasesFor: %v", err)
	}
	if aliases[node.ID] != "客厅的电脑" {
		t.Fatalf("aliases = %v, want acme name for node %s", aliases, node.ID)
	}

	bobAliases, err := s.ListNodeAliasesFor(ctx, "bob")
	if err != nil {
		t.Fatalf("ListNodeAliasesFor bob: %v", err)
	}
	if len(bobAliases) != 0 {
		t.Fatalf("bob aliases = %v, want none", bobAliases)
	}
}

// TestNodeAliasBlankClearsAlias 保证清空别名后回退到 agent 上报的主机名，
// 而不是留下一条空字符串把名字变成空白。
func TestNodeAliasBlankClearsAlias(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	node, err := s.UpsertLegacyNode(ctx, Node{Name: "host-a"})
	if err != nil {
		t.Fatalf("UpsertLegacyNode: %v", err)
	}
	if err := s.SetNodeAliasFor(ctx, "alice", node.ID, "客厅的电脑"); err != nil {
		t.Fatalf("SetNodeAliasFor: %v", err)
	}
	if err := s.SetNodeAliasFor(ctx, "alice", node.ID, "   "); err != nil {
		t.Fatalf("clear alias: %v", err)
	}

	if alias, ok, err := s.GetNodeAliasFor(ctx, "alice", node.ID); err != nil {
		t.Fatalf("GetNodeAliasFor: %v", err)
	} else if ok || alias != "" {
		t.Fatalf("alias after clear = %q, ok = %v; want none", alias, ok)
	}

	aliases, err := s.ListNodeAliasesFor(ctx, "alice")
	if err != nil {
		t.Fatalf("ListNodeAliasesFor: %v", err)
	}
	if _, ok := aliases[node.ID]; ok {
		t.Fatalf("aliases after clear = %v, want no entry", aliases)
	}
}
