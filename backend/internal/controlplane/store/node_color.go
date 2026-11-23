package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// nodeColorKeyPrefix 是节点颜色在偏好表里的键前缀。
//
// 颜色是「这个人怎么看这台节点」的展示偏好，不是节点身份；agent 每次 hello
// 覆盖主机名、节点重新上线都不该影响它，因此和别名一样按账号隔离存放。
const nodeColorKeyPrefix = "node.color."

// SetNodeColorFor 设置某账号对某节点的颜色；空值表示删除该偏好。
func (s *Store) SetNodeColorFor(ctx context.Context, userID, nodeID, color string) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}

	color = strings.TrimSpace(color)
	if color == "" {
		if err := s.db.WithContext(ctx).
			Where("key = ?", userKey(userID, nodeColorKeyPrefix+nodeID)).
			Delete(&Preference{}).Error; err != nil {
			return fmt.Errorf("clear node color for %q: %w", nodeID, err)
		}
		return nil
	}
	return s.SetPreferenceFor(ctx, userID, nodeColorKeyPrefix+nodeID, color)
}

// GetNodeColorFor 读取某账号对某节点的颜色；未设置或已清空时 ok 为 false。
func (s *Store) GetNodeColorFor(ctx context.Context, userID, nodeID string) (string, bool, error) {
	value, ok, err := s.GetPreferenceFor(ctx, userID, nodeColorKeyPrefix+nodeID)
	if err != nil {
		return "", false, err
	}
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", false, nil
	}
	return value, true, nil
}

// ListNodeColorsFor 一次返回某账号设置的全部节点颜色，键是节点 ID。
func (s *Store) ListNodeColorsFor(ctx context.Context, userID string) (map[string]string, error) {
	return s.listNodePrefsByPrefix(ctx, userID, nodeColorKeyPrefix)
}
