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

	// pendingIdleTimeout 是待定连接两次入站帧之间的最大间隔。
	//
	// 待定连接不参与节流型心跳判定：60 秒内没有任何帧就说明 agent 已经不在
	// 等这张码了，继续占着名额只会挤掉真正要配对的机器。
	pendingIdleTimeout = 60 * time.Second

	// pendingClaimTTL 是 agent 发起配对时短码的有效期（设计 §6：默认 5 分钟）。
	pendingClaimTTL = 5 * time.Minute

	// claimCodeLength 是 agent 发起配对时生成的短码长度。
	claimCodeLength = 5

	// claimCodeAttempts 是短码撞码时的重试次数：码空间有限，撞一次就换一个。
	claimCodeAttempts = 3

	// pairModePair 让控制面建/复用一条待定认领（扫码主路径）。
	pairModePair = "pair"

	// pairModeClaim 让 agent 出示账号事先出的码（`lework agent --code`）。
	pairModeClaim = "claim"
)

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

// pendingSourceIP 从 r.RemoteAddr 取来源主机地址。
//
// 只信任连接本身的对端地址，不解析 X-Forwarded-For：待定连接是 agent 直接
// 外拨的，转发头只是可伪造的额外输入。解析失败返回空串，由配额实现按
// 「只受全局上限约束」处理。
func pendingSourceIP(r *http.Request) string {
	remote := strings.TrimSpace(r.RemoteAddr)
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		logs.Debugf("agentgw: cannot parse pending connection remote address: %v", err)
		return ""
	}
	return host
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

	writer := g.newWriter(ws)
	defer func() { _ = writer.Close() }()

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
// 三态分派（设计 §6）：pending 重发同一个码、claimed 幂等重投、expired 关闭
// 连接让 agent 换一个 pair_key。订阅认领通知先于写出码，随后再复查一次状态，
// 覆盖「用户在看码之前就已经认领」和「通知早于订阅生效」两个窗口。
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
		// 幂等重投：认领成功但凭证没送达（实例挂掉、连接中断）时，agent 用
		// 同一个 pair_key 重连，这里重新签发一份，旧凭证随之失效。
		if claim.NodeID == "" {
			logs.Warnf("agentgw: claim %q is claimed without a node", claim.ID)
			closeWithError(ws, websocket.CloseInternalServerErr, "pairing incomplete")
			return
		}
		g.finishPendingPair(ctx, ws, writer, claim.ID, claim.NodeID)
		return
	case store.ClaimStatusPending:
	default:
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}

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
			g.deliverPendingPair(ctx, ws, writer, pairKey, claim.ID)
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
	// bug 的 agent 因为提前发事件被断开；pendingIdleTimeout 内无帧才放弃。
	for {
		if err := ws.SetReadDeadline(time.Now().Add(pendingIdleTimeout)); err != nil {
			return
		}
		var frame protocol.Frame
		if err := ws.ReadJSON(&frame); err != nil {
			return
		}
	}
}

// deliverPendingPair 处理一次认领通知：按最新认领行决定投递凭证还是关闭。
//
// 调用方用 sync.Once 保证每条连接只会走到这里一次；因此任何「还没到可投递
// 状态」的结果都只能关闭连接，让 agent 用同一 pair_key 重连走幂等重投，
// 而不是留下一条永远等不到通知的连接。
func (g *Gateway) deliverPendingPair(
	ctx context.Context,
	ws *websocket.Conn,
	writer node.Writer,
	pairKey, claimID string,
) {
	claim, found, err := g.store.ClaimByPairKey(ctx, pairKey)
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

	switch claim.Status {
	case store.ClaimStatusPending:
		// 通知早于认领可见（发布方提前发消息）：关连接，重连后按幂等重投补发。
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

	if err := g.publishPairResult(ctx, writer, claim.ID, claim.NodeID); err != nil {
		logs.Warnf("agentgw: deliver credential for claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	closeWithError(ws, websocket.CloseNormalClosure, "pair complete")
}

// serveClaimedPair 服务 `lework agent --code`：码由账号事先签出，pending 行
// 上的 ClaimedBy 就是这张码的归属账号，agent 出示码即完成绑定。
//
// 因此这里不需要确认步骤，也不发 pair.pending：直接写回机器信息、原子绑定
// 账号并下发凭证。码不可用（不存在、过期、已用、没有归属账号）一律关闭连接，
// 对 agent 只暴露同一个结论。
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

	claim, found, err := g.store.ClaimByCode(ctx, pair.Code, time.Now())
	if err != nil {
		logs.Warnf("agentgw: lookup claim code: %v", err)
		closeWithError(ws, websocket.CloseInternalServerErr, "pairing unavailable")
		return
	}
	if !found || strings.TrimSpace(claim.ClaimedBy) == "" {
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
		logs.Warnf("agentgw: bind claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.ClosePolicyViolation, "claim code unavailable")
		return
	}

	if err := g.publishPairResult(ctx, writer, claim.ID, bound.ID); err != nil {
		logs.Warnf("agentgw: deliver credential for claim %q: %v", claim.ID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	closeWithError(ws, websocket.CloseNormalClosure, "pair complete")
}

// finishPendingPair 把「连接建立时已经 claimed」的幂等重投一次性收尾。
func (g *Gateway) finishPendingPair(
	ctx context.Context,
	ws *websocket.Conn,
	writer node.Writer,
	claimID, nodeID string,
) {
	if err := g.publishPairResult(ctx, writer, claimID, nodeID); err != nil {
		logs.Warnf("agentgw: deliver credential for claim %q: %v", claimID, err)
		closeWithError(ws, websocket.CloseInternalServerErr, "credential delivery failed")
		return
	}
	closeWithError(ws, websocket.CloseNormalClosure, "pair complete")
}

// publishPairResult 是待定连接上唯一允许出现明文凭证的出口：它签发新的节点
// 凭证、把哈希写回节点、补齐投递簿记，然后经 writer 发送 pair.result。
//
// nodeID 必须来自最新认领行（claimed 且 node_id 非空），本方法不会创建节点，
// 也不会把凭证与别的节点关联。任何失败都只返回错误；调用方只记录 claim id，
// 不记录凭证本身。名称里刻意不含 token/secret，避免被日志字段扫描误采。
func (g *Gateway) publishPairResult(ctx context.Context, writer node.Writer, claimID, nodeID string) error {
	raw, hash, err := authn.NewToken()
	if err != nil {
		return fmt.Errorf("issue node credential: %w", err)
	}
	// 覆盖写：幂等重投时旧凭证必须立刻失效，agent 手上前一份不能再连回来。
	if err := g.store.SetNodeCredential(ctx, nodeID, hash); err != nil {
		return fmt.Errorf("store node credential: %w", err)
	}
	// 簿记失败不中断投递：BindClaimToUser 已在事务里写好 node_id，这里只是
	// 幂等重投的补写；agent 正等着凭证，丢帧只会让它多等一次重连。
	if err := g.store.MarkClaimDelivered(ctx, claimID, nodeID); err != nil {
		logs.Warnf("agentgw: record claim %q delivery: %v", claimID, err)
	}

	payload, err := json.Marshal(pairResult{NodeID: nodeID, Credential: raw})
	if err != nil {
		return fmt.Errorf("encode pair result: %w", err)
	}
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
