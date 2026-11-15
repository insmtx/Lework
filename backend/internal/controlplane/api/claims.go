package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/event"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

// 认领接口的本地默认值与上限。
//
// api 刻意不 import controlplane/config：这个包只关心「码活多久、码多长」，
// 引入整份服务配置会让接口单测必须装配无关字段，也让配置校验顺序变成隐式
// 依赖。这里的两项默认值与 config.DefaultClaimTTL / DefaultClaimCodeLength
// 保持一致，配置层负责在生产路径上覆盖它们。
const (
	defaultClaimTTL        = 5 * time.Minute
	defaultClaimCodeLength = 5

	// minClaimCodeLength / maxClaimCodeLength 取 config 与 authn.NewShortCode
	// 允许范围的交集：超出范围按默认长度处理，避免把配置错误变成 500。
	minClaimCodeLength = 4
	maxClaimCodeLength = 8

	// claimCodeRetries 是网页出码撞上已有待定码时的换码次数。
	//
	// 码空间 31^5 ≈ 25 bit，正常撞码概率极低；重试只是让「同时出两张码」
	// 不至于把冲突直接甩给浏览器。
	claimCodeRetries = 5

	// claimAttemptsPerAccount 是同一账号在一个窗口内允许的失败猜码次数。
	//
	// 抗爆破靠「一次性码 + 短 TTL + 每账号限流」而不是码长：4 位码在
	// 10 次/分钟的预算下也需要数天才能覆盖码空间，而码早已过期。
	claimAttemptsPerAccount = 10
	// claimAttemptWindowSize 是失败计数的时间窗，从第一次失败开始计算。
	claimAttemptWindowSize = time.Minute

	// maxClaimBindAttempts 与设计 §10 的「每 claim 20 次失败后作废」一致。
	// 它限制的是同一条认领被反复绑定失败的次数，不是账号级猜码预算。
	maxClaimBindAttempts = 20

	// claimNotificationTimeout 是 cp.pair 通知的发布超时。Core NATS 是至多
	// 一次投递，通知丢了由 agent 重连补发兜底，因此不能让它拖住浏览器响应。
	claimNotificationTimeout = 5 * time.Second

	// claimMemberEventType 是成员变更事件类型；SSE 收到后立即重算可见集合，
	// 不必等满 30 秒的重算周期。
	claimMemberEventType = "node.member.granted"
)

// ClaimConfig 是认领码接口需要的运行配置。
//
// 零值回落到包内默认值：调用方忘了装配配置时应当拿到一张保守的默认码
// （5 分钟、5 位），而不是一张立刻过期或长度不合法的码。
type ClaimConfig struct {
	// TTL 是认领码的有效期上限；非正时使用 5 分钟。
	TTL time.Duration
	// CodeLength 是认领码长度，取值 4..8；越界时使用 5 位。
	CodeLength int
}

// claimCreateRequest 是 POST /api/node-claims 的请求体。
type claimCreateRequest struct {
	// NodeName 是即将加入的机器展示名，可空：真正的名字以 agent hello 为准，
	// 这里只是让确认页在机器信息回写之前也有个可读标题。
	NodeName string `json:"node_name"`
	// TTLSeconds 是请求的码有效期（秒）。0 用配置默认值；超过配置上限时夹到
	// 上限——浏览器不能替服务端延长认领窗口。
	TTLSeconds int `json:"ttl"`
}

