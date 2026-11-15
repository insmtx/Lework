// 本文件实现待定连接（Pair 握手）与凭证下发。
//
// 待定连接不是节点：它不登记注册表、不接收转发请求，只负责把一条认领码
// 送到 agent，并在账号认领后用同一根 WebSocket 下发一次节点凭证。凭证明文
// 只出现在 pair.result 帧里，日志、错误与事件都不得包含它。
package agentgw

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ygpkg/yg-go/logs"

	"github.com/insmtx/lework/backend/internal/agentd/protocol"
	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/node"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
	"github.com/insmtx/lework/backend/pkg/messaging"
)

const (
	// maxPendingPerIP 是同一来源 IP 允许同时存在的待定连接数（设计 §10）。
	maxPendingPerIP = 3

	// maxPendingTotal 是单个控制面实例允许同时存在的待定连接总数。
	maxPendingTotal = 100

	// pendingClaimTTL 是 agent 发起配对时短码的有效期（设计 §6：默认 5 分钟）。
	pendingClaimTTL = 5 * time.Minute

	// claimCodeLength 是 agent 发起配对时生成的短码长度。
	claimCodeLength = 5

	// claimCodeAttempts 是短码撞码时的重试次数：码空间有限，撞一次就换一个。
	claimCodeAttempts = 3

	// claimAttemptsPerIP 是同一来源 IP 每分钟允许的 mode=claim 失败次数。
	claimAttemptsPerIP = 10

	// claimAttemptsPerKey 是同一个 agent pair_key 允许连续猜码的失败次数。
	claimAttemptsPerKey = 20

	// claimAttemptWindowSize 是失败计数的时间窗；窗口从第一次失败开始计算。
	claimAttemptWindowSize = time.Minute

	// pairModePair 让控制面建/复用一条待定认领（扫码主路径）。
	pairModePair = "pair"

	// pairModeClaim 让 agent 出示账号事先出的码（`lework agent --code`）。
	pairModeClaim = "claim"

	// pairConnReplacedReason 是同一认领被新连接接管时旧连接的关闭原因。
	pairConnReplacedReason = "pair connection replaced"

	// credentialAlreadyIssuedReason 是节点已有凭证时拒绝重投的关闭原因。
	credentialAlreadyIssuedReason = "credential already issued"

	// claimAttemptsExceededReason 是猜码失败次数超限的关闭原因。
	claimAttemptsExceededReason = "claim code attempts exceeded"
)

// pendingIdleTimeout 是待定连接两次入站帧之间的最大间隔。
//
// 待定连接不参与节流型心跳判定：60 秒内没有任何帧就说明 agent 已经不在
// 等这张码了，继续占着名额只会挤掉真正要配对的机器。
//
// 做成变量只为测试能缩短等待；生产代码不得在运行时改写它。
var pendingIdleTimeout = 60 * time.Second

// pendingMaxLifetime 是待定连接的绝对生存期，从连接建立起算。
//
// 入站帧只能续上空闲窗口，不能延长它：否则一个客户端每 59 秒发一帧就能永久
// 占住配额，几十个地址即可占满全局名额。到点无条件关闭，与帧活动无关。
//
// 做成变量只为测试能缩短等待；生产代码不得在运行时改写它。
var pendingMaxLifetime = 5 * time.Minute

// pairParams 是待定连接 hello 的参数，字段与 agent 侧的握手一一对应。
//
// 缺省 Mode 按 pair 处理：旧版 agent 只发 pair_key 时不至于被拒绝。
type pairParams struct {
	Mode    string `json:"mode"`
	PairKey string `json:"pair_key"`
	Code    string `json:"code"`
}

// pairPendingResult 是 pair.pending 帧的结果；Code 是终端上要展示的短码。
type pairPendingResult struct {
	ClaimID string `json:"claim_id"`
	Code    string `json:"code"`
}

// pairResult 是 pair.result 帧的结果；Credential 由本文件签发，只此一处明文。
type pairResult struct {
	NodeID     string `json:"node_id"`
	Credential string `json:"credential"`
}

