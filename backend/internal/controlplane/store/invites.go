package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CreateInvite 保存一条邀请码并返回落库后的记录（含生成的 ID 与创建时间）。
//
// 入参只接受 CodeHash：明文邀请码不落库、不进日志，调用方生成并展示一次后
// 就该丢弃。UsedBy/UsedAt 一律清空，避免调用方误把一条「已用」的旧记录
// 当成新邀请码写入。
func (s *Store) CreateInvite(ctx context.Context, inv Invite) (Invite, error) {
	if strings.TrimSpace(inv.CodeHash) == "" {
		return Invite{}, errors.New("invite code hash is required")
	}

	// 统一用 UTC：SQLite 把时间按带偏移的文本存储，与 ConsumeInvite 的
	// 过期比较必须使用同一时区基准，否则字符串比较会给出错误结果。
	now := time.Now().UTC()
	inv.ID = uuid.NewString()
	inv.UsedBy = ""
	inv.UsedAt = nil
	inv.ExpiresAt = inv.ExpiresAt.UTC()
	inv.CreatedAt = now
	if err := s.db.WithContext(ctx).Create(&inv).Error; err != nil {
		return Invite{}, fmt.Errorf("create invite: %w", err)
	}
	return inv, nil
}

// ConsumeInvite 原子地把一条未使用且未过期的邀请码标记给 userID。
//
// 判断与写入合并成单条 UPDATE：先 SELECT 再 UPDATE 会让同一邀请码在并发
// 注册时被两个人同时消费。RowsAffected 为 1 才代表本次调用抢到了邀请码；
// 0 表示不存在、已使用或已过期，三者对外都应表现为「邀请码无效」。
func (s *Store) ConsumeInvite(ctx context.Context, codeHash, userID string) (bool, error) {
	if strings.TrimSpace(codeHash) == "" {
		return false, nil
	}
	if strings.TrimSpace(userID) == "" {
		return false, errors.New("invite user id is required")
	}

	now := time.Now().UTC()
	res := s.db.WithContext(ctx).
		Model(&Invite{}).
		Where("code_hash = ? AND used_by = ? AND expires_at > ?", codeHash, "", now).
		UpdateColumns(Invite{UsedBy: userID, UsedAt: &now})
	if res.Error != nil {
		return false, fmt.Errorf("consume invite: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}
