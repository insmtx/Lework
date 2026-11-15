package store

import (
	"context"
	"testing"
)

// TestNotifierSkipsDevicesOfNonMembers 固化推送目标的口径：一次事件只推给
// 该节点成员的设备，别人的手机绝不能因为「也注册过推送」就收到通知。
//
// 用例放在 store 包而不是 push 包，是因为被验证的过滤发生在查询里
// （devices JOIN node_members），push 包只能看到已经过滤好的列表。
func TestNotifierSkipsDevicesOfNonMembers(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.AddNodeMember(ctx, NodeMember{NodeID: "n1", UserID: "u1", Role: NodeRoleOwner}); err != nil {
		t.Fatalf("AddNodeMember: %v", err)
	}
	if _, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-1",
	}, "u1"); err != nil {
		t.Fatalf("UpsertDeviceForUser(u1): %v", err)
	}
	if _, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "other", Platform: "ios", PushToken: "tok-2",
	}, "u2"); err != nil {
		t.Fatalf("UpsertDeviceForUser(u2): %v", err)
	}

	got, err := s.ListDevicesForNode(ctx, "n1")
	if err != nil {
		t.Fatalf("ListDevicesForNode: %v", err)
	}
	if len(got) != 1 || got[0].PushToken != "tok-1" {
		t.Fatalf("devices = %+v, want only tok-1", got)
	}
}

func TestUpsertDeviceForUserStoresOwner(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	device, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "android", PushToken: "tok-a",
	}, "u1")
	if err != nil {
		t.Fatalf("UpsertDeviceForUser() failed: %v", err)
	}
	if device.UserID != "u1" {
		t.Fatalf("UserID = %q, want u1", device.UserID)
	}
	if !isDeviceID(device.ID) {
		t.Fatalf("ID = %q, want device_ + 16 hex chars", device.ID)
	}

	owned, err := s.ListDevicesForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListDevicesForUser() failed: %v", err)
	}
	if len(owned) != 1 || owned[0].ID != device.ID || owned[0].UserID != "u1" {
		t.Fatalf("devices = %+v, want the stored device with owner", owned)
	}
}

// TestUpsertDeviceForUserReassignsExistingToken 固化「换账号」语义：同一台
// 手机换了登录账号后重新注册，必须把令牌改绑到新账号，而不是留下两条
// 记录、让旧账号继续收到新账号的通知。
func TestUpsertDeviceForUserReassignsExistingToken(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u1")
	if err != nil {
		t.Fatalf("UpsertDeviceForUser(u1): %v", err)
	}

	second, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u2")
	if err != nil {
		t.Fatalf("UpsertDeviceForUser(u2): %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("ID = %q, want the existing row %q", second.ID, first.ID)
	}
	if second.UserID != "u2" {
		t.Fatalf("UserID = %q, want u2 after reassignment", second.UserID)
	}

	oldOwner, err := s.ListDevicesForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListDevicesForUser(u1): %v", err)
	}
	if len(oldOwner) != 0 {
		t.Fatalf("devices of u1 = %+v, want none after reassignment", oldOwner)
	}
	newOwner, err := s.ListDevicesForUser(ctx, "u2")
	if err != nil {
		t.Fatalf("ListDevicesForUser(u2): %v", err)
	}
	if len(newOwner) != 1 || newOwner[0].PushToken != "tok-a" {
		t.Fatalf("devices of u2 = %+v, want tok-a", newOwner)
	}
}

func TestUpsertDeviceForUserRequiresUser(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, " "); err == nil {
		t.Fatal("UpsertDeviceForUser() without user: want error")
	}
}

// TestListDevicesForNodeSkipsUnownedDevices 保证升级前的历史设备（user_id 为空）
// 不会被当成「所有人的设备」推给任何人。
func TestListDevicesForNodeSkipsUnownedDevices(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if err := s.AddNodeMember(ctx, NodeMember{NodeID: "n1", UserID: "u1"}); err != nil {
		t.Fatalf("AddNodeMember: %v", err)
	}
	// 旧接口不写归属，等价于升级前的历史行。
	if _, err := s.UpsertDevice(ctx, Device{
		Name: "legacy", Platform: "ios", PushToken: "tok-legacy",
	}); err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}

	got, err := s.ListDevicesForNode(ctx, "n1")
	if err != nil {
		t.Fatalf("ListDevicesForNode: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("devices = %+v, want none: unowned devices are unreachable", got)
	}
}

// TestLegacyUpsertDeviceKeepsOwner 固化已弃用入口的保守语义：不传账号的旧
// 注册不会把设备从它所属的账号上摘下来，否则一次旧路径调用就会让推送静默
// 失效。
func TestLegacyUpsertDeviceKeepsOwner(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u1"); err != nil {
		t.Fatalf("UpsertDeviceForUser: %v", err)
	}

	legacy, err := s.UpsertDevice(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	})
	if err != nil {
		t.Fatalf("UpsertDevice: %v", err)
	}
	if legacy.UserID != "u1" {
		t.Fatalf("UserID = %q, want the original owner u1", legacy.UserID)
	}
}

func TestListDevicesForNodeEmptyNodeReturnsNothing(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u1"); err != nil {
		t.Fatalf("UpsertDeviceForUser: %v", err)
	}

	got, err := s.ListDevicesForNode(ctx, "")
	if err != nil {
		t.Fatalf("ListDevicesForNode(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("devices = %+v, want none for an empty node id", got)
	}
}

func TestListDevicesForUserEmptyUserReturnsNothing(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	if _, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u1"); err != nil {
		t.Fatalf("UpsertDeviceForUser: %v", err)
	}

	got, err := s.ListDevicesForUser(ctx, "")
	if err != nil {
		t.Fatalf("ListDevicesForUser(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("devices = %+v, want none for an empty user id", got)
	}
}

func TestDeleteDeviceForUserRefusesForeignDevice(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	device, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u1")
	if err != nil {
		t.Fatalf("UpsertDeviceForUser: %v", err)
	}

	if err := s.DeleteDeviceForUser(ctx, device.ID, "u2"); err == nil {
		t.Fatal("DeleteDeviceForUser() by a foreign user: want error")
	}
	owned, err := s.ListDevicesForUser(ctx, "u1")
	if err != nil {
		t.Fatalf("ListDevicesForUser: %v", err)
	}
	if len(owned) != 1 {
		t.Fatalf("devices = %+v, want the row untouched", owned)
	}
}

func TestDeleteDeviceForUserWithoutUserDeletesAny(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	device, err := s.UpsertDeviceForUser(ctx, Device{
		Name: "phone", Platform: "ios", PushToken: "tok-a",
	}, "u1")
	if err != nil {
		t.Fatalf("UpsertDeviceForUser: %v", err)
	}

	if err := s.DeleteDeviceForUser(ctx, device.ID, ""); err != nil {
		t.Fatalf("DeleteDeviceForUser() as admin: %v", err)
	}
	if all, listErr := s.ListDevices(ctx); listErr != nil {
		t.Fatalf("ListDevices: %v", listErr)
	} else if len(all) != 0 {
		t.Fatalf("devices = %+v, want none after admin delete", all)
	}

	if err := s.DeleteDeviceForUser(ctx, device.ID, ""); err == nil {
		t.Fatal("DeleteDeviceForUser() on a missing row: want error")
	}
}

// isDeviceID 校验设备 ID 的形状，避免把「任意非空串」当成成功。
func isDeviceID(id string) bool {
	const prefix = "device_"
	return len(id) == len(prefix)+16 && id[:len(prefix)] == prefix
}
