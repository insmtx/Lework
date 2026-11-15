package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

// newPendingClaim 造一条未过期的待定认领。pair key 与 code 由用例指定，
// 因为这两个键本身就是被测对象；其余元数据固定，方便断言「节点从认领记录
// 继承了机器信息」。
func newPendingClaim(t *testing.T, s *Store, pairKey, code string) NodeClaim {
	t.Helper()

	c, err := s.CreateClaim(context.Background(), NodeClaim{
		PairKey:      pairKey,
		Code:         code,
		NodeName:     "work-mac",
		OS:           "linux",
		Arch:         "amd64",
		AgentVersion: "v1.2.3",
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim(%q): %v", pairKey, err)
	}
	return c
}

// mustClaimByID 读回一条认领记录并断言存在；测试里丢弃错误会让失败原因
// 变成「零值断言不成立」，掩盖真正的故障。
func mustClaimByID(t *testing.T, s *Store, id string) NodeClaim {
	t.Helper()

	claim, found, err := claimByID(s.db.WithContext(context.Background()), id)
	if err != nil {
		t.Fatalf("claimByID(%q): %v", id, err)
	}
	if !found {
		t.Fatalf("claim %q disappeared", id)
	}
	return claim
}

func TestClaimClaimForUserIsAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk",
		Code:      "AB234",
		Status:    ClaimStatusPending,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	got, ok1, err := s.ClaimClaimForUser(ctx, c.ID, "u1")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	loser, ok2, err := s.ClaimClaimForUser(ctx, c.ID, "u2")
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if !ok1 || ok2 {
		t.Fatalf("ok1 = %v, ok2 = %v", ok1, ok2)
	}
	if got.Status != ClaimStatusClaimed || got.ClaimedBy != "u1" {
		t.Fatalf("claimed row = %+v", got)
	}
	if loser.ID != "" {
		t.Fatalf("losing claim returned a row: %+v", loser)
	}
}

func TestBumpClaimAttemptsExpiresAfterMax(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk2",
		Code:      "CD345",
		Status:    ClaimStatusPending,
		ExpiresAt: time.Now().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}

	for i := 0; i < 3; i++ {
		ok, err := s.BumpClaimAttempts(ctx, c.ID, 3)
		if err != nil {
			t.Fatalf("bump #%d: %v", i, err)
		}
		if !ok {
			t.Fatalf("attempt %d rejected early", i)
		}
	}
	ok, err := s.BumpClaimAttempts(ctx, c.ID, 3)
	if err != nil {
		t.Fatalf("over-limit bump: %v", err)
	}
	if ok {
		t.Fatal("claim still usable after max attempts")
	}

	got, ok, err := s.ClaimByPairKey(ctx, "pk2")
	if err != nil || !ok {
		t.Fatalf("ClaimByPairKey = %v, %v", ok, err)
	}
	if got.Status != ClaimStatusExpired {
		t.Fatalf("status = %q, want %q", got.Status, ClaimStatusExpired)
	}
	if ok, err := s.BumpClaimAttempts(ctx, c.ID, 3); ok || err != nil {
		t.Fatalf("bump on expired claim = %v, %v; want false, nil", ok, err)
	}
}

func TestCreateClaimDefaultsAndNormalizes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "  pk-normalize  ",
		Code:      "  ab234 ",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim: %v", err)
	}
	if c.ID == "" {
		t.Fatal("CreateClaim did not generate an id")
	}
	if c.Status != ClaimStatusPending {
		t.Fatalf("status = %q, want %q", c.Status, ClaimStatusPending)
	}
	if c.Code != "AB234" {
		t.Fatalf("code = %q, want the normalized %q", c.Code, "AB234")
	}
	if c.PairKey != "pk-normalize" {
		t.Fatalf("pair key = %q, want trimmed input", c.PairKey)
	}
	if c.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be set")
	}

	for _, bad := range []NodeClaim{
		{Code: "AB234", ExpiresAt: time.Now().Add(time.Minute)},
		{PairKey: "pk", ExpiresAt: time.Now().Add(time.Minute)},
		{PairKey: "pk", Code: "AB234"},
	} {
		if _, err := s.CreateClaim(ctx, bad); err == nil {
			t.Fatalf("CreateClaim accepted invalid input %+v", bad)
		}
	}
}

