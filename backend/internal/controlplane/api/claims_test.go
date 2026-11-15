package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

// createdClaimBody 是 POST /api/node-claims 的响应形状。
type createdClaimBody struct {
	Code      string    `json:"code"`
	NodeName  string    `json:"node_name"`
	ExpiresAt time.Time `json:"expires_at"`
}

// previewClaimBody 是 GET /api/claim/{code} 的响应形状。
type previewClaimBody struct {
	Code         string    `json:"code"`
	NodeName     string    `json:"node_name"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	AgentVersion string    `json:"agent_version"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// fakeCorePub 记录 PublishCore 的 subject 与负载。
//
// 认领通知只带 claim id，因此断言点就是 subject；fail 让用例覆盖「通知发不
// 出去时认领仍然成功」的降级路径。
type fakeCorePub struct {
	mu       sync.Mutex
	subjects []string
	fail     error
}

func (f *fakeCorePub) PublishCore(_ context.Context, subject string, _ []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.fail != nil {
		return f.fail
	}
	f.subjects = append(f.subjects, subject)
	return nil
}

func (f *fakeCorePub) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.subjects...)
}

func (f *fakeCorePub) setFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.fail = err
}

// pairSubjectOrFail 返回认领通知的 subject；构建失败直接终止用例。
func pairSubjectOrFail(t *testing.T, claimID string) string {
	t.Helper()

	subject, err := messaging.ControlPlanePairSubject(claimID)
	if err != nil {
		t.Fatalf("pair subject for claim %q: %v", claimID, err)
	}
	return subject
}

// createPendingClaim 直接写库造一条待定认领，码与过期时间由用例指定。
//
// 码碰撞与过期边界都需要精确控制，走接口的出码路径（随机码 + 配置 TTL）
// 无法稳定构造这些状态。
func createPendingClaim(t *testing.T, env *testEnv, code, name string, expiresAt time.Time) store.NodeClaim {
	t.Helper()

	claim, err := env.store.CreateClaim(context.Background(), store.NodeClaim{
		PairKey:      "pk-" + code,
		Code:         code,
		NodeName:     name,
		OS:           "linux",
		Arch:         "amd64",
		AgentVersion: "0.2.0",
		Status:       store.ClaimStatusPending,
		ExpiresAt:    expiresAt,
	})
	if err != nil {
		t.Fatalf("create pending claim %q: %v", code, err)
	}
	return claim
}

// createNodeClaim 以账号身份出码，返回解析后的响应。
func createNodeClaim(t *testing.T, env *testEnv, userID, body string) createdClaimBody {
	t.Helper()

	rec := doJSONAs(t, env, userID, http.MethodPost, "/api/node-claims", body)
	requireStatus(t, rec, http.StatusOK)
	return decodeBody[createdClaimBody](t, rec)
}

// requireExpiresIn 断言响应里的过期时间与期望时长相差不超过 10 秒。
func requireExpiresIn(t *testing.T, expiresAt time.Time, want time.Duration) {
	t.Helper()

	remaining := time.Until(expiresAt)
	if remaining < want-10*time.Second || remaining > want+10*time.Second {
		t.Fatalf("expires_at is %s away, want about %s", remaining, want)
	}
}

// TestCreateClaimEndpointRequiresLogin 固化任务书第一步：没有 principal 的
// 请求必须先在 requireUser 得到 401，而不是落到「按账号出码」。
func TestCreateClaimEndpointRequiresLogin(t *testing.T) {
	env := newTestEnv(t, time.Second)

	rec := env.doNoPrincipal(http.MethodPost, "/api/node-claims", `{}`)
	requireErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
}

