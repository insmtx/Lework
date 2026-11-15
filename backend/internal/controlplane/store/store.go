// Package store 提供控制面的节点存储（PostgreSQL / SQLite + GORM）。
//
// 生产环境使用 PostgreSQL（OpenPostgres），节点数据跨进程、跨实例共享；
// SQLite（Open）只用于单元测试和本地无外部依赖的临时启动，不承载生产数据。
// 调用方通过 context 控制查询生命周期。
//
// SQLite 驱动选用纯 Go 的 github.com/glebarez/sqlite（底层 modernc.org/sqlite），
// 而不是依赖 cgo 的 gorm.io/driver/sqlite：发布镜像与跨平台构建都在
// CGO_ENABLED=0 下编译，cgo 驱动会让二进制在 CGO_ENABLED=0 的产物里无法打开库。
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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"

	appconfig "github.com/insmtx/lework/backend/config"
)

// Node 是注册到控制面的节点记录。
//
// Name 只用于展示，因此不再是唯一键：多台机器同名是正常现象（比如两个人的
// 笔记本都叫 MacBook），归属由 NodeMember 表达，唯一性在应用层按 owner 保证。
// 旧库上的唯一索引 idx_nodes_name 由 migrateControlPlane 显式删除，
// 保留的 idx_nodes_name_lookup 只用于按名查找。not null 必须保留：它只表达
// 「展示名不能为空」，与唯一性无关，去掉会让升级库上的 schema 永久漂移。
//
// CredentialHash 存 sha256(节点凭证)，空串表示节点尚未激活（旧令牌直通或
// 配对未完成）。它是节点身份的唯一依据；明文凭证只在签发时出现在 agent
// 侧与 welcome 帧里，任何代码路径都不得把它写库或写日志。
type Node struct {
	ID             string `gorm:"primaryKey"`
	Name           string `gorm:"index:idx_nodes_name_lookup;not null"`
	OS             string
	Arch           string
	AgentVersion   string
	Capabilities   []string `gorm:"serializer:json"`
	WorkspaceRoots []string `gorm:"serializer:json"`
	CredentialHash string   `gorm:"index"`
	ClaimedAt      time.Time
	LastSeen       time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NodeMember 是「谁可以用这台节点」的归属行；nodes 表不存 owner_id。
//
// 一个节点可以有多个成员（owner 共享给别人），因此主键是 (node_id, user_id)，
// 角色只是这一行上的标记：owner 能共享/撤销，member 只能使用。撤销访问就是
// 删行，不需要迁移。没有成员行的节点是「未认领」的历史节点，仅管理员可见。
type NodeMember struct {
	NodeID    string `gorm:"primaryKey;column:node_id"`
	UserID    string `gorm:"primaryKey;column:user_id;index:idx_node_member_user"`
	Role      string // owner | member
	GrantedBy string
	CreatedAt time.Time
}

// Device 是注册了推送通知的终端。它只保存推送令牌，不保存认证凭据：
// 控制面的鉴权走共享 enrollment token，设备表存在的意义是知道往哪儿推。
//
// UserID 是设备主人。推送按「事件所在节点的成员」选择设备（devices JOIN
// node_members），因此没有主人的历史设备永远收不到推送；升级前的旧行
// user_id 为空，等设备下次登录注册时才绑定。
type Device struct {
	ID        string `gorm:"primaryKey"`
	UserID    string `gorm:"index"`
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

// User 是控制面的账号记录。
//
// 控制面自己保存账号而不是委托外部系统：部署形态是「一个人、多台机器」，
// 引入 IdP 只会让自托管多一个必填依赖。Email 存储前统一小写并作为唯一键，
// 因为用户输入的大小写不该产生两个账号。
//
// PasswordHash 只保存 argon2id 的 PHC 字符串，明文密码不落库、不进日志。
type User struct {
	ID           string `gorm:"primaryKey"`
	Email        string `gorm:"uniqueIndex;not null"`
	DisplayName  string
	PasswordHash string
	Role         string // admin | user
	Status       string // active | disabled
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// UserSession 是一次浏览器/移动端登录产生的会话。
//
// 只保存令牌的 sha256：数据库被读走也无法直接冒用会话。明文只在登录响应的
// cookie 里出现一次，任何代码路径都不得把它写日志。过期判断一律交给 SQL，
// 避免「先查出来再比时间」时把时区差异和时钟偏差散落到多处。
type UserSession struct {
	ID         string `gorm:"primaryKey"`
	UserID     string `gorm:"index;not null"`
	TokenHash  string `gorm:"uniqueIndex;not null"`
	UserAgent  string
	ExpiresAt  time.Time
	LastSeenAt time.Time
	CreatedAt  time.Time
}

// Invite 是注册邀请码；控制面默认 invite-only。
//
// 只保存 sha256 摘要：明文邀请码由调用方生成并交给用户，数据库被读走也
// 无法反推出可用邀请码。是否可用（未使用、未过期）的判定全部在
// ConsumeInvite 的单条 UPDATE 里完成，避免「先查后写」的并发窗口。
type Invite struct {
	ID        string `gorm:"primaryKey"`
	CodeHash  string `gorm:"uniqueIndex;not null"`
	CreatedBy string
	ExpiresAt time.Time
	UsedBy    string
	UsedAt    *time.Time
	CreatedAt time.Time
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

	if err := migrateControlPlane(database); err != nil {
		// 迁移失败时主动释放底层连接，避免句柄泄漏。
		if sqlDB, dbErr := database.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("migrate nodes table: %w", err)
	}

	return &Store{db: database}, nil
}

// OpenPostgres 打开 cfg.URL 指向的 PostgreSQL 数据库并迁移全部控制面表。
//
// 生产环境走 PostgreSQL：控制面与节点可能分布在多台机器上，SQLite 的单机
// 文件不满足共享存储需求。错误信息里不回显连接串，避免凭据进入日志。
func OpenPostgres(cfg appconfig.DatabaseConfig) (*Store, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, errors.New("database url is required")
	}

	// 默认静默：GORM 的 Info 级日志会打印每条 SQL，生产环境噪声过大；
	// 只有显式 cfg.Debug 时才打开，便于排查连接与迁移问题。
	gormCfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	if cfg.Debug {
		gormCfg.Logger = logger.Default.LogMode(logger.Info)
	}

	database, err := gorm.Open(postgres.Open(cfg.URL), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	sqlDB, err := database.DB()
	if err != nil {
		return nil, fmt.Errorf("get postgres sql database: %w", err)
	}
	// 连接池参数：控制面并发不高，20 条足够吸收突发且不会耗尽测试库连接数。
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := migrateControlPlane(database); err != nil {
		// 迁移失败时主动释放底层连接，避免句柄泄漏。
		_ = sqlDB.Close()
		return nil, fmt.Errorf("migrate postgres tables: %w", err)
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

// userNamespace 是偏好类表的账号隔离前缀。
//
// preferences / project_preferences 的主键就是键名本身，加 user_id 列意味着
// 改 DDL 并同步所有调用点；用键名前缀 u/<user_id>/<key> 表达归属在 SQLite 与
// PostgreSQL 上行为一致，也不需要迁移——旧键留在无前缀的历史命名空间，按
// 设计 §12 不迁移（旧值保留不读）。前缀因此是保留的：历史命名空间里恰好以
// u/ 开头的键不再可读。
const userNamespace = "u/"

// userKey 把裸键映射到账号命名空间；空 userID 原样返回。
//
// 空 userID 是 local / admin_token 这类部署级身份的共享命名空间，语义上等于
// 改造前的全局偏好，不能与任何账号的键混在一起。
func userKey(userID, key string) string {
	if userID == "" {
		return key
	}
	return userPrefix(userID) + key
}

// userPrefix 返回账号前缀。末尾的斜杠让 u1 与 u11 不会互相吞掉对方的键。
func userPrefix(userID string) string {
	return userNamespace + userID + "/"
}

// stripUserKey 判断一条已落库的键是否属于 userID 的命名空间，并剥掉前缀。
//
// SQL 侧的 LIKE 只用来收敛扫描范围，归属判定以这里为准：userID 里若含 SQL
// 通配符（当前是服务端生成的 UUID，不会发生），LIKE 最多多扫几行，不会把
// 别人的行当成自己的。
func stripUserKey(userID, key string) (string, bool) {
	if userID == "" {
		return key, !strings.HasPrefix(key, userNamespace)
	}
	prefix := userPrefix(userID)
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	return strings.TrimPrefix(key, prefix), true
}

// ListProjectPreferencesFor 返回某个账号的全部项目偏好，ProjectKey 已剥掉前缀。
//
// 空 userID 只返回未加前缀的历史行：那是部署级身份的共享命名空间，把账号的
// 偏好混进来会让「本机管理员」看到别人的项目设置。
func (s *Store) ListProjectPreferencesFor(ctx context.Context, userID string) ([]ProjectPreference, error) {
	rows := make([]ProjectPreference, 0)
	query := s.db.WithContext(ctx)
	if userID == "" {
		query = query.Where("project_key NOT LIKE ?", userNamespace+"%")
	} else {
		query = query.Where("project_key LIKE ?", userPrefix(userID)+"%")
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list project preferences: %w", err)
	}

	prefs := make([]ProjectPreference, 0, len(rows))
	for _, row := range rows {
		key, ok := stripUserKey(userID, row.ProjectKey)
		if !ok {
			continue
		}
		row.ProjectKey = key
		prefs = append(prefs, row)
	}
	return prefs, nil
}

// ListProjectPreferences 返回历史命名空间（空 userID）的全部项目偏好。
//
// Deprecated: 用 ListProjectPreferencesFor。等价于传空 userID，供 local /
// admin_token 这类没有账号的部署级身份使用；账号维度的调用点必须传自己的
// userID。
func (s *Store) ListProjectPreferences(ctx context.Context) ([]ProjectPreference, error) {
	return s.ListProjectPreferencesFor(ctx, "")
}

// SetProjectPrimaryNodeFor 设置某账号下项目的主节点。
//
// 首次设置时建行；重复设置覆盖主节点但保留别名，避免改节点顺手丢掉用户起的
// 名字。空 projectKey / nodeID 视为调用方错误。
func (s *Store) SetProjectPrimaryNodeFor(ctx context.Context, userID, projectKey, nodeID string) error {
	if strings.TrimSpace(projectKey) == "" {
		return errors.New("project key is required")
	}
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}
	row := ProjectPreference{ProjectKey: userKey(userID, projectKey), PrimaryNodeID: nodeID}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project_key"}},
		DoUpdates: clause.AssignmentColumns([]string{"primary_node_id", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("set primary node for %q: %w", projectKey, err)
	}
	return nil
}

// SetProjectPrimaryNode 设置历史命名空间（空 userID）下项目的主节点。
//
// Deprecated: 用 SetProjectPrimaryNodeFor。等价于传空 userID，见其说明。
func (s *Store) SetProjectPrimaryNode(ctx context.Context, projectKey, nodeID string) error {
	return s.SetProjectPrimaryNodeFor(ctx, "", projectKey, nodeID)
}

// GetPreferenceFor 读取某账号的一条偏好；不存在时 ok 为 false 且不视为错误，
// 因为「用户还没设置过」是正常状态。
//
// 错误文本只带裸 key，不拼接账号前缀：偏好键可能包含目录等用户信息，越少写进
// 日志越好。
func (s *Store) GetPreferenceFor(ctx context.Context, userID, key string) (string, bool, error) {
	var rows []Preference
	fullKey := userKey(userID, key)
	if err := s.db.WithContext(ctx).Where("key = ?", fullKey).Limit(1).Find(&rows).Error; err != nil {
		return "", false, fmt.Errorf("get preference %q: %w", key, err)
	}
	if len(rows) == 0 {
		return "", false, nil
	}
	return rows[0].Value, true, nil
}

// GetPreference 读取历史命名空间（空 userID）的一条偏好。
//
// Deprecated: 用 GetPreferenceFor。等价于传空 userID，见其说明。
func (s *Store) GetPreference(ctx context.Context, key string) (string, bool, error) {
	return s.GetPreferenceFor(ctx, "", key)
}

// SetPreferenceFor 写入或覆盖某账号的一条偏好。空 key 视为调用方错误。
func (s *Store) SetPreferenceFor(ctx context.Context, userID, key, value string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("preference key is required")
	}
	row := Preference{Key: userKey(userID, key), Value: value}
	if err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return fmt.Errorf("set preference %q: %w", key, err)
	}
	return nil
}

// SetPreference 写入或覆盖历史命名空间（空 userID）的一条偏好。
//
// Deprecated: 用 SetPreferenceFor。等价于传空 userID，见其说明。
func (s *Store) SetPreference(ctx context.Context, key, value string) error {
	return s.SetPreferenceFor(ctx, "", key, value)
}

// ListPreferencesFor 一次性返回某账号的全部偏好，键名已剥掉账号前缀。
//
// 空 userID 只返回未加前缀的历史键：local / admin_token 与账号是两条互不
// 可见的命名空间，任何一侧的同步包都不能混入另一侧的值。
func (s *Store) ListPreferencesFor(ctx context.Context, userID string) (map[string]string, error) {
	rows := make([]Preference, 0)
	query := s.db.WithContext(ctx)
	if userID == "" {
		query = query.Where("key NOT LIKE ?", userNamespace+"%")
	} else {
		query = query.Where("key LIKE ?", userPrefix(userID)+"%")
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list preferences: %w", err)
	}

	out := make(map[string]string, len(rows))
	for _, row := range rows {
		key, ok := stripUserKey(userID, row.Key)
		if !ok {
			continue
		}
		out[key] = row.Value
	}
	return out, nil
}

// ListPreferences 一次性返回历史命名空间（空 userID）的全部偏好。
//
// Deprecated: 用 ListPreferencesFor。等价于传空 userID，见其说明。
func (s *Store) ListPreferences(ctx context.Context) (map[string]string, error) {
	return s.ListPreferencesFor(ctx, "")
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