// TestCreateClaimRejectsUnknownStatus 固化状态闭集：状态是网关三态分派的唯一
// 依据，落一个拼错的值进去会让认领既不 pending 也不 claimed，永远卡住。
func TestCreateClaimRejectsUnknownStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-bogus-status",
		Code:      "AB234",
		Status:    "bogus",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err == nil {
		t.Fatal("CreateClaim accepted an unknown status")
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&NodeClaim{}).Count(&count).Error; err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if count != 0 {
		t.Fatalf("claims row count = %d, want 0", count)
	}
}

// TestCreateClaimReusesPairKeyRow 固化「重连拿到同一个码」：pair_key 是 agent
// 生成的稳定标识，第二次建认领必须回读同一行（ON CONFLICT DO NOTHING 分支），
// 而不是新建一条让终端上的码失效。
func TestCreateClaimReusesPairKeyRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := newPendingClaim(t, s, "pk-reuse", "ab234")
	second, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-reuse",
		Code:      "ZZ999",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second CreateClaim: %v", err)
	}
	if second.ID != first.ID || second.Code != first.Code {
		t.Fatalf("second claim = {%q, %q}, want the existing {%q, %q}",
			second.ID, second.Code, first.ID, first.Code)
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&NodeClaim{}).Count(&count).Error; err != nil {
		t.Fatalf("count claims: %v", err)
	}
	if count != 1 {
		t.Fatalf("claims row count = %d, want 1", count)
	}

	// 已经认领的行同样原样返回：gateway 需要它来回放凭证。
	if _, _, err := s.ClaimClaimForUser(ctx, first.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	again, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-reuse",
		Code:      "ab234",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim on claimed pair key: %v", err)
	}
	if again.ID != first.ID || again.Status != ClaimStatusClaimed {
		t.Fatalf("claimed reuse = {%q, %q}, want {%q, %q}",
			again.ID, again.Status, first.ID, ClaimStatusClaimed)
	}
}

// TestCreateClaimRejectsCodeInUse 覆盖应用层的码唯一性：码列只有普通索引，
// 并发下可能重复，但「一张码同时对应两个待定认领」会让参与者认错机器。
func TestCreateClaimRejectsCodeInUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	newPendingClaim(t, s, "pk-a", "AB2CD")

	_, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-b",
		Code:      "ab2cd",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if !errors.Is(err, ErrClaimCodeTaken) {
		t.Fatalf("duplicate code error = %v, want ErrClaimCodeTaken", err)
	}
	if strings.Contains(err.Error(), "ab2cd") || strings.Contains(err.Error(), "AB2CD") {
		t.Fatalf("error leaks the claim code: %v", err)
	}

	// 过期认领上的码可以重新使用：它已经不可能被人认领。
	if _, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-old",
		Code:      "ZZZZZ",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}
	if _, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-new",
		Code:      "zzzzz",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatalf("code from an expired claim rejected: %v", err)
	}
}

func TestClaimByCodeOnlyPendingAndUnexpired(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	live := newPendingClaim(t, s, "pk-live", "AB234")
	got, ok, err := s.ClaimByCode(ctx, " ab234 ", now)
	if err != nil || !ok {
		t.Fatalf("ClaimByCode(live) = %v, %v", ok, err)
	}
	if got.ID != live.ID {
		t.Fatalf("ClaimByCode returned %q, want %q", got.ID, live.ID)
	}

	expired, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-expired",
		Code:      "CD345",
		ExpiresAt: now.Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}
	if _, ok, err := s.ClaimByCode(ctx, expired.Code, now); err != nil || ok {
		t.Fatalf("ClaimByCode(expired) = %v, %v; want a miss", ok, err)
	}

	claimed := newPendingClaim(t, s, "pk-claimed", "EF456")
	if _, _, err := s.ClaimClaimForUser(ctx, claimed.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	if _, ok, err := s.ClaimByCode(ctx, claimed.Code, now); err != nil || ok {
		t.Fatalf("ClaimByCode(claimed) = %v, %v; want a miss", ok, err)
	}

	if _, ok, err := s.ClaimByCode(ctx, "NOPE1", now); err != nil || ok {
		t.Fatalf("ClaimByCode(unknown) = %v, %v; want a miss", ok, err)
	}
	if _, ok, err := s.ClaimByCode(ctx, "  ", now); err != nil || ok {
		t.Fatalf("ClaimByCode(blank) = %v, %v; want a miss", ok, err)
	}
}