// TestClaimEndpointsRequireAuthentication 覆盖三个认领端点的未认证路径。
func TestClaimEndpointsRequireAuthentication(t *testing.T) {
	env := newTestEnv(t, time.Second)

	for _, tc := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{"preview", http.MethodGet, "/api/claim/AB234", ""},
		{"claim", http.MethodPost, "/api/claim/AB234", ""},
		{"create", http.MethodPost, "/api/node-claims", `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := env.doNoPrincipal(tc.method, tc.target, tc.body)
			requireErrorCode(t, rec, http.StatusUnauthorized, "unauthorized")
		})
	}
}

// TestClaimCodeBindsNodeAndPublishesNotification 是任务书的端到端用例：
// 预览 → 确认 → 建节点 + owner 行 → 发布 cp.pair → 同一张码不可再用。
func TestClaimCodeBindsNodeAndPublishesNotification(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	claim := createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))

	preview := doJSONAs(t, env, user.ID, http.MethodGet, "/api/claim/AB234", "")
	requireStatus(t, preview, http.StatusOK)
	body := decodeBody[previewClaimBody](t, preview)
	if body.NodeName != "work-mac" || body.OS != "linux" || body.Arch != "amd64" {
		t.Fatalf("preview = %+v, want work-mac/linux/amd64", body)
	}
	if body.AgentVersion != "0.2.0" || body.Code != "AB234" {
		t.Fatalf("preview = %+v, want agent version 0.2.0 and code AB234", body)
	}

	created := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", "")
	requireStatus(t, created, http.StatusOK)
	view := decodeBody[nodeResponse](t, created)
	if !view.Claimed {
		t.Fatalf("claimed = false in the claim response, want true")
	}
	if view.Name != "work-mac" {
		t.Fatalf("node name = %q, want work-mac", view.Name)
	}

	// cp.pair 只发一次，且必须指向这条 claim。
	want := pairSubjectOrFail(t, claim.ID)
	if subjects := env.corePub.snapshot(); len(subjects) != 1 || subjects[0] != want {
		t.Fatalf("published subjects = %v, want exactly [%s]", subjects, want)
	}

	// 归属行与认领时间都必须落库：节点列表靠它们决定可见性。
	role, ok, err := env.store.NodeRole(context.Background(), view.ID, user.ID)
	if err != nil {
		t.Fatalf("lookup node role: %v", err)
	}
	if !ok || role != store.NodeRoleOwner {
		t.Fatalf("role = %q ok = %v, want owner", role, ok)
	}
	owned, err := env.store.ListNodesForUser(context.Background(), user.ID)
	if err != nil {
		t.Fatalf("list nodes for user: %v", err)
	}
	if len(owned) != 1 || owned[0].ClaimedAt.IsZero() {
		t.Fatalf("owned nodes = %+v, want one claimed node", owned)
	}

	again := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", "")
	requireErrorCode(t, again, http.StatusNotFound, "claim_not_found")
}

// TestPreviewClaimNotFoundIsIndistinguishable 固化「不暴露码的状态」：
// 不存在、已过期、已被认领三种情况必须返回逐字节相同的 404。
func TestPreviewClaimNotFoundIsIndistinguishable(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	other := createTestUser(t, env, "other@x.c", testLoginPassword, roleUser, statusActive)

	createPendingClaim(t, env, "EEEEE", "expired-box", time.Now().Add(-time.Minute))
	claimed := createPendingClaim(t, env, "CCCCC", "claimed-box", time.Now().Add(time.Minute))
	if _, err := env.store.BindClaimToUser(context.Background(), claimed.ID, other.ID, claimed.NodeName); err != nil {
		t.Fatalf("bind claimed claim: %v", err)
	}

	unknown := doJSONAs(t, env, user.ID, http.MethodGet, "/api/claim/ZZZZZ", "")
	expired := doJSONAs(t, env, user.ID, http.MethodGet, "/api/claim/EEEEE", "")
	already := doJSONAs(t, env, user.ID, http.MethodGet, "/api/claim/CCCCC", "")

	wantBody := unknown.Body.String()
	for name, rec := range map[string]*httptest.ResponseRecorder{
		"unknown": unknown,
		"expired": expired,
		"claimed": already,
	} {
		requireErrorCode(t, rec, http.StatusNotFound, "claim_not_found")
		if rec.Body.String() != wantBody {
			t.Fatalf("%s body = %q, want byte-identical to unknown %q", name, rec.Body.String(), wantBody)
		}
	}
}

// TestCreateClaimIssuesAccountScopedCode 覆盖网页出码：码长、归属账号、
// pair key 与状态都按控制器裁决写入。
func TestCreateClaimIssuesAccountScopedCode(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)

	body := createNodeClaim(t, env, user.ID, `{"node_name":"work-mac"}`)
	if len(body.Code) != defaultClaimCodeLength {
		t.Fatalf("code length = %d, want %d", len(body.Code), defaultClaimCodeLength)
	}
	if body.NodeName != "work-mac" {
		t.Fatalf("node_name = %q, want work-mac", body.NodeName)
	}

	stored, ok, err := env.store.ClaimByCode(context.Background(), body.Code, time.Now())
	if err != nil {
		t.Fatalf("lookup created claim: %v", err)
	}
	if !ok {
		t.Fatal("created claim is not pending/unexpired")
	}
	if stored.ClaimedBy != user.ID {
		t.Fatalf("claimed_by = %q, want %q", stored.ClaimedBy, user.ID)
	}
	if strings.TrimSpace(stored.PairKey) == "" {
		t.Fatal("pair key is empty, want a fresh unique token")
	}

	// 空请求体按零值处理：命令行/脚本出码时可以不带任何字段。
	empty := createNodeClaim(t, env, user.ID, "")
	if len(empty.Code) != defaultClaimCodeLength {
		t.Fatalf("code length with empty body = %d, want %d", len(empty.Code), defaultClaimCodeLength)
	}
}

// TestCreateClaimTTLDefaultsAndClamps 固化 TTL 语义：缺省用配置默认值，
// 更短的请求被尊重，更长的请求被夹到配置上限。
func TestCreateClaimTTLDefaultsAndClamps(t *testing.T) {
	t.Run("uses default when omitted", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)

		body := createNodeClaim(t, env, user.ID, `{}`)
		requireExpiresIn(t, body.ExpiresAt, defaultClaimTTL)
	})

	t.Run("honors a shorter request", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)

		body := createNodeClaim(t, env, user.ID, `{"ttl":30}`)
		requireExpiresIn(t, body.ExpiresAt, 30*time.Second)
	})

	t.Run("clamps to the configured maximum", func(t *testing.T) {
		env := newTestEnv(t, time.Second)
		user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
		env.api.claimCfg = ClaimConfig{TTL: time.Minute, CodeLength: 4}

		body := createNodeClaim(t, env, user.ID, `{"ttl":3600}`)
		requireExpiresIn(t, body.ExpiresAt, time.Minute)
		if len(body.Code) != 4 {
			t.Fatalf("code length = %d, want configured 4", len(body.Code))
		}
	})
}

// TestCreateClaimRetriesCodeCollision 证明撞码时换一个码重试，而不是把
// 冲突直接回给浏览器。
//
// 随机码空间无法构造可复现的碰撞，因此替换生成器（newClaimCode）注入一个
// 确定的碰撞序列；被测的仍是真实的 CreateClaim / ErrClaimCodeTaken 路径。
func TestCreateClaimRetriesCodeCollision(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	taken := createPendingClaim(t, env, "AAAAA", "other-box", time.Now().Add(time.Minute))

	calls := 0
	env.api.newClaimCode = func(int) (string, error) {
		calls++
		if calls == 1 {
			return "AAAAA", nil
		}
		return "BBBBB", nil
	}

	body := createNodeClaim(t, env, user.ID, `{}`)
	if body.Code != "BBBBB" {
		t.Fatalf("code = %q, want the second candidate BBBBB", body.Code)
	}
	if calls != 2 {
		t.Fatalf("code generator called %d times, want 2", calls)
	}

	// 已有的待定认领不能被出码流程改写。
	existing, ok, err := env.store.ClaimByID(context.Background(), taken.ID)
	if err != nil || !ok {
		t.Fatalf("load pre-existing claim: ok=%v err=%v", ok, err)
	}
	if existing.Code != "AAAAA" || existing.ClaimedBy != "" {
		t.Fatalf("pre-existing claim = %+v, want untouched", existing)
	}
}

// TestCreateClaimFailsAfterRetryExhaustion 固化「撞码重试也有上限」：
// 生成器持续返回已被占用的码时返回 500，而不是无限循环。
func TestCreateClaimFailsAfterRetryExhaustion(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	createPendingClaim(t, env, "AAAAA", "other-box", time.Now().Add(time.Minute))

	calls := 0
	env.api.newClaimCode = func(int) (string, error) {
		calls++
		return "AAAAA", nil
	}

	rec := doJSONAs(t, env, user.ID, http.MethodPost, "/api/node-claims", `{}`)
	requireErrorCode(t, rec, http.StatusInternalServerError, "internal_error")
	if calls != claimCodeRetries {
		t.Fatalf("code generator called %d times, want %d", calls, claimCodeRetries)
	}
}

// TestClaimEndpointsRejectDeploymentPrincipals 固化控制器裁决：出码与确认
// 都需要真实账号；local / admin_token 只能预览。
func TestClaimEndpointsRejectDeploymentPrincipals(t *testing.T) {
	env := newTestEnv(t, time.Second)
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))

	for name, principal := range map[string]authz.Principal{
		"local":       {Kind: authz.KindLocal, Role: roleAdmin},
		"admin_token": {Kind: authz.KindAdminToken, Role: roleAdmin},
	} {
		t.Run(name, func(t *testing.T) {
			create := doJSONAsRole(t, env, principal, http.MethodPost, "/api/node-claims", `{}`)
			requireErrorCode(t, create, http.StatusForbidden, "user_required")

			claim := doJSONAsRole(t, env, principal, http.MethodPost, "/api/claim/AB234", "")
			requireErrorCode(t, claim, http.StatusForbidden, "user_required")

			// 预览只读机器信息，部署级身份允许：码本身已经是能力。
			preview := doJSONAsRole(t, env, principal, http.MethodGet, "/api/claim/AB234", "")
			requireStatus(t, preview, http.StatusOK)
			if body := decodeBody[previewClaimBody](t, preview); body.NodeName != "work-mac" {
				t.Fatalf("preview node_name = %q, want work-mac", body.NodeName)
			}
		})
	}
}

// TestClaimCodeSecondAccountGetsNotFound 覆盖两个账号先后认领同一张码。
//
// 串行请求下第二个账号必然拿到 404：POST 每次都会重新按码查 pending，
// 第一次确认已经把它变成 claimed。409 claim_taken 只会出现在「第二个请求
// 已经查到 pending、但在绑定之前第一个请求完成」的交叉窗口里，单测无法
// 稳定制造；这里同时断言没有第二个节点。
func TestClaimCodeSecondAccountGetsNotFound(t *testing.T) {
	env := newTestEnv(t, time.Second)
	first := createTestUser(t, env, "first@x.c", testLoginPassword, roleUser, statusActive)
	second := createTestUser(t, env, "second@x.c", testLoginPassword, roleUser, statusActive)
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))

	requireStatus(t, doJSONAs(t, env, first.ID, http.MethodPost, "/api/claim/AB234", ""), http.StatusOK)

	loser := doJSONAs(t, env, second.ID, http.MethodPost, "/api/claim/AB234", "")
	requireErrorCode(t, loser, http.StatusNotFound, "claim_not_found")

	firstNodes, err := env.store.ListNodesForUser(context.Background(), first.ID)
	if err != nil {
		t.Fatalf("list nodes for first account: %v", err)
	}
	if len(firstNodes) != 1 {
		t.Fatalf("first account nodes = %d, want 1", len(firstNodes))
	}
	secondNodes, err := env.store.ListNodesForUser(context.Background(), second.ID)
	if err != nil {
		t.Fatalf("list nodes for second account: %v", err)
	}
	if len(secondNodes) != 0 {
		t.Fatalf("second account nodes = %d, want 0", len(secondNodes))
	}
}

// TestClaimCodeRateLimitsFailedAttempts 固化「每账号 10 次失败/分钟」：
// 第 11 次直接 429，且超限后即使出示有效码也被限流挡住。
func TestClaimCodeRateLimitsFailedAttempts(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)

	for i := 0; i < claimAttemptsPerAccount; i++ {
		rec := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/ZZZZZ", "")
		requireErrorCode(t, rec, http.StatusNotFound, "claim_not_found")
	}

	limited := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/ZZZZZ", "")
	requireErrorCode(t, limited, http.StatusTooManyRequests, "too_many_attempts")

	// 预算在查库之前生效：超限期间连有效码也不放行，避免「限流只挡错码」。
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))
	valid := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", "")
	requireErrorCode(t, valid, http.StatusTooManyRequests, "too_many_attempts")
}

// TestClaimSuccessResetsOnlyItsOwnBudget 固化限流键的隔离：
// 成功只清当前账号的失败计数，别的账号的预算不受影响。
func TestClaimSuccessResetsOnlyItsOwnBudget(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	other := createTestUser(t, env, "other@x.c", testLoginPassword, roleUser, statusActive)

	// 先用掉 9 次预算，再成功认领一次：计数应被清零。
	for i := 0; i < claimAttemptsPerAccount-1; i++ {
		rec := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/ZZZZZ", "")
		requireErrorCode(t, rec, http.StatusNotFound, "claim_not_found")
	}
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))
	requireStatus(t, doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", ""), http.StatusOK)

	// 清零后还能再失败 10 次；若没有清零，第 2 次就会 429。
	for i := 0; i < claimAttemptsPerAccount; i++ {
		rec := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/ZZZZY", "")
		requireErrorCode(t, rec, http.StatusNotFound, "claim_not_found")
	}
	requireErrorCode(t,
		doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/ZZZZY", ""),
		http.StatusTooManyRequests, "too_many_attempts")

	// 另一个账号的 budget 与上面那次成功无关：同样 10 次后超限。
	for i := 0; i < claimAttemptsPerAccount; i++ {
		rec := doJSONAs(t, env, other.ID, http.MethodPost, "/api/claim/ZZZZX", "")
		requireErrorCode(t, rec, http.StatusNotFound, "claim_not_found")
	}
	requireErrorCode(t,
		doJSONAs(t, env, other.ID, http.MethodPost, "/api/claim/ZZZZX", ""),
		http.StatusTooManyRequests, "too_many_attempts")
}

// TestClaimCodeNameCollisionAddsSuffix 固化设计 §6：同一 owner 重装产生
// 同名节点时自动加 -2 后缀，而不是拒绝认领。
func TestClaimCodeNameCollisionAddsSuffix(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)

	existing, _, _ := env.addNode("work-mac")
	mustAddMember(t, env, existing.ID, user.ID, store.NodeRoleOwner)
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))

	created := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", "")
	requireStatus(t, created, http.StatusOK)
	if view := decodeBody[nodeResponse](t, created); view.Name != "work-mac-2" {
		t.Fatalf("node name = %q, want work-mac-2", view.Name)
	}
}

// TestClaimCodePublishesMemberGrantedEvent 证明确认后立即广播成员变更，
// 让 SSE 订阅者不必等满 30 秒的重算周期。
func TestClaimCodePublishesMemberGrantedEvent(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))

	events, cancel := env.hub.Subscribe(8)
	defer cancel()

	created := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", "")
	requireStatus(t, created, http.StatusOK)
	view := decodeBody[nodeResponse](t, created)

	select {
	case got := <-events:
		if got.Type != claimMemberEventType {
			t.Fatalf("event type = %q, want %q", got.Type, claimMemberEventType)
		}
		if got.NodeID != view.ID {
			t.Fatalf("event node = %q, want %q", got.NodeID, view.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no node.member.granted event reached the hub")
	}
}

// TestClaimCodeSucceedsWhenPairNotificationFails 固化降级语义：认领已经
// 落库，Core NATS 发布失败只能记 warn，不能把浏览器请求变成失败——agent
// 会在待定期限内用同一 pair_key 重连补发。
func TestClaimCodeSucceedsWhenPairNotificationFails(t *testing.T) {
	env := newTestEnv(t, time.Second)
	user := createTestUser(t, env, "u@x.c", testLoginPassword, roleUser, statusActive)
	createPendingClaim(t, env, "AB234", "work-mac", time.Now().Add(time.Minute))
	env.corePub.setFail(errors.New("core nats down"))

	created := doJSONAs(t, env, user.ID, http.MethodPost, "/api/claim/AB234", "")
	requireStatus(t, created, http.StatusOK)
	if view := decodeBody[nodeResponse](t, created); !view.Claimed {
		t.Fatal("claimed = false, want the node to be created despite the publish failure")
	}
}
