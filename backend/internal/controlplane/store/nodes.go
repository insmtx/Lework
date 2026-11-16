package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// 本文件集中实现「一台节点从账号里消失」的存储语义。

// ErrNodeNotOwner 表示调用者不是该节点的 owner。
//
// 与 ErrNodeNotFound 分开：接口层需要 403/404 的区分只发生在「调用者确实是
// 成员但不是 owner」这一种情况，其余一律 404，避免把节点存在性泄漏给非成员。
var ErrNodeNotOwner = errors.New("node not owned by user")

// DeleteNodeForOwner 在一个事务里删除节点及其衍生的全部数据。
//
// 不删 devices：推送按「设备主人是否仍是事件节点的成员」过滤，成员行没了
// 自然收不到；删设备会波及账号在其他节点上的推送。
// project_preferences 只清空 primary_node_id、保留 alias 行：别名是用户给
// 项目起的名字，不该因为节点消失而丢失；也不自动改选另一台，那会伪造一次
// 「人工选择」。
func (s *Store) DeleteNodeForOwner(ctx context.Context, nodeID, userID string) error {
	if strings.TrimSpace(nodeID) == "" || strings.TrimSpace(userID) == "" {
		return errors.New("node id and user id are required")
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var roles []string
		if err := tx.Model(&NodeMember{}).
			Where("node_id = ? AND user_id = ?", nodeID, userID).
			Limit(1).Pluck("role", &roles).Error; err != nil {
			return fmt.Errorf("lookup node role: %w", err)
		}
		if len(roles) == 0 {
			// 非成员与节点不存在对外都是 404。
			return ErrNodeNotFound
		}
		if roles[0] != NodeRoleOwner {
			return ErrNodeNotOwner
		}

		// 成员 ID 要在删行之前取出来：目录偏好按账号命名空间存放，
		// 删完成员行就不知道还有谁需要清理。
		members := make([]NodeMember, 0)
		if err := tx.Where("node_id = ?", nodeID).Find(&members).Error; err != nil {
			return fmt.Errorf("list node members: %w", err)
		}

		if err := tx.Where("node_id = ?", nodeID).Delete(&NodeMember{}).Error; err != nil {
			return fmt.Errorf("delete node members: %w", err)
		}
		if err := tx.Where("node_id = ?", nodeID).Delete(&NodeClaim{}).Error; err != nil {
			return fmt.Errorf("delete node claims: %w", err)
		}
		if err := tx.Where("node_id = ?", nodeID).Delete(&ProjectIndex{}).Error; err != nil {
			return fmt.Errorf("delete project index: %w", err)
		}
		if err := tx.Model(&ProjectPreference{}).
			Where("primary_node_id = ?", nodeID).
			Where("project_key LIKE ?", "u/%").
			UpdateColumn("primary_node_id", "").Error; err != nil {
			return fmt.Errorf("clear project primary node: %w", err)
		}
		for _, m := range members {
			if err := tx.Where("key = ?", userPrefix(m.UserID)+"inbox.dir."+nodeID).
				Delete(&Preference{}).Error; err != nil {
				return fmt.Errorf("delete inbox preference: %w", err)
			}
			if err := tx.Where("key = ?", userPrefix(m.UserID)+nodeAliasKeyPrefix+nodeID).
				Delete(&Preference{}).Error; err != nil {
				return fmt.Errorf("delete node alias: %w", err)
			}
		}
		// 历史命名空间（改造前没有账号前缀）的同名偏好也一并清掉：
		// 节点已经不存在，任何残留键都只会在下次同 ID 出现时变成幽灵值。
		if err := tx.Where("key = ?", "inbox.dir."+nodeID).Delete(&Preference{}).Error; err != nil {
			return fmt.Errorf("delete legacy inbox preference: %w", err)
		}
		if err := tx.Where("key = ?", nodeAliasKeyPrefix+nodeID).Delete(&Preference{}).Error; err != nil {
			return fmt.Errorf("delete legacy node alias: %w", err)
		}

		res := tx.Where("id = ?", nodeID).Delete(&Node{})
		if res.Error != nil {
			return fmt.Errorf("delete node: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrNodeNotFound
		}
		return nil
	})
}
