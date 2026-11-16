package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// nodeAliasKeyPrefix 是节点展示名在偏好表里的键前缀。
//
// 别名属于「用户怎么看这台节点」，不是节点身份，因此和项目别名一样按账号
// 隔离存放；agent 每次 hello 覆盖 nodes.name 不会影响它。
const nodeAliasKeyPrefix = "node.alias."

// SetNodeAliasFor 设置某账号对某节点的展示名；空白别名表示删除该偏好，
// 让界面回退到 agent 上报的主机名。
func (s *Store) SetNodeAliasFor(ctx context.Context, userID, nodeID, alias string) error {
	if strings.TrimSpace(nodeID) == "" {
		return errors.New("node id is required")
	}

	alias = strings.TrimSpace(alias)
	if alias == "" {
		if err := s.db.WithContext(ctx).
			Where("key = ?", userKey(userID, nodeAliasKeyPrefix+nodeID)).
			Delete(&Preference{}).Error; err != nil {
			return fmt.Errorf("clear node alias for %q: %w", nodeID, err)
		}
		return nil
	}
	return s.SetPreferenceFor(ctx, userID, nodeAliasKeyPrefix+nodeID, alias)
}

// GetNodeAliasFor 读取某账号对某节点的展示名；未设置或已清空时 ok 为 false。
func (s *Store) GetNodeAliasFor(ctx context.Context, userID, nodeID string) (string, bool, error) {
	value, ok, err := s.GetPreferenceFor(ctx, userID, nodeAliasKeyPrefix+nodeID)
	if err != nil {
		return "", false, err
	}
	value = strings.TrimSpace(value)
	if !ok || value == "" {
		return "", false, nil
	}
	return value, true, nil
}

// ListNodeAliasesFor 一次返回某账号设置的全部节点展示名，键是节点 ID。
//
// 列表与总览每次都要投影整页节点，逐个 GetPreferenceFor 会打出 N 次查询；
// 这里只扫描该账号命名空间下的别名键。
func (s *Store) ListNodeAliasesFor(ctx context.Context, userID string) (map[string]string, error) {
	rows := make([]Preference, 0)
	prefix := userKey(userID, nodeAliasKeyPrefix)
	if err := s.db.WithContext(ctx).Where("key LIKE ?", prefix+"%").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list node aliases: %w", err)
	}

	out := make(map[string]string, len(rows))
	for _, row := range rows {
		key, ok := stripUserKey(userID, row.Key)
		if !ok || !strings.HasPrefix(key, nodeAliasKeyPrefix) {
			continue
		}
		nodeID := strings.TrimPrefix(key, nodeAliasKeyPrefix)
		value := strings.TrimSpace(row.Value)
		if nodeID == "" || value == "" {
			continue
		}
		out[nodeID] = value
	}
	return out, nil
}
