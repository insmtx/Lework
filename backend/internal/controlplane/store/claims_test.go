package store

import (
	"context"
	"errors"
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

func TestClaimClaimForUserIsAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateClaim(ctx, NodeClaim{PairKey: "pk", Code: "AB234", Status: "pending", ExpiresAt: time.Now().Add(time.Minute)})

	got, ok1, _ := s.ClaimClaimForUser(ctx, c.ID, "u1")
	ok2claim, ok2, _ := s.ClaimClaimForUser(ctx, c.ID, "u2")
	if !ok1 || ok2 {
		t.Fatalf("ok1 = %v, ok2 = %v", ok1, ok2)
	}
	if got.Status != ClaimStatusClaimed || got.ClaimedBy != "u1" {
		t.Fatalf("claimed row = %+v", got)
	}
	if ok2claim.Status != "" {
		t.Fatalf("losing claim returned a row: %+v", ok2claim)
	}
}

func TestBumpClaimAttemptsExpiresAfterMax(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c, _ := s.CreateClaim(ctx, NodeClaim{PairKey: "pk2", Code: "CD345", Status: "pending", ExpiresAt: time.Now().Add(time.Minute)})

	for i := 0; i < 3; i++ {
		if ok, _ := s.BumpClaimAttempts(ctx, c.ID, 3); !ok {
			t.Fatalf("attempt %d rejected early", i)
		}
	}
	if ok, _ := s.BumpClaimAttempts(ctx, c.ID, 3); ok {
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

// TestCreateClaimReusesPairKeyRow 固化「重连拿到同一个码」：pair_key 是 agent
// 生成的稳定标识，第二次建认领必须是同一行，而不是新建一条让终端上的码失效。
func TestCreateClaimReusesPairKeyRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first := newPendingClaim(t, s, "pk-reuse", "ab234")
	second, err := s.CreateClaim(ctx, NodeClaim{
		PairKey: "pk-reuse", Code: "ZZ999",
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
		PairKey: "pk-reuse", Code: "ab234",
		ExpiresAt: time.Now().UTC().Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim on claimed pair key: %v", err)
	}
	if again.ID != first.ID || again.Status != ClaimStatusClaimed {
		t.Fatalf("claimed reuse = {%q, %q}, want {%q, %q}", again.ID, again.Status, first.ID, ClaimStatusClaimed)
	}
}

// TestCreateClaimRejectsCodeInUse 覆盖应用层的码唯一性：码列只有普通索引，
// 并发下可能重复，但「一张码同时对应两个待定认领」会让参与者认错机器。
func TestCreateClaimRejectsCodeInUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	newPendingClaim(t, s, "pk-a", "AB2CD")

	_, err := s.CreateClaim(ctx, NodeClaim{
		PairKey: "pk-b", Code: "ab2cd",
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
		PairKey: "pk-old", Code: "ZZZZZ",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}); err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}
	if _, err := s.CreateClaim(ctx, NodeClaim{
		PairKey: "pk-new", Code: "zzzzz",
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
		PairKey: "pk-expired", Code: "CD345",
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

// TestClaimByPairKeyIgnoresStatus 固化 gateway 的幂等重投路径：拿到已认领或
// 已过期的行才能决定「重发凭证」还是「关连接」。这里不能按状态过滤。
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

	expired, err := s.CreateClaim(ctx, NodeClaim{
		PairKey: "pk-expired", Code: "CD345",
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	if err != nil {
		t.Fatalf("CreateClaim(expired): %v", err)
	}
	got, ok, err = s.ClaimByPairKey(ctx, "pk-expired")
	if err != nil || !ok {
		t.Fatalf("ClaimByPairKey(expired) = %v, %v", ok, err)
	}
	if got.ID != expired.ID || got.Status != ClaimStatusPending {
		t.Fatalf("expired claim = {%q, %q}, want {%q, %q}", got.ID, got.Status, expired.ID, ClaimStatusPending)
	}

	if _, ok, err := s.ClaimByPairKey(ctx, "pk-missing"); err != nil || ok {
		t.Fatalf("ClaimByPairKey(unknown) = %v, %v; want a miss", ok, err)
	}
	if _, ok, err := s.ClaimByPairKey(ctx, "  "); err != nil || ok {
		t.Fatalf("ClaimByPairKey(blank) = %v, %v; want a miss", ok, err)
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

	if role, ok, _ := s.NodeRole(ctx, node.ID, "u1"); !ok || role != NodeRoleOwner {
		t.Fatalf("role = %q, %v; want owner", role, ok)
	}

	got, ok, err := s.ClaimByPairKey(ctx, "pk-bind")
	if err != nil || !ok {
		t.Fatalf("ClaimByPairKey = %v, %v", ok, err)
	}
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
		PairKey: "pk-expired-bind", Code: "CD345",
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
	got, _, _ := s.ClaimByPairKey(ctx, "pk-deliver")
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
	var stored NodeClaim
	if err := s.db.WithContext(ctx).Where("id = ?", c.ID).Take(&stored).Error; err != nil {
		t.Fatalf("reload claim: %v", err)
	}
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