// TestClaimByPairKeyIgnoresStatus 固化 gateway 的幂等重投路径：拿到已认领的行
// 才能决定「重发凭证」。这里不能按状态过滤，否则重连永远看不到 claimed 行。
func TestClaimByPairKeyIgnoresStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	claimed := newPendingClaim(t, s, "pk-claimed", "AB234")
	if _, _, err := s.ClaimClaimForUser(ctx, claimed.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	got, ok, err := s.ClaimByPairKey(ctx, "pk-claimed")
	if err != nil || !ok {
		t.Fatalf("ClaimByPairKey(claimed) = %v, %v", ok, err)
	}
	if got.Status != ClaimStatusClaimed {
		t.Fatalf("status = %q, want %q", got.Status, ClaimStatusClaimed)
	}

	if _, ok, err := s.ClaimByPairKey(ctx, "pk-missing"); err != nil || ok {
		t.Fatalf("ClaimByPairKey(unknown) = %v, %v; want a miss", ok, err)
	}
	if _, ok, err := s.ClaimByPairKey(ctx, "  "); err != nil || ok {
		t.Fatalf("ClaimByPairKey(blank) = %v, %v; want a miss", ok, err)
	}
}

// TestClaimByPairKeyReportsTTLExpiry 固化三态分派的输入：TTL 到期的 pending 行
// 必须在返回值里表现为 expired。否则 gateway 会对着一条早该作废的认领永远重发
// 同一个码——库里没人把它改成 expired，唯一写 expired 的是尝试次数超限。
func TestClaimByPairKeyReportsTTLExpiry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-ttl",
		Code:      "AB234",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}

	got, ok, err := s.ClaimByPairKey(ctx, "pk-ttl")
	if err != nil || !ok {
		t.Fatalf("ClaimByPairKey = %v, %v", ok, err)
	}
	if got.Status != ClaimStatusExpired {
		t.Fatalf("status = %q, want %q", got.Status, ClaimStatusExpired)
	}
	if got.ID != c.ID || got.Code != c.Code {
		t.Fatalf("TTL normalization changed the row: %+v", got)
	}

	// 只是「调用方视角的过期」，不落库改写：状态机仍然看到 pending，
	// 清理任务与调试查询不会被内存里的展示值污染。
	stored := mustClaimByID(t, s, c.ID)
	if stored.Status != ClaimStatusPending {
		t.Fatalf("stored status = %q, want %q", stored.Status, ClaimStatusPending)
	}

	if _, ok, err := s.ClaimByCode(ctx, c.Code, time.Now().UTC()); err != nil || ok {
		t.Fatalf("ClaimByCode(expired) = %v, %v; want a miss", ok, err)
	}
}

