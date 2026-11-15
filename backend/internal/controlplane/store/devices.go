package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// UpsertDevice 按推送令牌写入设备，但不记录归属。
//
// Deprecated: 设备必须归属账号才可能收到推送，新代码请用 UpsertDeviceForUser。
// 保留它只为兼容「先写设备、后补账号」的旧调用方；对已有设备不会改变主人。
func (s *Store) UpsertDevice(ctx context.Context, d Device) (Device, error) {
	return s.upsertDevice(ctx, d, "")
}

// UpsertDeviceForUser 按推送令牌写入设备并绑定到 userID。
//
// 同一令牌重复注册时复用原行（ID 与创建时间不变），避免重装 App 后留下永远
// 推不到的僵尸记录。令牌已属于另一个账号时改绑到 userID：手机换了登录账号
// 后，通知必须跟着新账号走，否则旧账号会继续收到新账号的审批。
//
// userID 为空一律拒绝：推送按节点成员筛选设备，没有主人的设备既无法判断
// 可见性，也永远匹配不到任何节点成员，写进去只会是一条永远收不到通知的记录。
func (s *Store) UpsertDeviceForUser(ctx context.Context, d Device, userID string) (Device, error) {
	owner := strings.TrimSpace(userID)
	if owner == "" {
		return Device{}, errors.New("device user is required")
	}
	return s.upsertDevice(ctx, d, owner)
}

// upsertDevice 是两条注册入口共用的实现；userID 为空表示旧的无归属写入。
//
// 旧入口不清空已有归属：它的调用方本来就不提供账号，若顺手把 user_id 写成
// 空串，等于让一次旧路径注册悄悄摘掉设备的推送资格。
func (s *Store) upsertDevice(ctx context.Context, d Device, userID string) (Device, error) {
	if strings.TrimSpace(d.PushToken) == "" {
		return Device{}, errors.New("push token is required")
	}
	if strings.TrimSpace(d.Name) == "" {
		return Device{}, errors.New("device name is required")
	}
	if d.Platform != "android" && d.Platform != "ios" {
		return Device{}, fmt.Errorf("unsupported platform %q", d.Platform)
	}

	// 与 UpsertLegacyNode 同理：用 Find 查询，未命中是正常分支，不产生日志噪声。
	var existing []Device
	res := s.db.WithContext(ctx).Where("push_token = ?", d.PushToken).Limit(1).Find(&existing)
	if res.Error != nil {
		return Device{}, fmt.Errorf("lookup device: %w", res.Error)
	}

	// owner 为空表示旧接口的「不提供账号」：已有设备保留原主人，新设备没有
	// 归属。调用方即使往结构体里填了 UserID 也不会被采信——绑定账号只能走
	// UpsertDeviceForUser 这条显式入口。
	owner := userID
	if len(existing) > 0 {
		d.ID = existing[0].ID
		d.CreatedAt = existing[0].CreatedAt
		if owner == "" {
			owner = existing[0].UserID
		}
	} else {
		id, idErr := newDeviceID()
		if idErr != nil {
			return Device{}, idErr
		}
		d.ID = id
	}
	d.UserID = owner

	if err := s.db.WithContext(ctx).Save(&d).Error; err != nil {
		return Device{}, fmt.Errorf("save device: %w", err)
	}
	return d, nil
}

// ListDevices 返回全部设备，按创建时间升序；空库返回空切片而非 nil。
//
// 只给管理员视图用。推送与普通账号的列表都必须走 ListDevicesForNode /
// ListDevicesForUser，否则会把别人的设备暴露出去。
func (s *Store) ListDevices(ctx context.Context) ([]Device, error) {
	devices := make([]Device, 0)
	if err := s.db.WithContext(ctx).Order("created_at ASC").Find(&devices).Error; err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	return devices, nil
}

// ListDevicesForUser 返回某账号名下的设备，按创建时间升序。
//
// 空 userID 返回空列表而不是全表：把「没有用户」当成「所有用户」会让一次
// 缺身份的请求变成全量泄漏，这个失败方向无法接受。
func (s *Store) ListDevicesForUser(ctx context.Context, userID string) ([]Device, error) {
	devices := make([]Device, 0)
	owner := strings.TrimSpace(userID)
	if owner == "" {
		return devices, nil
	}
	if err := s.db.WithContext(ctx).
		Where("user_id = ?", owner).
		Order("created_at ASC, id ASC").
		Find(&devices).Error; err != nil {
		return nil, fmt.Errorf("list devices for user %q: %w", owner, err)
	}
	return devices, nil
}

// ListDevicesForNode 返回在该节点上有成员行的账号名下的设备，按创建时间升序。
//
// 推送目标就是这里的结果：审批与执行结束只与节点有关，通知必须发给「正在用
// 这台机器的人」，而不是所有注册过推送的手机。devices.user_id 为空的历史行
// 不会命中 node_members（成员行的 user_id 永不为空），因此天然被排除。
//
// 显式 Select("devices.*") 是必要的：两张表都有 created_at，SELECT * 在
// JOIN 下会把同名结果列混在一起，设备列表的排序与展示会取到成员行的值。
func (s *Store) ListDevicesForNode(ctx context.Context, nodeID string) ([]Device, error) {
	devices := make([]Device, 0)
	if strings.TrimSpace(nodeID) == "" {
		return devices, nil
	}

	err := s.db.WithContext(ctx).
		Model(&Device{}).
		Select("devices.*").
		Joins("JOIN node_members ON node_members.user_id = devices.user_id").
		Where("node_members.node_id = ?", nodeID).
		Order("devices.created_at ASC, devices.id ASC").
		Find(&devices).Error
	if err != nil {
		return nil, fmt.Errorf("list devices for node %q: %w", nodeID, err)
	}
	return devices, nil
}

// DeleteDevice 删除设备，不校验归属。
//
// Deprecated: 用它删除用户设备会绕过归属校验；新代码请用 DeleteDeviceForUser，
// 管理员清理任意设备时显式传空 userID。
func (s *Store) DeleteDevice(ctx context.Context, id string) error {
	return s.DeleteDeviceForUser(ctx, id, "")
}

// DeleteDeviceForUser 删除 id 且仅当它属于 userID；userID 为空表示不限制归属。
//
// 归属不匹配与记录不存在返回同一个错误，handler 因此只会回 404：区分两者
// 等于告诉调用方「这个 id 存在，只是不是你的」。空 userID 是管理员语义，
// 调用方必须先确认自己确实有管理权限。
func (s *Store) DeleteDeviceForUser(ctx context.Context, id, userID string) error {
	query := s.db.WithContext(ctx).Where("id = ?", id)
	if owner := strings.TrimSpace(userID); owner != "" {
		query = query.Where("user_id = ?", owner)
	}

	res := query.Delete(&Device{})
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
