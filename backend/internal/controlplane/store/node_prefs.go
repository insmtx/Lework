package store

import (
	"context"
	"fmt"
	"strings"
)

// listNodePrefsByPrefix 返回某账号下键为 prefix+节点ID 的全部偏好值，键是节点 ID。
//
// 别名和颜色都按「一个节点一条偏好」存放；列表接口每次都要投影整页节点，
// 逐个查询会打出 N 次数据库往返，因此共用一次前缀扫描。
func (s *Store) listNodePrefsByPrefix(ctx context.Context, userID, prefix string) (map[string]string, error) {
	rows := make([]Preference, 0)
	if err := s.db.WithContext(ctx).Where("key LIKE ?", userKey(userID, prefix)+"%").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list node preferences %q: %w", prefix, err)
	}

	out := make(map[string]string, len(rows))
	for _, row := range rows {
		key, ok := stripUserKey(userID, row.Key)
		if !ok || !strings.HasPrefix(key, prefix) {
			continue
		}
		nodeID := strings.TrimPrefix(key, prefix)
		value := strings.TrimSpace(row.Value)
		if nodeID == "" || value == "" {
			continue
		}
		out[nodeID] = value
	}
	return out, nil
}
