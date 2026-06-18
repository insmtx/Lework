package db

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/insmtx/Leros/backend/types"
)

// CreateMessage 创建消息
func CreateMessage(ctx context.Context, db *gorm.DB, message *types.SessionMessage) error {
	return db.WithContext(ctx).Create(message).Error
}

// GetMessageByID 根据ID获取消息
func GetMessageByID(ctx context.Context, db *gorm.DB, id uint) (*types.SessionMessage, error) {
	var entity types.SessionMessage
	err := db.WithContext(ctx).First(&entity, id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetSessionMessages 查询会话的所有消息（按 sequence 排序，支持分页）
func GetSessionMessages(ctx context.Context, db *gorm.DB, sessionID uint, page, perPage int) ([]*types.SessionMessage, int64, error) {
	var entities []*types.SessionMessage
	var total int64

	query := db.WithContext(ctx).Model(&types.SessionMessage{}).Where("session_id = ?", sessionID)

	err := query.Count(&total).Error
	if err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * perPage
	err = query.Offset(offset).Limit(perPage).Order("sequence ASC").Find(&entities).Error
	if err != nil {
		return nil, 0, err
	}

	return entities, total, nil
}

// GetSessionMessagesByIDs queries user messages by IDs within one session.
func GetSessionMessagesByIDs(ctx context.Context, db *gorm.DB, sessionID uint, ids []uint) ([]*types.SessionMessage, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var entities []*types.SessionMessage
	err := db.WithContext(ctx).
		Where("session_id = ? AND id IN ?", sessionID, ids).
		Order("sequence ASC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// GetRecentProcessingUserMessages returns recent user messages that are currently being answered.
func GetRecentProcessingUserMessages(ctx context.Context, db *gorm.DB, sessionID uint, since time.Time) ([]*types.SessionMessage, error) {
	var entities []*types.SessionMessage
	err := db.WithContext(ctx).
		Where("session_id = ? AND role = ? AND status = ? AND updated_at >= ?",
			sessionID,
			string(types.MessageRoleUser),
			string(types.MessageStatusProcessing),
			since,
		).
		Order("updated_at DESC").
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	return entities, nil
}

// DeleteMessage 软删除消息
func DeleteMessage(ctx context.Context, db *gorm.DB, id uint) error {
	return db.WithContext(ctx).Delete(&types.SessionMessage{}, id).Error
}

// ClearSessionMessages 清空会话的所有消息（软删除）
func ClearSessionMessages(ctx context.Context, db *gorm.DB, sessionID uint) error {
	return db.WithContext(ctx).Where("session_id = ?", sessionID).Delete(&types.SessionMessage{}).Error
}

// GetLatestMessage 获取会话的最新消息
func GetLatestMessage(ctx context.Context, db *gorm.DB, sessionID uint) (*types.SessionMessage, error) {
	var entity types.SessionMessage
	err := db.WithContext(ctx).Where("session_id = ?", sessionID).Order("sequence DESC").First(&entity).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &entity, nil
}

// GetMessageCount 获取会话的消息数量
func GetMessageCount(ctx context.Context, db *gorm.DB, sessionID uint) (int64, error) {
	var count int64
	err := db.WithContext(ctx).Model(&types.SessionMessage{}).Where("session_id = ?", sessionID).Count(&count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// GetNextSequence 获取会话的下一个消息序号
func GetNextSequence(ctx context.Context, db *gorm.DB, sessionID uint) (int64, error) {
	var maxSequence int64
	err := db.WithContext(ctx).Model(&types.SessionMessage{}).Where("session_id = ?", sessionID).Select("COALESCE(MAX(sequence), 0)").Scan(&maxSequence).Error
	if err != nil {
		return 0, err
	}
	return maxSequence + 1, nil
}

// UpdateMessageSequence 更新消息序号
func UpdateMessageSequence(ctx context.Context, db *gorm.DB, messageID uint, sequence int64) error {
	return db.WithContext(ctx).Model(&types.SessionMessage{}).Where("id = ?", messageID).Update("sequence", sequence).Error
}

// ClaimSessionMessagesByStatus 原子占位匹配状态的会话消息，并更新为目标状态。
func ClaimSessionMessagesByStatus(ctx context.Context, db *gorm.DB, sessionID uint, role string, fromStatus string, toStatus string) ([]*types.SessionMessage, error) {
	var claimed []*types.SessionMessage
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("session_id = ? AND role = ? AND status = ?", sessionID, role, fromStatus).
			Order("created_at ASC").
			Find(&claimed).Error; err != nil {
			return err
		}
		if len(claimed) == 0 {
			return nil
		}
		ids := make([]uint, 0, len(claimed))
		for _, message := range claimed {
			ids = append(ids, message.ID)
			message.Status = toStatus
		}
		return tx.Model(&types.SessionMessage{}).
			Where("id IN ? AND status = ?", ids, fromStatus).
			Update("status", toStatus).Error
	})
	if err != nil {
		return nil, err
	}
	return claimed, nil
}

// UpdateMessagesStatus 批量更新指定消息的状态。
func UpdateMessagesStatus(ctx context.Context, db *gorm.DB, ids []uint, status string) error {
	if len(ids) == 0 {
		return nil
	}
	return db.WithContext(ctx).Model(&types.SessionMessage{}).
		Where("id IN ?", ids).
		Update("status", status).Error
}

// GetRecentSessionMessages 获取会话最近的 N 条消息（按时间顺序）
func GetRecentSessionMessages(ctx context.Context, db *gorm.DB, sessionID uint, limit int) ([]*types.SessionMessage, error) {
	var entities []*types.SessionMessage
	err := db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("sequence DESC").
		Limit(limit).
		Find(&entities).Error
	if err != nil {
		return nil, err
	}
	reverse(entities)
	return entities, nil
}

func reverse(messages []*types.SessionMessage) {
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
}
