// Package store 提供控制面的 SQLite 节点存储。
//
// 控制面是单用户本地服务，节点数据量小且只需单机持久化，
// 因此使用 SQLite + GORM；调用方通过 context 控制查询生命周期。
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Node 是注册到控制面的节点记录。
type Node struct {
	ID           string `gorm:"primaryKey"`
	Name         string `gorm:"uniqueIndex;not null"`
	OS           string
	Arch         string
	AgentVersion string
	Capabilities []string `gorm:"serializer:json"`
	LastSeen     time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Store 是节点存储，持有 SQLite 连接。
type Store struct {
	db     *gorm.DB
	mu     sync.Mutex
	closed bool
}

// Open 打开 path 处的 SQLite 数据库并迁移节点表。
// path 为 ":memory:" 时使用内存数据库，便于测试。
func Open(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store path is required")
	}

	database, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	if err := database.AutoMigrate(&Node{}); err != nil {
		// 迁移失败时主动释放底层连接，避免句柄泄漏。
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("migrate nodes table: %w", err)
	}

	return &Store{db: database}, nil
}

// Close 关闭底层数据库连接，可重复调用。
func (s *Store) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	sqlDB, err := s.db.DB()
	if err != nil {
		return fmt.Errorf("get sql database: %w", err)
	}
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}
	return nil
}

// UpsertNode 按名称写入节点记录：已存在时复用原 ID 并更新可变字段，否则生成新 ID 插入。
// 返回落库后的记录（含 ID 与时间戳）。
func (s *Store) UpsertNode(ctx context.Context, n Node) (Node, error) {
	if strings.TrimSpace(n.Name) == "" {
		return Node{}, errors.New("node name is required")
	}

	// 用 Find 而非 First 查询：未命中是正常分支，不应产生 record not found 日志。
	var existing []Node
	res := s.db.WithContext(ctx).Where("name = ?", n.Name).Limit(1).Find(&existing)
	if res.Error != nil {
		return Node{}, fmt.Errorf("lookup node %q: %w", n.Name, res.Error)
	}

	if len(existing) > 0 {
		n.ID = existing[0].ID
		n.CreatedAt = existing[0].CreatedAt
	} else if n.ID == "" {
		id, idErr := newNodeID()
		if idErr != nil {
			return Node{}, idErr
		}
		n.ID = id
	}

	if err := s.db.WithContext(ctx).Save(&n).Error; err != nil {
		return Node{}, fmt.Errorf("save node %q: %w", n.Name, err)
	}
	return n, nil
}

// ListNodes 返回全部节点，按名称升序排列；空库返回空切片而非 nil。
func (s *Store) ListNodes(ctx context.Context) ([]Node, error) {
	nodes := make([]Node, 0)
	if err := s.db.WithContext(ctx).Order("name ASC").Find(&nodes).Error; err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	return nodes, nil
}

// TouchNode 更新节点的最后心跳时间；节点不存在时返回错误。
func (s *Store) TouchNode(ctx context.Context, id string, at time.Time) error {
	res := s.db.WithContext(ctx).Model(&Node{}).Where("id = ?", id).Update("last_seen", at)
	if res.Error != nil {
		return fmt.Errorf("touch node %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("touch node %q: node not found", id)
	}
	return nil
}

// newNodeID 生成 node 前缀的随机 ID（8 字节随机数的十六进制）。
func newNodeID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate node id: %w", err)
	}
	return "node_" + hex.EncodeToString(buf), nil
}