// pairWriter 给待定连接写出的帧补上 hello 的 ID。
//
// publishPairResult 的签名不携带请求 ID（它只关心 claim 与 node），而 agent
// 需要按 hello ID 关联结果帧；在这里补写比给每个调用方加参数更不容易漏。
type pairWriter struct {
	node.Writer
	id string
}

// Write 只在帧本身没有 ID 时补写，显式指定的 ID（如 pair.pending）保持不变。
func (w *pairWriter) Write(ctx context.Context, f protocol.Frame) error {
	if f.ID == "" {
		f.ID = w.id
	}
	return w.Writer.Write(ctx, f)
}

// pendingLimiter 统计本实例上同时存在的待定连接。
//
// 配额在升级之前检查，被拒的请求只消耗一次 HTTP 往返，不会占用 WebSocket
// 连接或订阅。来源 IP 解析失败时按「不计数但记 debug 日志」处理：拒绝会误伤
// 合法部署（例如自定义 RemoteAddr 的代理），而全局上限仍能兜住总量。
type pendingLimiter struct {
	mu    sync.Mutex
	perIP map[string]int
	total int
}

func newPendingLimiter() *pendingLimiter {
	return &pendingLimiter{perIP: make(map[string]int)}
}

// acquire 申请一个待定连接名额；ip 为空表示不可按 IP 计数。
func (l *pendingLimiter) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.total >= maxPendingTotal {
		return false
	}
	if ip != "" {
		if l.perIP[ip] >= maxPendingPerIP {
			return false
		}
		l.perIP[ip]++
	}
	l.total++
	return true
}

// release 归还一个名额；必须与成功的 acquire 成对调用。
func (l *pendingLimiter) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.total > 0 {
		l.total--
	}
	if ip == "" {
		return
	}
	if n := l.perIP[ip]; n > 1 {
		l.perIP[ip] = n - 1
		return
	}
	delete(l.perIP, ip)
}

// claimAttemptWindow 是一条来源/IP 键上的失败计数窗口。
type claimAttemptWindow struct {
	count int
	start time.Time
}

// claimAttemptLimiter 限制 mode=claim 的猜码失败次数。
//
// 配额分两层：来源 IP 挡住「一台机器轮询码空间」，pair_key 挡住「同一 agent
// 连续换码猜测」。成功的认领不消耗配额，也不重置 IP 的失败计数——只有持有
// 有效码的人才会成功，攻击者无法用成功来洗白失败记录。
//
// 计数只在进程内存里：多实例部署时每个实例各自限流，攻击者换实例会得到新的
// 预算。这是本任务的已知边界，跨实例的全局限流留给后续任务。
type claimAttemptLimiter struct {
	mu     sync.Mutex
	perIP  map[string]claimAttemptWindow
	perKey map[string]claimAttemptWindow

	perIPLimit  int
	perKeyLimit int
	window      time.Duration
}

func newClaimAttemptLimiter(perIP, perKey int, window time.Duration) *claimAttemptLimiter {
	return &claimAttemptLimiter{
		perIP:       make(map[string]claimAttemptWindow),
		perKey:      make(map[string]claimAttemptWindow),
		perIPLimit:  perIP,
		perKeyLimit: perKey,
		window:      window,
	}
}

// allow 判断这次尝试是否还在预算内；被拒时调用方应关闭连接。
//
// 空 IP / 空 pair_key 表示无法归因（代理或旧版 agent），跳过对应维度而不是
// 把它们合并成同一个桶：合并会让一个匿名客户端耗尽所有人的预算。
func (l *claimAttemptLimiter) allow(ip, key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.pruneLocked(now)

	if ip != "" && l.perIPLimit > 0 {
		entry := l.perIP[ip]
		count, _ := claimAttemptCount(entry, now, l.window)
		if count >= l.perIPLimit {
			return false
		}
	}
	if key != "" && l.perKeyLimit > 0 {
		entry := l.perKey[key]
		count, _ := claimAttemptCount(entry, now, l.window)
		if count >= l.perKeyLimit {
			return false
		}
	}
	return true
}

