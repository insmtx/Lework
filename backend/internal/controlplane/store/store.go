// Package store 提供控制面的 SQLite 节点存储。
//
// 控制面是单用户本地服务，节点数据量小且只需单机持久化，
// 因此使用 SQLite + GORM；调用方通过 context 控制查询生命周期。
//
// 驱动选用纯 Go 的 github.com/glebarez/sqlite（底层 modernc.org/sqlite），
// 而不是依赖 cgo 的 gorm.io/driver/sqlite：发布镜像与跨平台构建都在
// CGO_ENABLED=0 下编译，cgo 驱动会让 lework serve 在这些产物里无法打开库。
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
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

// Device 是注册了推送通知的终端。它只保存推送令牌，不保存认证凭据：
// 控制面的鉴权走共享 enrollment token，设备表存在的意义是知道往哪儿推。
type Device struct {
	ID        string `gorm:"primaryKey"`
	Name      string `gorm:"not null"`
	Platform  string `gorm:"not null"`
	PushToken string `gorm:"uniqueIndex;not null"`
	LastSeen  time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
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

	// 默认数据库位于用户配置目录下，首次运行时父目录可能不存在。
	// 父目录权限收紧到 0700：其中保存的是本机节点注册信息。
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("create store directory for %q: %w", path, err)
		}
	}

	dsn, err := sqliteDSN(path)
	if err != nil {
		return nil, err
	}

	database, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open sqlite %q: %w", path, err)
	}

	if path == ":memory:" {
		// 内存库的生命周期与连接绑定：连接池若再打开一条连接，会得到另一个
		// 空库（表现为 no such table）。共享缓存让所有连接指向同一个内存库，
		// 单连接池则保证任何时刻只有一个句柄持有它。
		if err := pinMemoryConnection(database); err != nil {
			return nil, err
		}
	}

	if err := database.AutoMigrate(&Node{}, &Device{}); err != nil {
		// 迁移失败时主动释放底层连接，避免句柄泄漏。
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("migrate nodes table: %w", err)
	}

	return &Store{db: database}, nil
}

// sqliteDSN 返回打开的 DSN。文件库原样使用；内存库改用带唯一名称的共享
// 缓存 DSN，避免连接池中每条连接各自得到一个独立空库，同时保证不同 Store
// 之间互不干扰。
func sqliteDSN(path string) (string, error) {
	if path != ":memory:" {
		return path, nil
	}

	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate memory database name: %w", err)
	}
	return "file:lework-memory-" + hex.EncodeToString(buf) + "?mode=memory&cache=shared", nil
}

// pinMemoryConnection 把内存库限制在单个连接上。
//
// 共享缓存的内存库在最后一个连接关闭时即被销毁，而连接池可能因并发压力
// 打开第二条连接；固定为单连接后既不会出现第二个空库，也不会让库在两次
// 查询之间消失。SQLite 写入本身串行，单连接不会降低实际吞吐。
func pinMemoryConnection(database *gorm.DB) error {
	sqlDB, err := database.DB()
	if err != nil {
		return fmt.Errorf("get sql database for memory store: %w", err)
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	// 保持空闲连接不被回收，否则最后一个连接关闭会销毁内存库。
	sqlDB.SetConnMaxIdleTime(0)
	sqlDB.SetConnMaxLifetime(0)
	return nil
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
	} else {
		// 名称不存在时一律生成新 ID：沿用调用方传入的 ID 会在 GORM Save 时
		// 覆盖另一条节点的行，导致静默改错记录。
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

// UpsertDevice 按推送令牌写入设备：同一令牌重复注册时复用原 ID 并更新
// 名称与平台，避免重装 App 后留下永远推不到的僵尸记录。
func (s *Store) UpsertDevice(ctx context.Context, d Device) (Device, error) {
	if strings.TrimSpace(d.PushToken) == "" {
		return Device{}, errors.New("push token is required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return Device{}, errors.New("device name is required")
	}
	if d.Platform != "android" && d.Platform != "ios" {
		return Device{}, fmt.Errorf("unsupported platform %q", d.Platform)
	}

	// 与 UpsertNode 同理：用 Find 查询，未命中是正常分支，不产生日志噪声。
	var existing []Device
	res := s.db.WithContext(ctx).Where("push_token = ?", d.PushToken).Limit(1).Find(&existing)
	if res.Error != nil {
		return Device{}, fmt.Errorf("lookup device: %w", res.Error)
	}

	if len(existing) > 0 {
		d.ID = existing[0].ID
		d.CreatedAt = existing[0].CreatedAt
	} else {
		id, idErr := newDeviceID()
		if idErr != nil {
			return Device{}, idErr
		}
		d.ID = id
	}

	if err := s.db.WithContext(ctx).Save(&d).Error; err != nil {
		return Device{}, fmt.Errorf("save device: %w", err)
	}
	return d, nil
}

// ListDevices 返回全部设备，按创建时间升序；空库返回空切片而非 nil。
func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	devices := make([]Device, 0)
	if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&devices).Error; err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

// DeleteDevice 删除设备；不存在时返回错误，让调用方能回 404 而不是假装成功。
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	res := s.db.WithContext(ctx).Delete(&Device{}, "id = ?", id)
	if res.Error != nil {
		return fmt.Errorf("delete device %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("delete device %q: device not found", id)
	}
	return nil
}

// TouchDevice 记录设备最近一次活动时间，用于判断推送是否还有意义。
func (s *Store) TouchDevice(ctx context.Context, id string, at time.Time) error {
	res := s.db.WithContext(ctx).Model(&Device{}).Where("id = ?", id).Update("last_seen", at)
	if res.Error != nil {
		return fmt.Errorf("touch device %q: %w", id, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("touch device %q: device not found", id)
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

// newDeviceID 生成 device 前缀的随机 ID，规则与节点一致。
func newDeviceID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate device id: %w", err)
	}
	return "device_" + hex.EncodeToString(buf), nil
}
