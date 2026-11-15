package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CountUsers 返回账号总数。
//
// 登录页需要判断「这是不是一台刚部署的控制面」：为 0 时才允许用 enrollment
// token 初始化第一个管理员，否则任何人都能凭令牌再造一个 admin。
func (s *Store) CountUsers(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&User{}).Count(&n).Error; err != nil {
		return 0, fmt.Errorf("count users: %w", err)
	}
	return n, nil
}

// CreateUser 新建账号并返回落库后的记录（含生成的 UUID 与时间戳）。
//
// 邮箱统一小写去空格后才写入并判重：不做这步，Alice@Example.com 与
// alice@example.com 会变成两个账号，登录时谁也说不清哪个才是「她」。
// 判重同时依赖这里的预查询与 email 唯一索引——预查询给出可读错误，
// 唯一索引兜住并发注册的竞态。
//
// 调用方可以预置 ID：注册流程要先消费邀请码、再建账号，而邀请码记录的是
// 「未来用户 ID」，两处必须是同一个值，否则邀请码的 used_by 指向不存在的行。
func (s *Store) CreateUser(ctx context.Context, u User) (User, error) {
	email := normalizeEmail(u.Email)
	if email == "" {
		return User{}, errors.New("user email is required")
	}

	// 用 Find 而非 First：未命中是正常分支，不应产生 record not found 日志。
	var existing []User
	if err := s.db.WithContext(ctx).Where("email = ?", email).Limit(1).Find(&existing).Error; err != nil {
		return User{}, fmt.Errorf("lookup user %q: %w", email, err)
	}
	if len(existing) > 0 {
		// 错误文本里不再带哈希或密码，只有邮箱本身。
		return User{}, fmt.Errorf("create user %q: email already registered", email)
	}

	if strings.TrimSpace(u.ID) == "" {
		u.ID = uuid.NewString()
	}
	u.Email = email
	if err := s.db.WithContext(ctx).Create(&u).Error; err != nil {
		return User{}, fmt.Errorf("create user %q: %w", email, err)
	}
	return u, nil
}

// UserByEmail 按邮箱查账号；未找到时 ok 为 false 且不视为错误，
// 因为「邮箱不存在」在登录流程里与密码错误一样是正常分支。
func (s *Store) UserByEmail(ctx context.Context, email string) (User, bool, error) {
	var rows []User
	if err := s.db.WithContext(ctx).Where("email = ?", normalizeEmail(email)).Limit(1).Find(&rows).Error; err != nil {
		return User{}, false, fmt.Errorf("lookup user by email: %w", err)
	}
	if len(rows) == 0 {
		return User{}, false, nil
	}
	return rows[0], true, nil
}

// UserByID 按 ID 查账号；未找到时 ok 为 false，供中间件把失效会话当成未登录。
func (s *Store) UserByID(ctx context.Context, id string) (User, bool, error) {
	var rows []User
	if err := s.db.WithContext(ctx).Where("id = ?", id).Limit(1).Find(&rows).Error; err != nil {
		return User{}, false, fmt.Errorf("lookup user %q: %w", id, err)
	}
	if len(rows) == 0 {
		return User{}, false, nil
	}
	return rows[0], true, nil
}

// CreateSession 为用户签发一条会话记录，返回可直接使用的行（ID、过期时间等）。
//
// 入参是令牌哈希而非明文：明文只存在于调用方即将写进 cookie 的那个局部变量里。
// ttl 允许为负——那正是「立刻过期」的写法，测试与强制下线都会用到，因此这里
// 不校验符号。
func (s *Store) CreateSession(
	ctx context.Context,
	userID, tokenHash, userAgent string,
	ttl time.Duration,
) (UserSession, error) {
	if strings.TrimSpace(userID) == "" {
		return UserSession{}, errors.New("session user id is required")
	}
	if strings.TrimSpace(tokenHash) == "" {
		return UserSession{}, errors.New("session token hash is required")
	}

	// 统一用 UTC：SQLite 把时间按带偏移的文本存储，若写入值分别落在夏令时
	// 前后，字符串比较会给出错误的先后关系；固定 UTC 后两侧都可比较。
	now := time.Now().UTC()
	session := UserSession{
		ID:         uuid.NewString(),
		UserID:     userID,
		TokenHash:  tokenHash,
		UserAgent:  userAgent,
		ExpiresAt:  now.Add(ttl),
		LastSeenAt: now,
	}
	if err := s.db.WithContext(ctx).Create(&session).Error; err != nil {
		return UserSession{}, fmt.Errorf("create session for user %q: %w", userID, err)
	}
	return session, nil
}

// SessionByTokenHash 按令牌哈希取出仍有效的会话。
//
// 过期条件写在 SQL 里（expires_at > now）而不是取回来再比较：判断只有一处，
// 数据库与 Go 两侧的时钟、时区不会给出互相矛盾的结果。已过期的行视为不存在，
// 调用方据此回 401。
func (s *Store) SessionByTokenHash(ctx context.Context, hash string) (UserSession, bool, error) {
	if strings.TrimSpace(hash) == "" {
		return UserSession{}, false, nil
	}

	var rows []UserSession
	err := s.db.WithContext(ctx).
		Where("token_hash = ? AND expires_at > ?", hash, time.Now().UTC()).
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return UserSession{}, false, fmt.Errorf("lookup session: %w", err)
	}
	if len(rows) == 0 {
		return UserSession{}, false, nil
	}
	return rows[0], true, nil
}

// DeleteSession 删除一条会话。
//
// 行不存在时不报错：登出接口会被重复调用（刷新页面、多标签页同时退出），
// 让「已经登出」变成失败只会制造无意义的 404。
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if strings.TrimSpace(id) == "" {
		return errors.New("session id is required")
	}
	if err := s.db.WithContext(ctx).Delete(&UserSession{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete session %q: %w", id, err)
	}
	return nil
}

// DeleteUserSessions 清空某个用户的全部会话。
//
// 改密、禁用或移除账号时必须调用：只改密码而留着旧会话，等于让泄露的令牌
// 继续有效。空 userID 视为调用方错误，避免误删全表。
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	if strings.TrimSpace(userID) == "" {
		return errors.New("session user id is required")
	}
	if err := s.db.WithContext(ctx).Delete(&UserSession{}, "user_id = ?", userID).Error; err != nil {
		return fmt.Errorf("delete sessions for user %q: %w", userID, err)
	}
	return nil
}

// normalizeEmail 把邮箱收敛成存储与比较的唯一形式（小写、去首尾空格）。
func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