// fail 记录一次失败尝试；只有码查不到、码不可用或绑定失败才调用。
func (l *claimAttemptLimiter) fail(ip, key string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if ip != "" && l.perIPLimit > 0 {
		count, start := claimAttemptCount(l.perIP[ip], now, l.window)
		l.perIP[ip] = claimAttemptWindow{count: count + 1, start: start}
	}
	if key != "" && l.perKeyLimit > 0 {
		count, start := claimAttemptCount(l.perKey[key], now, l.window)
		l.perKey[key] = claimAttemptWindow{count: count + 1, start: start}
	}
}

// succeed 清掉这个 pair_key 的失败计数。
//
// 成功之后同一个 agent 的预算重新开始：合法 agent 可能先输错几次再输对，
// 之后重新配对不该继承之前的失败。IP 计数刻意不清理，避免换码成功成为
// 「洗白」同一地址失败记录的手段。
func (l *claimAttemptLimiter) succeed(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if key != "" {
		delete(l.perKey, key)
	}
}

// pruneLocked 清理已经过窗的条目，避免大量随机 IP 把内存撑大。
// 条目不多时跳过遍历，正常流量因此没有额外开销。
func (l *claimAttemptLimiter) pruneLocked(now time.Time) {
	if len(l.perIP) < 64 && len(l.perKey) < 64 {
		return
	}
	cutoff := now.Add(-l.window)
	for key, entry := range l.perIP {
		if entry.start.Before(cutoff) {
			delete(l.perIP, key)
		}
	}
	for key, entry := range l.perKey {
		if entry.start.Before(cutoff) {
			delete(l.perKey, key)
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

// sourceHost 从 RemoteAddr 取主机部分；解析失败返回空串。
//
// 只信任连接本身的对端地址，不解析 X-Forwarded-For：待定连接是 agent 直接
// 外拨的，转发头只是可伪造的额外输入。
func sourceHost(remote string) string {
	remote = strings.TrimSpace(remote)
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		logs.Debugf("agentgw: cannot parse pending connection remote address: %v", err)
		return ""
	}
	return host
}

// pendingSourceIP 从 HTTP 请求取来源主机地址。
func pendingSourceIP(r *http.Request) string {
	return sourceHost(r.RemoteAddr)
}

// pendingConnSourceIP 从已升级的连接取来源主机地址，用于 claim 模式限流。
func pendingConnSourceIP(ws *websocket.Conn) string {
	return sourceHost(ws.RemoteAddr().String())
}

// servePair 服务一条待定连接，直到凭证下发、认领过期或连接失效。
//
// 与普通节点路径的区别是它不走 identify/welcome/注册表：节点尚不存在，身份
// 要等账号认领后才产生。mode 必为 authPair（调用方已按握手头分派），secret
// 是 Pair 头的值，仅在 claim 模式下作为短码的回退来源。
func (g *Gateway) servePair(ctx context.Context, ws *websocket.Conn, mode authMode, secret string) {
	if mode != authPair {
		// 防御性检查：只有 Pair 握手能进这里，凭证路径不能复用这条流程。
		closeWithError(ws, websocket.CloseInternalServerErr, "unsupported pair mode")
		return
	}

	hello, params, err := readHello(ws)
	if err != nil {
		closeWithError(ws, websocket.CloseProtocolError, "invalid hello")
		return
	}

	pair := pairParams{}
	if len(hello.Params) > 0 {
		if err := json.Unmarshal(hello.Params, &pair); err != nil {
			closeWithError(ws, websocket.CloseProtocolError, "invalid pair params")
			return
		}
	}
	pair.Mode = strings.ToLower(strings.TrimSpace(pair.Mode))
	pair.PairKey = strings.TrimSpace(pair.PairKey)
	pair.Code = strings.TrimSpace(pair.Code)
	if pair.Code == "" {
		// `--code` 路径可能把码放进 Pair 头：只用来查库，绝不写日志。
		pair.Code = strings.TrimSpace(secret)
	}

	// pair.result 需要带上 hello 的 ID 供 agent 关联，统一在这里补写。
	writer := &pairWriter{Writer: g.newWriter(ws), id: hello.ID}
	defer func() { _ = writer.Close() }()

	switch pair.Mode {
	case "", pairModePair:
		g.servePendingPair(ctx, ws, writer, hello.ID, params, pair.PairKey)
	case pairModeClaim:
		g.serveClaimedPair(ctx, ws, writer, params, pair)
	default:
		closeWithError(ws, websocket.CloseProtocolError, "unsupported pair mode")
	}
}

// servePendingPair 建/复用一条待定认领，写出 pair.pending 后等待认领通知。
//
// 三态分派（设计 §6）：pending 重发同一个码、claimed 只在节点还没有凭证时
// 补发、expired 关闭连接让 agent 换一个 pair_key。订阅认领通知先于写出码，
// 随后再复查一次状态，覆盖「用户在看码之前就已经认领」和「通知早于订阅生效」
// 两个窗口。
//
// 同一 claim 只认一条当前连接：新连接接管旧的（旧连接收到
// pair connection replaced），投递前还要再确认自己仍是当前连接，避免半开旧
// 连接和立即重连各收到一份凭证、后写库的那份作废另一台机器。
func (g *Gateway) servePendingPair(
	ctx context.Context,
	ws *websocket.Conn,
	writer node.Writer,
	helloID string,
	params helloParams,
	pairKey string,
) {
	if pairKey == "" {
		closeWithError(ws, websocket.CloseProtocolError, "pair key required")
		return
	}

	started := time.Now()
	idle := pendingIdleTimeout
	maxLifetime := pendingMaxLifetime

	claim, found, err := g.store.ClaimByPairKey(ctx, pairKey)
	if err != nil {
		logs.Warnf("agentgw: lookup pending pair: %v", err)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}
	if !found {
		claim, err = g.createPendingClaim(ctx, pairKey, params)
		if err != nil {
			// 错误只来自存储与短码生成，不含 pair_key 或码。
			logs.Warnf("agentgw: create pending claim: %v", err)
			closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
			return
		}
	}

	switch claim.Status {
	case store.ClaimStatusExpired:
		// TTL 到期的行不在同一 pair_key 上复活：agent 必须换一个 pair_key，
		// 否则终端会一直显示一张已经不可能被认领的码。
		closeWithError(ws, websocket.ClosePolicyViolation, "pairing expired")
		return
	case store.ClaimStatusClaimed:
		// 认领成功但凭证没送达（实例挂掉、连接中断）时，agent 用同一个
		// pair_key 重连。只有节点还没有凭证才补发；已有凭证时关闭连接，
		// 否则拿到 pair_key 的人可以永久轮换合法 agent 的凭证。
		g.registerPendingConn(claim.ID, ws)
		defer g.unregisterPendingConn(claim.ID, ws)
		g.deliverPendingClaim(ctx, ws, writer, claim.ID, pairKey)
		return
	case store.ClaimStatusPending:
	default:
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}

	// 先接管这条 claim 的当前连接，再订阅通知：旧连接即使还半开着，也不能
	// 在下面任何一步抢到投递权。
	g.registerPendingConn(claim.ID, ws)
	defer g.unregisterPendingConn(claim.ID, ws)

	subject, err := messaging.ControlPlanePairSubject(claim.ID)
	if err != nil {
		logs.Warnf("agentgw: pair subject for claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}

	// 机器信息尽力写回：失败只影响确认页展示，不该让配对整体失败。行被并发
	// 认领/删除时状态会由后面的复查纠正，这里只处理「行已经不存在」。
	if err := g.store.UpdateClaimMachine(ctx, claim.ID, machineFromHello(params)); err != nil {
		if errors.Is(err, store.ErrClaimNotFound) {
			closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
			return
		}
		logs.Warnf("agentgw: update pending claim %q: %v", claim.ID, err)
	}

	subCtx, cancelSub := context.WithCancel(ctx)
	defer cancelSub()

	// 同一连接只投递一次：订阅回调与随后的复查可能同时命中。
	//
	// 回调运行在订阅协程里，而主协程阻塞在读循环中；投递完必须主动关闭套接字，
	// 否则主协程要等到空闲超时才会退出。
	var once sync.Once
	deliver := func() {
		once.Do(func() {
			g.deliverPendingClaim(ctx, ws, writer, claim.ID, pairKey)
			_ = ws.Close()
		})
	}

	subscribed := make(chan struct{})
	go func() {
		// SubscribeCore 没有「订阅生效」回调，进入阻塞订阅前放行；之后写出的
		// pair.pending 因此不会早于订阅动作。
		close(subscribed)
		err := g.coreSub.SubscribeCore(subCtx, subject, "", func(context.Context, []byte) []byte {
			deliver()
			return nil
		})
		if err != nil && subCtx.Err() == nil {
			logs.Warnf("agentgw: pair notification subscription for claim %q stopped: %v", claim.ID, err)
		}
	}()
	<-subscribed

	payload, err := json.Marshal(pairPendingResult{ClaimID: claim.ID, Code: claim.Code})
	if err != nil {
		return
	}
	if err := writer.Write(ctx, protocol.Frame{
		Type:   protocol.FramePairPending,
		ID:     helloID,
		Result: payload,
	}); err != nil {
		return
	}

	// 复查：码可能在上面的订阅与写出之间就已经被认领（尤其账号先出的码）。
	latest, found, err := g.store.ClaimByPairKey(ctx, pairKey)
	if err != nil {
		logs.Warnf("agentgw: recheck pending pair: %v", err)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}
	if found && latest.Status != store.ClaimStatusPending {
		deliver()
		return
	}

	// 待定连接只把入站帧当作存活信号：心跳以外的内容一律忽略，避免一个有
	// bug 的 agent 因为提前发事件被断开。空闲窗口每次入站帧重置，但绝对生存期
	// 从连接建立起算、不随帧活动延长。
	deadline := started.Add(maxLifetime)
	for {
		readBy := time.Now().Add(idle)
		if readBy.After(deadline) {
			readBy = deadline
		}
		if err := ws.SetReadDeadline(readBy); err != nil {
			return
		}
		var frame protocol.Frame
		if err := ws.ReadJSON(&frame); err != nil {
			return
		}
	}
}

// registerPendingConn 把 ws 设为该 claim 的唯一当前连接。
//
// 已有别的连接时先给它一个明确的关闭原因再替换。旧连接退出时只会删除
// 「指针仍是自己」的条目，因此不会误删刚接管的新连接。
func (g *Gateway) registerPendingConn(claimID string, ws *websocket.Conn) {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()

	if prev := g.pendingConns[claimID]; prev != nil && prev != ws {
		closeWithError(prev, websocket.ClosePolicyViolation, pairConnReplacedReason)
	}
	g.pendingConns[claimID] = ws
}

// unregisterPendingConn 只在当前指针仍是 ws 时删除，避免旧连接清掉新连接。
func (g *Gateway) unregisterPendingConn(claimID string, ws *websocket.Conn) {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()

	if g.pendingConns[claimID] == ws {
		delete(g.pendingConns, claimID)
	}
}

// deliverPendingClaim 处理一次认领通知或已认领重连：确认本连接仍是当前连接、
// 认领确实属于本连接、节点还没有凭证，然后签发并写出凭证。
//
// 整段在 pendingMu 下执行：当前连接检查与凭证轮换必须原子，否则两条连接可以
// 各自通过检查、各写一份 hash，后写的让先拿到的凭证失效。任何不满足投递
// 条件的情况都关闭连接，让 agent 明确失败而不是留下一条永远等不到结果的连接。
func (g *Gateway) deliverPendingClaim(
	ctx context.Context,
	ws *websocket.Conn,
	writer node.Writer,
	claimID, pairKey string,
) {
	g.pendingMu.Lock()
	defer g.pendingMu.Unlock()

	if g.pendingConns[claimID] != ws {
		// 已被更新的连接接管：由新连接负责投递，这里不生成、不写库、不写帧。
		return
	}

	claim, found, err := g.store.ClaimByID(ctx, claimID)
	if err != nil {
		logs.Warnf("agentgw: reload claim %q for delivery: %v", claimID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	if !found {
		logs.Warnf("agentgw: claim %q disappeared before delivery", claimID)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	// 通知里的 claim id 必须就是本连接那条：按 id 读回来后再校验 pair_key，
	// 避免「按 pair_key 读」把投递绑定到别的行上。
	if strings.TrimSpace(claim.PairKey) != pairKey {
		logs.Warnf("agentgw: claim %q does not belong to this pair connection", claimID)
		closeWithError(ws, websocket.ClosePolicyViolation, "pair connection mismatch")
		return
	}

	switch claim.Status {
	case store.ClaimStatusPending:
		// 通知早于认领可见（发布方提前发消息）：关连接，重连后按补发路径处理。
		logs.Warnf("agentgw: claim %q notified while still pending", claim.ID)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	case store.ClaimStatusExpired:
		closeWithError(ws, websocket.ClosePolicyViolation, "pairing expired")
		return
	case store.ClaimStatusClaimed:
	default:
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}

	if claim.NodeID == "" {
		logs.Warnf("agentgw: claim %q is claimed without a node", claim.ID)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing incomplete")
		return
	}

	rec, found, err := g.store.NodeByID(ctx, claim.NodeID)
	if err != nil {
		logs.Warnf("agentgw: load node %q for claim %q: %v", claim.NodeID, claim.ID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	if !found {
		logs.Warnf("agentgw: node %q for claim %q is missing", claim.NodeID, claim.ID)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing incomplete")
		return
	}
	if strings.TrimSpace(rec.CredentialHash) != "" {
		// 节点已有凭证：绝不重投。pair_key 泄露不能让任何人轮换合法 agent
		// 的凭证；agent 应提示用户重新配对。
		closeWithError(ws, websocket.ClosePolicyViolation, credentialAlreadyIssuedReason)
		return
	}

	if err := g.publishPairResult(ctx, writer, claim.ID, claim.NodeID); err != nil {
		logs.Warnf("agentgw: deliver credential for claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	// 簿记放在帧写成功之后：先记账后写帧会让「库里说已投递」与 agent 实际
	// 没收到分叉。失败只 warn，凭证已经在 agent 手上，不影响本次投递。
	if err := g.store.MarkClaimDelivered(ctx, claim.ID, claim.NodeID); err != nil {
		logs.Warnf("agentgw: record claim %q delivery: %v", claim.ID, err)
	}
	closeWithError(ws, websocket.CloseNormalClosure, "pair complete")
}

// serveClaimedPair 服务 `lework agent --code`：码由账号事先签出，pending 行
// 上的 ClaimedBy 就是这张码的归属账号，agent 出示码即完成绑定。
//
// 因此这里不需要确认步骤，也不发 pair.pending：直接写回机器信息、原子绑定
// 账号并下发凭证。码不可用（不存在、过期、已用、没有归属账号）一律关闭连接，
// 对 agent 只暴露同一个结论。
//
// mode=claim 不需要登录就能提交任意码，因此按来源 IP 与 agent 的 pair_key
// 限制失败次数；成功不消耗配额。超出预算直接关闭，不再查库。
func (g *Gateway) serveClaimedPair(
	ctx context.Context,
	ws *websocket.Conn,
	writer node.Writer,
	params helloParams,
	pair pairParams,
) {
	if pair.Code == "" {
		closeWithError(ws, websocket.CloseProtocolError, "claim code required")
		return
	}

	sourceIP := pendingConnSourceIP(ws)
	now := time.Now()
	if !g.claimAttempts.allow(sourceIP, pair.PairKey, now) {
		closeWithError(ws, websocket.ClosePolicyViolation, claimAttemptsExceededReason)
		return
	}

	claim, found, err := g.store.ClaimByCode(ctx, pair.Code, now)
	if err != nil {
		logs.Warnf("agentgw: lookup claim code: %v", err)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}
	if !found || strings.TrimSpace(claim.ClaimedBy) == "" {
		g.claimAttempts.fail(sourceIP, pair.PairKey, time.Now())
		closeWithError(ws, websocket.ClosePolicyViolation, "claim code unavailable")
		return
	}

	// ErrClaimTaken 表示行刚被并发认领：交给 BindClaimToUser 给出同样的结论。
	if err := g.store.UpdateClaimMachine(ctx, claim.ID, machineFromHello(params)); err != nil &&
		!errors.Is(err, store.ErrClaimTaken) {
		logs.Warnf("agentgw: update claim %q machine info: %v", claim.ID, err)
	}

	bound, err := g.store.BindClaimToUser(ctx, claim.ID, claim.ClaimedBy, params.NodeName)
	if err != nil {
		// 错误里只有 claim id 包装，不含码、pair_key 或凭证。
		g.claimAttempts.fail(sourceIP, pair.PairKey, time.Now())
		logs.Warnf("agentgw: bind claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.ClosePolicyViolation, "claim code unavailable")
		return
	}
	g.claimAttempts.succeed(pair.PairKey)

	// 这里不登记 pendingConns：claim 模式的行由账号预先签出、没有并行的
	// pair.pending 连接，绑定本身是原子的——并发用同一张码时只有一条连接能
	// 绑定成功，另一条在 BindClaimToUser 就失败了，不会各发一份凭证。

	if err := g.publishPairResult(ctx, writer, claim.ID, bound.ID); err != nil {
		logs.Warnf("agentgw: deliver credential for claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	if err := g.store.MarkClaimDelivered(ctx, claim.ID, bound.ID); err != nil {
		logs.Warnf("agentgw: record claim %q delivery: %v", claim.ID, err)
	}
	closeWithError(ws, websocket.CloseNormalClosure, "pair complete")
}

// publishPairResult 是待定连接上唯一允许出现明文凭证的出口：它签发新的节点
// 凭证、把哈希写回节点，然后经 writer 发送 pair.result。
//
// nodeID 必须来自最新认领行（claimed 且 node_id 非空），本方法不会创建节点，
// 也不会把凭证与别的节点关联。调用方负责确认节点尚无凭证：本方法只负责签发，
// 不负责判断「是否允许签发」。任何失败都只返回错误；调用方只记录 claim id，
// 不记录凭证本身。名称里刻意不含 token/secret，避免被日志字段扫描误采。
func (g *Gateway) publishPairResult(ctx context.Context, writer node.Writer, claimID, nodeID string) error {
	raw, hash, err := authn.NewToken()
	if err != nil {
		return fmt.Errorf("issue node credential: %w", err)
	}
	// 覆盖写兜住两个实例并发的最后写入者语义；单实例内由 pendingMu 串行化，
	// 调用前也已确认节点没有凭证，正常路径不会轮换已下发的凭证。
	if err := g.store.SetNodeCredential(ctx, nodeID, hash); err != nil {
		return fmt.Errorf("store node credential: %w", err)
	}

	payload, err := json.Marshal(pairResult{NodeID: nodeID, Credential: raw})
	if err != nil {
		return fmt.Errorf("encode pair result: %w", err)
	}
	// 帧的 ID 由 pairWriter 从 hello 补写；claimID 由调用方用于日志与
	// MarkClaimDelivered 簿记，不进入帧内容。
	if err := writer.Write(ctx, protocol.Frame{
		Type:   protocol.FramePairResult,
		Result: payload,
	}); err != nil {
		return fmt.Errorf("write pair result: %w", err)
	}
	return nil
}

// createPendingClaim 为一条待定连接生成短码并落库；撞码时换一个码重试。
func (g *Gateway) createPendingClaim(ctx context.Context, pairKey string, params helloParams) (store.NodeClaim, error) {
	meta := machineFromHello(params)
	meta.PairKey = pairKey
	meta.ExpiresAt = time.Now().UTC().Add(pendingClaimTTL)

	var lastErr error
	for i := 0; i < claimCodeAttempts; i++ {
		code, err := authn.NewShortCode(claimCodeLength)
		if err != nil {
			return store.NodeClaim{}, fmt.Errorf("issue claim code: %w", err)
		}
		meta.Code = code
		claim, err := g.store.CreateClaim(ctx, meta)
		if err == nil {
			return claim, nil
		}
		lastErr = err
		if !errors.Is(err, store.ErrClaimCodeTaken) {
			return store.NodeClaim{}, err
		}
	}
	return store.NodeClaim{}, fmt.Errorf("issue claim code after %d attempts: %w", claimCodeAttempts, lastErr)
}

// machineFromHello 只取展示用的机器信息，身份与授权字段一概不带。
func machineFromHello(params helloParams) store.NodeClaim {
	return store.NodeClaim{
		NodeName:     params.NodeName,
		OS:           params.OS,
		Arch:         params.Arch,
		AgentVersion: params.AgentVersion,
	}
}