// TestClaimByPairKeyKeepsClaimedAfterTTL 认领成功后实例在发凭证前挂掉，
// agent 可能几分钟后才重连：claimed 行即便过了 TTL 也必须保持 claimed，
// 否则幂等重投会被 TTL 掐断，节点永远拿不到凭证。
func TestClaimByPairKeyKeepsClaimedAfterTTL(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := newPendingClaim(t, s, "pk-claimed-ttl", "CD345")
	if _, _, err := s.ClaimClaimForUser(ctx, c.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	if err := s.db.WithContext(ctx).
		Model(&NodeClaim{}).
		Where("id = ?", c.ID).
		UpdateColumn("expires_at", time.Now().UTC().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("age the claim: %v", err)
	}

	got, ok, err := s.ClaimByPairKey(ctx, "pk-claimed-ttl")
	if err != nil || !ok {
		t.Fatalf("ClaimByPairKey = %v, %v", ok, err)
	}
	if got.Status != ClaimStatusClaimed || got.ClaimedBy != "u1" {
		t.Fatalf("status = %q, claimed_by = %q; want claimed/u1", got.Status, got.ClaimedBy)
	}
}

func TestBindClaimToUserCreatesNodeAndOwner(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := newPendingClaim(t, s, "pk-bind", "AB234")
	node, err := s.BindClaimToUser(ctx, c.ID, "u1", "")
	if err != nil {
		t.Fatalf("BindClaimToUser: %v", err)
	}
	if node.ID == "" {
		t.Fatal("created node has no id")
	}
	if node.Name != "work-mac" {
		t.Fatalf("name = %q, want the claim's node name", node.Name)
	}
	if node.OS != "linux" || node.Arch != "amd64" || node.AgentVersion != "v1.2.3" {
		t.Fatalf("node metadata = %+v, want the claim's", node)
	}
	if node.ClaimedAt.IsZero() {
		t.Fatal("claimed_at not recorded")
	}

	role, ok, err := s.NodeRole(ctx, node.ID, "u1")
	if err != nil {
		t.Fatalf("NodeRole: %v", err)
	}
	if !ok || role != NodeRoleOwner {
		t.Fatalf("role = %q, %v; want owner", role, ok)
	}

	got := mustClaimByID(t, s, c.ID)
	if got.Status != ClaimStatusClaimed || got.ClaimedBy != "u1" || got.NodeID != node.ID {
		t.Fatalf("claim after bind = %+v", got)
	}

	if _, err := s.BindClaimToUser(ctx, c.ID, "u2", ""); !errors.Is(err, ErrClaimTaken) {
		t.Fatalf("second bind error = %v, want ErrClaimTaken", err)
	}
	if _, err := s.BindClaimToUser(ctx, "claim_missing", "u1", ""); !errors.Is(err, ErrClaimNotFound) {
		t.Fatalf("unknown claim error = %v, want ErrClaimNotFound", err)
	}

	expired, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-expired-bind",
		Code:      "CD345",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}
	if _, err := s.BindClaimToUser(ctx, expired.ID, "u1", ""); !errors.Is(err, ErrClaimTaken) {
		t.Fatalf("expired bind error = %v, want ErrClaimTaken", err)
	}
}

// TestBindClaimToUserSuffixesDuplicateNames 固化设计 §6 的「同名自动加后缀」：
// 重装系统会产生同名新节点，拒绝认领会把用户卡在配对流程里。
func TestBindClaimToUserSuffixesDuplicateNames(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := newPendingClaim(t, s, "pk-one", "AB234")
	one, err := s.BindClaimToUser(ctx, first.ID, "u1", "")
	if err != nil {
		t.Fatalf("first bind: %v", err)
	}
	if one.Name != "work-mac" {
		t.Fatalf("first name = %q", one.Name)
	}

	second := newPendingClaim(t, s, "pk-two", "CD345")
	two, err := s.BindClaimToUser(ctx, second.ID, "u1", "work-mac")
	if err != nil {
		t.Fatalf("second bind: %v", err)
	}
	if two.Name != "work-mac-2" {
		t.Fatalf("second name = %q, want work-mac-2", two.Name)
	}

	// 名字只按 owner 去重：别人名下的同名节点不占用后缀。
	third := newPendingClaim(t, s, "pk-three", "EF456")
	other, err := s.BindClaimToUser(ctx, third.ID, "u2", "work-mac")
	if err != nil {
		t.Fatalf("third bind: %v", err)
	}
	if other.Name != "work-mac" {
		t.Fatalf("other owner's name = %q, want work-mac", other.Name)
	}

	// 显式名字覆盖认领记录里的机器名。
	fourth := newPendingClaim(t, s, "pk-four", "GH789")
	named, err := s.BindClaimToUser(ctx, fourth.ID, "u1", "office-pc")
	if err != nil {
		t.Fatalf("fourth bind: %v", err)
	}
	if named.Name != "office-pc" {
		t.Fatalf("explicit name = %q, want office-pc", named.Name)
	}
}

