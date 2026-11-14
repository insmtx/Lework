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
	"gorm.io/gorm/clause"
)

// Node 是注册到控制面的节点记录。
type Node struct {
	ID             string `gorm:"primaryKey"`
	Name           string `gorm:"uniqueIndex;not null"`
	OS             string
	Arch           string
	AgentVersion   string
	Capabilities   []string `gorm:"serializer:json"`
	WorkspaceRoots []string `gorm:"serializer:json"`
	LastSeen       time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
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

// ProjectIndex 记录某个节点上「见过」的项目元数据。
//
// 只缓存项目身份（id、canonical、vcs）与最后出现时间，不缓存会话、消息
// 或任何执行状态：那些永远以 OpenCode 为准。节点离线时浏览器仍能列出
// 项目卡片，靠的就是这张表。
type ProjectIndex struct {
	// ProjectKey 是聚合键：通常等于 ProjectID，但 OpenCode 把所有无提交的
	// git 仓库都报成 "global"，此时用 ProjectID + ":" + Canonical 区分。
	ProjectKey string `gorm:"primaryKey;column:project_key"`
	NodeID     string `gorm:"primaryKey;column:node_id"`
	ProjectID  string `gorm:"column:project_id"`
	Canonical  string
	VCS        string
	LastSeen   time.Time
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// ProjectPreference 保存用户对单个项目的偏好：主节点与显示别名。
type ProjectPreference struct {
	ProjectKey    string `gorm:"primaryKey;column:project_key"`
	PrimaryNodeID string `gorm:"column:primary_node_id"`
	Alias         string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Preference 是通用的键值偏好（我的目录、上次目标等）。
// 只存用户选择，不存任何会话事实。
type Preference struct {
	Key       string `gorm:"primaryKey;column:key"`
	Value     string
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

	if err := database.AutoMigrate(&Node{}, &Device{}, &ProjectIndex{}, &ProjectPreference{}, &Preference{}); err != nil {
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

// UpsertProjectIndex 批量写入节点上的项目索引。
//
// 同一 (project_key, node_id) 重复出现时覆盖可变字段而不是插入新行：
// 项目可能在两次拉取之间改过 canonical（例如仓库重新初始化），以最新
// 一次为准。rows 为空是正常情况（节点没有任何项目），直接返回。
func (s *Store) UpsertProjectIndex(ctx context.Context, rows []ProjectIndex) error {
	if len(rows) == 0 {
		return nil
	}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_key"}, {Name: "node_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"project_id", "canonical", "vcs", "last_seen", "updated_at"}),
	}).Create(&rows).Error; err != nil {
		return fmt.Errorf("upsert project index: %w", err)
	}
	return nil
}

// ListProjectIndex 返回全部项目索引（含已离线节点），按最后出现时间倒序。
func (s *Store) ListProjectIndex(ctx context.Context) ([]ProjectIndex, error) {
	rows := make([]ProjectIndex, 0)
	if err := s.db.WithContext(ctx).Order("last_seen DESC").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list project index: %w", err)
	}
	return rows, nil
}

// ListProjectPreferences 返回全部项目偏好；空库返回空切片而非 nil。
func (s *Store) ListProjectPreferences(ctx context.Context) ([]ProjectPreference, error) {
	prefs := make([]ProjectPreference, 0)
	if err := s.db.WithContext(ctx).Find(&prefs).Error; err != nil {
		return nil, fmt.Errorf("list project preferences: %w", err)
	}
	return prefs, nil
}

// SetProjectPrimaryNode 设置项目的主节点。首次设置时建行；重复设置覆盖
// 主节点但保留别名，避免改节点顺手丢掉用户起的名字。
func (s *Store) SetProjectPrimaryNode(ctx context.Context, projectKey, nodeID string) error {
	if strings.TrimSpace(projectKey) == "" {
		return errors.New("project key is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}
	row := ProjectPreference{ProjectKey: projectKey, PrimaryNodeID: nodeID}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"primary_node_id", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("set primary node for %q: %w", projectKey, err)
	}
	return nil
}

// GetPreference 读取一条偏好；不存在时 ok 为 false 且不视为错误，
// 因为「用户还没设置过」是正常状态。
func (s *Store) GetPreference(ctx context.Context, key string) (string, bool, error) {
	var rows []Preference
	if err := s.db.WithContext(ctx).Where("key = ?", key).Limit(1).Find(&rows).Error; err != nil {
		return "", false, fmt.Errorf("get preference %q: %w", key, err)
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].Value, true, nil
}

// SetPreference 写入或覆盖一条偏好。空 key 视为调用方错误。
func (s *Store) SetPreference(ctx context.Context, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("preference key is required")
	}
	row := Preference{Key: key, Value: value}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("set preference %q: %w", key, err)
	}
	return nil
}

// ListPreferences 一次性返回全部偏好，供客户端启动时整包同步。
func (s *Store) ListPreferences(ctx context.Context) (map[string]string, error) {
	rows := make([]Preference, 0)
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
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
