//go:build integration

package store

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	appconfig "github.com/insmtx/lework/backend/config"
)

// openTestPostgres 打开 LEWORK_TEST_DATABASE_URL 指向的测试库。
// 未配置时跳过：Postgres 集成测试依赖外部服务，默认测试集不应因此失败。
func openTestPostgres(t *testing.T) *Store {
	t.Helper()

	url := strings.TrimSpace(os.Getenv("LEWORK_TEST_DATABASE_URL"))
	if url == "" {
		t.Skip("LEWORK_TEST_DATABASE_URL is not set")
	}

	s, err := OpenPostgres(appconfig.DatabaseConfig{URL: url})
	if err != nil {
		t.Fatalf("OpenPostgres() failed: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("Close() failed: %v", err)
		}
	})
	return s
}

func TestOpenPostgresMigratesAndUpsertsNode(t *testing.T) {
	s := openTestPostgres(t)
	ctx := context.Background()

	// 集成测试共用同一个测试库，名称带时间戳避免与历史运行及唯一索引冲突。
	name := fmt.Sprintf("task3-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		// 只删除本次插入的行，绝不 truncate 或 drop 共享表。
		if err := s.db.WithContext(ctx).Where("name = ?", name).Delete(&Node{}).Error; err != nil {
			t.Errorf("cleanup node %q failed: %v", name, err)
		}
	})

	for _, model := range []any{&Node{}, &Device{}, &ProjectIndex{}, &ProjectPreference{}, &Preference{}} {
		if !s.db.Migrator().HasTable(model) {
			t.Fatalf("expected table for %T to be migrated", model)
		}
	}

	first, err := s.UpsertNode(ctx, Node{
		Name:           name,
		OS:             "linux",
		Arch:           "amd64",
		AgentVersion:   "test",
		Capabilities:   []string{"shell"},
		WorkspaceRoots: []string{"/tmp/work"},
		LastSeen:       time.Now().UTC().Truncate(time.Second),
	})
	if err != nil {
		t.Fatalf("UpsertNode() failed: %v", err)
	}
	if first.ID == "" {
		t.Fatal("expected generated node ID")
	}

	nodes, err := s.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes() failed: %v", err)
	}
	var found *Node
	for i := range nodes {
		if nodes[i].Name == name {
			found = &nodes[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected node %q in ListNodes(), got %d rows", name, len(nodes))
	}
	if found.ID != first.ID {
		t.Fatalf("ListNodes() returned ID %q, want %q", found.ID, first.ID)
	}
	if found.AgentVersion != "test" {
		t.Fatalf("ListNodes() returned agent version %q, want test", found.AgentVersion)
	}

	second, err := s.UpsertNode(ctx, Node{
		Name:         name,
		OS:           "linux",
		Arch:         "arm64",
		AgentVersion: "test-2",
		LastSeen:     time.Now().UTC().Truncate(time.Second),
	})
	if err != nil {
		t.Fatalf("second UpsertNode() failed: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("repeat upsert changed ID: first %q, second %q", first.ID, second.ID)
	}
	// PostgreSQL timestamptz 只有微秒精度：第二次读到的是落库后的值，
	// 与首次由 GORM 在本地生成的纳秒时间戳按微秒比较才有意义。
	if !second.CreatedAt.Truncate(time.Microsecond).Equal(first.CreatedAt.Truncate(time.Microsecond)) {
		t.Fatalf("repeat upsert changed CreatedAt: first %v, second %v", first.CreatedAt, second.CreatedAt)
	}
}

func TestOpenPostgresRejectsEmptyURL(t *testing.T) {
	_, err := OpenPostgres(appconfig.DatabaseConfig{})
	if err == nil {
		t.Fatal("expected error for empty database url")
	}
	// 错误信息不得回显 DSN（空 URL 场景下也不应带连接串片段）。
	if strings.Contains(err.Error(), "postgres://") {
		t.Fatalf("error leaks DSN: %v", err)
	}
}