// TestBindClaimToUserRollsBackWhenNamesExhausted 覆盖后缀上限：20 个候选名全
// 占用时必须整体回滚——认领回到 pending，且不留下没有归属行的半成品节点。
func TestBindClaimToUserRollsBackWhenNamesExhausted(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	seatClaimedNames(t, s, "u1", "work-mac", maxNodeNameCandidates)

	c := newPendingClaim(t, s, "pk-exhausted", "AB234")
	_, err := s.BindClaimToUser(ctx, c.ID, "u1", "")
	if err == nil {
		t.Fatal("bind succeeded although every candidate name is taken")
	}
	if !strings.Contains(err.Error(), "no free node name") {
		t.Fatalf("error = %v, want the name-exhausted message", err)
	}

	got := mustClaimByID(t, s, c.ID)
	if got.Status != ClaimStatusPending || got.ClaimedBy != "" || got.NodeID != "" {
		t.Fatalf("claim changed after a failed bind: %+v", got)
	}

	var nodes, members int64
	if err := s.db.WithContext(ctx).Model(&Node{}).Count(&nodes).Error; err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if err := s.db.WithContext(ctx).Model(&NodeMember{}).Count(&members).Error; err != nil {
		t.Fatalf("count members: %v", err)
	}
	if nodes != maxNodeNameCandidates || members != maxNodeNameCandidates {
		t.Fatalf("nodes/members = %d/%d, want %d/%d (no partial rows)",
			nodes, members, maxNodeNameCandidates, maxNodeNameCandidates)
	}
}

// TestBindClaimToUserLeavesNoRowsOnRefusal 未过期/已认领之外的失败同样整体回滚：
// 状态迁移先执行，失败后不能留下「认领已 claimed 但没有节点」的中间态。
func TestBindClaimToUserLeavesNoRowsOnRefusal(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	expired, err := s.CreateClaim(ctx, NodeClaim{
		PairKey:   "pk-refused",
		Code:      "AB234",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}
	if _, err := s.BindClaimToUser(ctx, expired.ID, "u1", ""); !errors.Is(err, ErrClaimTaken) {
		t.Fatalf("expired bind error = %v, want ErrClaimTaken", err)
	}

	var nodes, members int64
	if err := s.db.WithContext(ctx).Model(&Node{}).Count(&nodes).Error; err != nil {
		t.Fatalf("count nodes: %v", err)
	}
	if err := s.db.WithContext(ctx).Model(&NodeMember{}).Count(&members).Error; err != nil {
		t.Fatalf("count members: %v", err)
	}
	if nodes != 0 || members != 0 {
		t.Fatalf("nodes/members = %d/%d, want 0/0", nodes, members)
	}

	got := mustClaimByID(t, s, expired.ID)
	if got.Status != ClaimStatusPending {
		t.Fatalf("stored status = %q, want %q", got.Status, ClaimStatusPending)
	}
}

func TestBindClaimToUserValidatesInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.BindClaimToUser(ctx, " ", "u1", ""); err == nil {
		t.Fatal("blank claim id accepted")
	}
	if _, err := s.BindClaimToUser(ctx, "claim_x", " ", ""); err == nil {
		t.Fatal("blank user id accepted")
	}
}

func TestMarkClaimDeliveredIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := newPendingClaim(t, s, "pk-deliver", "AB234")
	if _, _, err := s.ClaimClaimForUser(ctx, c.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := s.MarkClaimDelivered(ctx, c.ID, "node_x"); err != nil {
			t.Fatalf("MarkClaimDelivered #%d: %v", i, err)
		}
	}
	got := mustClaimByID(t, s, c.ID)
	if got.NodeID != "node_x" {
		t.Fatalf("node_id = %q, want node_x", got.NodeID)
	}

	pending := newPendingClaim(t, s, "pk-pending", "CD345")
	if err := s.MarkClaimDelivered(ctx, pending.ID, "node_y"); !errors.Is(err, ErrClaimNotClaimed) {
		t.Fatalf("pending deliver error = %v, want ErrClaimNotClaimed", err)
	}
	if err := s.MarkClaimDelivered(ctx, "claim_missing", "node_y"); !errors.Is(err, ErrClaimNotFound) {
		t.Fatalf("unknown deliver error = %v, want ErrClaimNotFound", err)
	}
	if err := s.MarkClaimDelivered(ctx, " ", "node_y"); err == nil {
		t.Fatal("blank claim id accepted")
	}
	if err := s.MarkClaimDelivered(ctx, c.ID, " "); err == nil {
		t.Fatal("blank node id accepted")
	}
}