// claimCreateResponse 是网页出码的结果；node_name 只是回显，不是身份。
type claimCreateResponse struct {
	Code      string    `json:"code"`
	NodeName  string    `json:"node_name,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
}

// claimPreviewResponse 是确认页展示的机器信息。
//
// 只包含「是不是这台机器」需要的字段：账号归属、pair key 与实例都不在这里，
// 确认页拿到码本身就已经是能力，不需要更多授权信息。
type claimPreviewResponse struct {
	Code         string    `json:"code"`
	NodeName     string    `json:"node_name"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	AgentVersion string    `json:"agent_version"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// claimAttemptWindow 是一个账号的失败计数窗口。
type claimAttemptWindow struct {
	count int
	start time.Time
}

// claimAttemptLimiter 限制浏览器端按账号猜码的失败次数。
//
// 只在进程内存里：多实例部署时每个实例各自限流，攻击者换实例会得到新的
// 预算。这是与 agentgw 的猜码限流一致的已知边界，跨实例全局限流不在本接口
// 的职责里。成功的认领只清当前账号的计数（合法用户可能先输错几次再输对），
// 不影响其他账号。
type claimAttemptLimiter struct {
	mu         sync.Mutex
	perAccount map[string]claimAttemptWindow

	limit  int
	window time.Duration
}

func newClaimAttemptLimiter(limit int, window time.Duration) *claimAttemptLimiter {
	return &claimAttemptLimiter{
		perAccount: make(map[string]claimAttemptWindow),
		limit:      limit,
		window:     window,
	}
}

// allow 判断这次请求是否还在预算内；空账号表示无法归因，跳过限流。
func (l *claimAttemptLimiter) allow(account string, now time.Time) bool {
	if account == "" || l.limit <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(now)
	count, _ := claimAttemptCount(l.perAccount[account], now, l.window)
	return count < l.limit
}

// fail 记录一次失败；只有 404 的预览/确认才调用。
func (l *claimAttemptLimiter) fail(account string, now time.Time) {
	if account == "" || l.limit <= 0 {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	count, start := claimAttemptCount(l.perAccount[account], now, l.window)
	l.perAccount[account] = claimAttemptWindow{count: count + 1, start: start}
}

// succeed 清掉当前账号的失败计数；成功之后预算重新开始。
func (l *claimAttemptLimiter) succeed(account string) {
	if account == "" {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.perAccount, account)
}

// pruneLocked 清理已经过窗的条目，避免大量账号把内存撑大。
// 条目不多时跳过遍历，正常流量因此没有额外开销。
func (l *claimAttemptLimiter) pruneLocked(now time.Time) {
	if len(l.perAccount) < 64 {
		return
	}
	cutoff := now.Add(-l.window)
	for account, entry := range l.perAccount {
		if entry.start.Before(cutoff) {
			delete(l.perAccount, account)
		}
	}
}

// claimAttemptCount 把窗口外的计数折叠成 0，并返回窗口起点。
func claimAttemptCount(entry claimAttemptWindow, now time.Time, window time.Duration) (int, time.Time) {
	if entry.start.IsZero() || window <= 0 || now.Sub(entry.start) >= window {
		return 0, now
	}
	return entry.count, entry.start
}

// claimAccountKey 返回猜码限流的计数键。
//
// 只有真实账号参与限流：local / admin_token 是部署级身份（本机部署者或持有
// enrollment token 的运维），没有账号可供计数，也没有「多个用户分享同一个
// 预算」的语义。空键在限流器里表示不计数。
func claimAccountKey(p authz.Principal) string {
	if p.Kind == authz.KindUser {
		return p.UserID
	}
	return ""
}

// claimCodeLength 返回实际使用的码长；越界配置回落到默认值。
func (a *API) claimCodeLength() int {
	if n := a.claimCfg.CodeLength; n >= minClaimCodeLength && n <= maxClaimCodeLength {
		return n
	}
	return defaultClaimCodeLength
}

// claimTTL 返回实际使用的码有效期上限；非正配置回落到默认值。
func (a *API) claimTTL() time.Duration {
	if ttl := a.claimCfg.TTL; ttl > 0 {
		return ttl
	}
	return defaultClaimTTL
}

// decodeClaimRequest 解析可选请求体；空体按零值处理。
//
// 命令行与脚本出码常常不带 body，把 EOF 当成 400 只会让同一个动作有两种
// 写法；真正的解析错误（坏 JSON、超大请求体）仍然回 400。
func decodeClaimRequest(w http.ResponseWriter, r *http.Request, dst any) error {
	err := decodeRequest(w, r, dst)
	if err == nil || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

// handleCreateClaim 为当前账号签出一张认领码（网页「添加节点」/ --code）。
//
// 码记录在 pending 行的 ClaimedBy 上：`lework agent --code` 出示码时，
// 网关按这一列决定节点归属，因此出码本身就是一次归属声明。agent 没有
// pair key，PairKey 只是一个唯一随机串，保证这行可以独立存在。
func (a *API) handleCreateClaim(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if principal.Kind != authz.KindUser || principal.UserID == "" {
		writeAPIError(w, http.StatusForbidden, "user_required", "claim requires an account")
		return
	}

	var req claimCreateRequest
	if err := decodeClaimRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	ttl := a.claimTTL()
	if req.TTLSeconds > 0 {
		if requested := time.Duration(req.TTLSeconds) * time.Second; requested < ttl {
			ttl = requested
		}
	}
	expiresAt := time.Now().UTC().Add(ttl)

	pairKey, _, err := authn.NewToken()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	record := store.NodeClaim{
		PairKey:   pairKey,
		NodeName:  strings.TrimSpace(req.NodeName),
		Status:    store.ClaimStatusPending,
		ClaimedBy: principal.UserID,
		ExpiresAt: expiresAt,
	}

	var lastErr error
	for i := 0; i < claimCodeRetries; i++ {
		code, err := a.newClaimCode(a.claimCodeLength())
		if err != nil {
			lastErr = err
			break
		}
		record.Code = code
		claim, err := a.store.CreateClaim(r.Context(), record)
		if err == nil {
			writeJSON(w, http.StatusOK, claimCreateResponse{
				Code:      claim.Code,
				NodeName:  claim.NodeName,
				ExpiresAt: claim.ExpiresAt,
			})
			return
		}
		lastErr = err
		if !errors.Is(err, store.ErrClaimCodeTaken) {
			break
		}
	}

	// 错误文本只带 claim id / 长度，不含码本身；码是短暂有效的授权凭据。
	logs.Warnf("api: issue claim code: %v", lastErr)
	writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
}

// handlePreviewClaim 返回待定认领的机器信息，供确认页判断「是不是这台机器」。
//
// 任何已登录身份（账号或部署级身份）都可以预览：码本身是能力，且这里不返回
// 账号归属、pair key 或实例信息。不存在、过期、已被认领一律回同一个 404，
// 区分它们只会把「码是否存在过」与别人认领的时机泄露给猜码者。
func (a *API) handlePreviewClaim(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	now := time.Now().UTC()
	account := claimAccountKey(principal)
	if !a.claimAttempts.allow(account, now) {
		writeClaimAttemptsExceeded(w)
		return
	}

	claim, found, err := a.store.ClaimByCode(r.Context(), r.PathValue("code"), now)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !found {
		a.claimAttempts.fail(account, now)
		writeClaimNotFound(w)
		return
	}

	writeJSON(w, http.StatusOK, claimPreviewResponse{
		Code:         claim.Code,
		NodeName:     claim.NodeName,
		OS:           claim.OS,
		Arch:         claim.Arch,
		AgentVersion: claim.AgentVersion,
		ExpiresAt:    claim.ExpiresAt,
	})
}

// handleClaimCode 把待定认领绑定到当前账号，并在同一事务里建节点与归属行。
//
// 绑定由存储层原子完成：先查 pending 再绑会让两个账号同时认领同一台机器都
// 成功。绑定成功后发布 cp.pair.<claim_id> 通知持有待定连接的实例下发凭证，
// 再广播一条 node.member.granted 让 SSE 订阅者立即重算可见集合。
func (a *API) handleClaimCode(w http.ResponseWriter, r *http.Request) {
	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if principal.Kind != authz.KindUser || principal.UserID == "" {
		writeAPIError(w, http.StatusForbidden, "user_required", "claim requires an account")
		return
	}

	now := time.Now().UTC()
	if !a.claimAttempts.allow(principal.UserID, now) {
		writeClaimAttemptsExceeded(w)
		return
	}

	claim, found, err := a.store.ClaimByCode(r.Context(), r.PathValue("code"), now)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !found {
		a.claimAttempts.fail(principal.UserID, now)
		writeClaimNotFound(w)
		return
	}

	node, err := a.store.BindClaimToUser(r.Context(), claim.ID, principal.UserID, claim.NodeName)
	if err != nil {
		a.claimAttempts.fail(principal.UserID, now)
		switch {
		case errors.Is(err, store.ErrClaimNotFound):
			// 预览与绑定之间行被清理：对参与者就是「这张码不能再用」。
			writeClaimNotFound(w)
		case errors.Is(err, store.ErrClaimTaken):
			a.writeClaimBindConflict(w, r, claim.ID)
		default:
			logs.Warnf("api: bind claim %q: %v", claim.ID, err)
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		}
		return
	}

	// 认领已经提交：从这里开始即使通知发不出去也不能回失败，否则浏览器会
	// 以为没认领成功而重试，拿到的是 404。
	a.claimAttempts.succeed(principal.UserID)
	a.publishClaimNotification(r.Context(), claim.ID)
	a.hub.Publish(event.Event{
		NodeID: node.ID,
		Type:   claimMemberEventType,
		Time:   time.Now(),
	})

	// 读回完整视图（名称、在线状态、claimed）而不是手写第二个投影：
	// 节点列表与这里的形状必须完全一致。
	view, ok, err := a.findNode(r.Context(), node.ID)
	if err != nil || !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

// writeClaimBindConflict 把「预览之后码不能再绑定」映射成 409 或 429。
//
// 409 表示另一个账号抢先完成认领；429 表示这条认领因反复绑定失败被
// BumpClaimAttempts 作废。Bump 对「已被别人认领」的行同样返回 false，
// 单看返回值会把正常竞态误报成限流，因此返回 false 时再读一次状态区分。
func (a *API) writeClaimBindConflict(w http.ResponseWriter, r *http.Request, claimID string) {
	remaining, err := a.store.BumpClaimAttempts(r.Context(), claimID, maxClaimBindAttempts)
	if err != nil {
		if errors.Is(err, store.ErrClaimNotFound) {
			writeClaimNotFound(w)
			return
		}
		logs.Warnf("api: bump claim %q attempts: %v", claimID, err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if remaining {
		writeAPIError(w, http.StatusConflict, "claim_taken", "claim already claimed")
		return
	}

	claim, found, err := a.store.ClaimByID(r.Context(), claimID)
	if err != nil {
		logs.Warnf("api: reload claim %q: %v", claimID, err)
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if found && claim.Status == store.ClaimStatusExpired {
		writeClaimAttemptsExceeded(w)
		return
	}
	writeAPIError(w, http.StatusConflict, "claim_taken", "claim already claimed")
}

// publishClaimNotification 通知持有待定连接的实例把认领推进到发凭证这一步。
//
// 只传 claim id：凭证由持有 WebSocket 的实例本地签发，绝不经过总线。Core
// NATS 是至多一次投递，发布失败不影响已经落库的认领——agent 用同一 pair_key
// 重连会在待定期限内补发，因此这里只记 warn（不含码与 pair key）。
func (a *API) publishClaimNotification(ctx context.Context, claimID string) {
	subject, err := messaging.ControlPlanePairSubject(claimID)
	if err != nil {
		logs.Warnf("api: claim notification subject for claim %q: %v", claimID, err)
		return
	}
	if a.core == nil {
		logs.Warnf("api: claim notification for claim %q skipped: no core publisher", claimID)
		return
	}

	pubCtx, cancel := context.WithTimeout(ctx, claimNotificationTimeout)
	defer cancel()
	if err := a.core.PublishCore(pubCtx, subject, nil); err != nil {
		logs.Warnf("api: publish claim notification for claim %q: %v", claimID, err)
	}
}

// writeClaimNotFound 回固定的 404 正文。
//
// 「码不存在」「已过期」「被别的账号认领」共用同一个正文：区分它们等于把
// 「这张码是否存在过」以及别人认领的时机泄露给猜码者。
func writeClaimNotFound(w http.ResponseWriter) {
	writeAPIError(w, http.StatusNotFound, "claim_not_found", "claim not found")
}

// writeClaimAttemptsExceeded 回固定的 429 正文；码或账号都不出现在错误体里。
func writeClaimAttemptsExceeded(w http.ResponseWriter) {
	writeAPIError(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts")
}