// TestMarkClaimDeliveredRefusesRebinding 固化「幂等重投只认已绑定的那个节点」：
// 两个实例并发补发时，后到的实例可能刚建了自己的节点，静默改绑会让先落库的
// 节点变成没人认领的孤儿。
func TestMarkClaimDeliveredRefusesRebinding(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := newPendingClaim(t, s, "pk-rebind", "AB234")
	if _, _, err := s.ClaimClaimForUser(ctx, c.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	if err := s.MarkClaimDelivered(ctx, c.ID, "node_first"); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if err := s.MarkClaimDelivered(ctx, c.ID, "node_first"); err != nil {
		t.Fatalf("repeated delivery: %v", err)
	}
	if err := s.MarkClaimDelivered(ctx, c.ID, "node_other"); !errors.Is(err, ErrClaimTaken) {
		t.Fatalf("rebinding error = %v, want ErrClaimTaken", err)
	}

	got := mustClaimByID(t, s, c.ID)
	if got.NodeID != "node_first" {
		t.Fatalf("node_id = %q, want node_first", got.NodeID)
	}
}

// TestUpdateClaimMachineOnlyPending 固化待定连接写回机器信息的边界：只有
// pending 行可以更新展示元数据，已认领/已过期的行不能被晚到的连接改写。
func TestUpdateClaimMachineOnlyPending(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := newPendingClaim(t, s, "pk-machine", "AB234")
	meta := NodeClaim{
		NodeName:     "renamed-box",
		OS:           "darwin",
		Arch:         "arm64",
		AgentVersion: "v9.9.9",
	}
	if err := s.UpdateClaimMachine(ctx, c.ID, meta); err != nil {
		t.Fatalf("UpdateClaimMachine: %v", err)
	}

	got := mustClaimByID(t, s, c.ID)
	if got.NodeName != "renamed-box" || got.OS != "darwin" || got.Arch != "arm64" {
		t.Fatalf("machine info = %+v, want the update", got)
	}
	if got.AgentVersion != "v9.9.9" {
		t.Fatalf("agent version = %q, want v9.9.9", got.AgentVersion)
	}
	// 身份相关列与码都不能被这次更新碰到。
	if got.Status != ClaimStatusPending || got.PairKey != "pk-machine" || got.Code != "AB234" {
		t.Fatalf("claim identity columns changed: %+v", got)
	}

	claimed := newPendingClaim(t, s, "pk-machine-claimed", "CD345")
	if _, _, err := s.ClaimClaimForUser(ctx, claimed.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	if err := s.UpdateClaimMachine(ctx, claimed.ID, meta); !errors.Is(err, ErrClaimTaken) {
		t.Fatalf("update on claimed claim = %v, want ErrClaimTaken", err)
	}
	if got := mustClaimByID(t, s, claimed.ID); got.NodeName != "work-mac" {
		t.Fatalf("claimed claim machine info changed: %+v", got)
	}

	if err := s.UpdateClaimMachine(ctx, "claim_missing", meta); !errors.Is(err, ErrClaimNotFound) {
		t.Fatalf("unknown claim error = %v, want ErrClaimNotFound", err)
	}
	if err := s.UpdateClaimMachine(ctx, " ", meta); err == nil {
		t.Fatal("blank claim id accepted")
	}
}

func TestBumpClaimAttemptsValidatesInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.BumpClaimAttempts(ctx, "claim_missing", 3); !errors.Is(err, ErrClaimNotFound) {
		t.Fatalf("unknown claim error = %v, want ErrClaimNotFound", err)
	}
	if _, err := s.BumpClaimAttempts(ctx, "claim_missing", 0); err == nil {
		t.Fatal("zero attempt limit accepted")
	}
	if _, err := s.BumpClaimAttempts(ctx, "claim_missing", -1); err == nil {
		t.Fatal("negative attempt limit accepted")
	}
	if _, err := s.BumpClaimAttempts(ctx, " ", 3); err == nil {
		t.Fatal("blank claim id accepted")
	}

	c := newPendingClaim(t, s, "pk-claimed-bump", "AB234")
	if _, _, err := s.ClaimClaimForUser(ctx, c.ID, "u1"); err != nil {
		t.Fatalf("ClaimClaimForUser: %v", err)
	}
	if ok, err := s.BumpClaimAttempts(ctx, c.ID, 3); ok || err != nil {
		t.Fatalf("bump on claimed claim = %v, %v; want false, nil", ok, err)
	}
	stored := mustClaimByID(t, s, c.ID)
	if stored.Attempts != 0 || stored.Status != ClaimStatusClaimed {
		t.Fatalf("claimed claim changed: %+v", stored)
	}
}

// TestClaimSentinelsSurviveWrapping 固化接口层依赖的 errors.Is 契约：handler
// 要把这些错误映射成 404 / 409 / 410，而不是解析错误文本。同时确认包装后的
// 错误不带短码——码是短暂有效的授权凭据，不该进日志或响应体。
func TestClaimSentinelsSurviveWrapping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	c := newPendingClaim(t, s, "pk-sentinel", "AB234")
	if _, err := s.BindClaimToUser(ctx, c.ID, "u1", ""); err != nil {
		t.Fatalf("BindClaimToUser: %v", err)
	}

	_, err := s.BindClaimToUser(ctx, c.ID, "u2", "")
	if !errors.Is(err, ErrClaimTaken) {
		t.Fatalf("error = %v, want ErrClaimTaken", err)
	}
	if errors.Is(err, ErrClaimNotFound) {
		t.Fatalf("error = %v must not match ErrClaimNotFound", err)
	}
	if strings.Contains(err.Error(), c.Code) {
		t.Fatalf("error leaks the claim code: %v", err)
	}
	if strings.Count(err.Error(), c.ID) != 1 {
		t.Fatalf("claim id wrapped more than once: %v", err)
	}

	pending := newPendingClaim(t, s, "pk-sentinel-pending", "CD345")
	if err := s.MarkClaimDelivered(ctx, pending.ID, "node_x"); !errors.Is(err, ErrClaimNotClaimed) {
		t.Fatalf("deliver-on-pending error = %v, want ErrClaimNotClaimed", err)
	}
}

func TestMigrateControlPlaneCreatesClaimTable(t *testing.T) {
	s := newTestStore(t)

	if !s.db.Migrator().HasTable(&NodeClaim{}) {
		t.Fatal("expected node_claims table to be migrated")
	}
	for _, idx := range []string{"idx_node_claims_pair_key", "idx_node_claims_code"} {
		if !s.db.Migrator().HasIndex(&NodeClaim{}, idx) {
			t.Fatalf("expected index %q on node_claims", idx)
		}
	}
}

// seatClaimedNames 在 userID 名下预置 count 个节点，名字依次为 base、base-2 …
// 直接写库而不是走 20 次认领：这里要构造的是「后缀耗尽」的前置状态，
// 认领流程本身已经由别的用例覆盖。
func seatClaimedNames(t *testing.T, s *Store, userID, base string, count int) {
	t.Helper()

	ctx := context.Background()
	now := time.Now().UTC()
	for i := 0; i < count; i++ {
		name := base
		if i > 0 {
			name = fmt.Sprintf("%s-%d", base, i+1)
		}
		id, err := newNodeID()
		if err != nil {
			t.Fatalf("newNodeID: %v", err)
		}
		node := Node{ID: id, Name: name, ClaimedAt: now}
		if err := s.db.WithContext(ctx).Create(&node).Error; err != nil {
			t.Fatalf("create node %q: %v", name, err)
		}
		member := NodeMember{
			NodeID:    node.ID,
			UserID:    userID,
			Role:      NodeRoleOwner,
			GrantedBy: userID,
			CreatedAt: now,
		}
		if err := s.db.WithContext(ctx).Create(&member).Error; err != nil {
			t.Fatalf("create member for %q: %v", name, err)
		}
	}
}
