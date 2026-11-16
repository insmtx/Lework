// Package authz 把 HTTP 请求解析成控制面的 Principal，并回答「这个身份能不能
// 看到或操作某台节点」。
//
// 控制面同时存在三种身份来源：本机回环直通（部署者本人）、浏览器登录会话与
// 共享 enrollment token（脚本、旧部署、首个管理员的引导凭证）。把解析与授权
// 收进这一层，是为了让 API、SSE 与推送共用同一份判定，而不是每个 handler
// 各自解释「谁在请求」。
//
// 本包不写响应、不写日志，也从不把任何令牌明文放进错误文本：只有调用方知道
// 该回 401 还是 404，也只有调用方能保证错误不会进日志。
package authz

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// KindUser 表示请求来自已登录账号，权限边界由 node_members 决定。
	KindUser = "user"
	// KindAdminToken 表示请求出示了 enrollment token。它是部署级共享密钥，
	// 语义上等于旧版管理员：能操作所有节点，但没有对应的账号。
	KindAdminToken = "admin_token"
	// KindLocal 表示请求直接来自本机回环且开启了 LocalAdmin 直通。
	KindLocal = "local"

	// DefaultCookieName 是浏览器会话 cookie 的默认名称。
	//
	// 导出它是为了让签发方（登录接口）与解析方使用同一个常量，避免两处字面量
	// 各自漂移后「登录成功但一直未认证」。
	DefaultCookieName = "lework_session"

	// roleAdmin 是 User.Role 的管理员取值；statusActive 是唯一允许登录的状态。
	//
	// 用「等于 active」而不是「不等于 disabled」判断：CreateUser 不会补默认
	// 状态，漏填 Status 的账号必须被拒绝，而不是默默获得登录权。
	roleAdmin    = "admin"
	statusActive = "active"

	// sessionRenewCeiling 是滑动续期的最小写库间隔上限。
	//
	// 每次请求都写一次 last_seen_at 会让一个打开的浏览器把数据库写满；活跃
	// 时间只需要小时级精度，1 小时既能反映「最近用过」，也把写入压到可忽略。
	// 实际间隔由 renewInterval 按 SessionTTL 收缩，避免短会话永远到不了续期点。
	sessionRenewCeiling = time.Hour
)

// Principal 是一次已认证请求的身份。
//
// Kind 与 UserID 分开表达而不是用「UserID 是否为空」推断：本机直通与共享令牌
// 都没有账号，但它们的可见范围与审计语义不同，混成一个布尔值后授权判定就
// 无处安放。Role 只对 Kind == user 有意义。
type Principal struct {
	Kind   string
	UserID string
	Role   string
	// SessionID 是本次请求所用会话的行 ID，只在 Kind == user 且凭证是会话
	// 令牌时非空。登出要精确吊销「这一条」会话；只凭 UserID 清空全部会话会
	// 把手机端一起踢下线，与用户点「退出登录」的预期不符。
	SessionID string
}

// IsAdmin 判断该身份能否进入管理员接口。
//
// 「管理员」与「能看到所有节点」是两件事，这里只回答前者：Role == admin 的
// 普通账号可以管理邀请码与成员，但节点可见范围仍由 VisibleNodeIDs 按归属
// 计算，不会因此看到别人的节点。
func (p Principal) IsAdmin() bool {
	return p.Role == roleAdmin || p.Kind == KindAdminToken || p.Kind == KindLocal
}

// Resolver 按固定顺序把请求解析成 Principal。
//
// 顺序是有意的：本机直通最先（它代表部署者本人），其次是会话 cookie，最后才是
// 共享 enrollment token。cookie 优先于 Bearer，是为了让「浏览器里登录着普通
// 账号、同时又带了管理员令牌」的请求以普通账号身份执行——一次误带令牌不该把
// 操作记到管理员头上。
type Resolver struct {
	// Store 用于校验会话与查询账号，是唯一的持久化依赖。
	Store *store.Store
	// EnrollmentToken 是共享注册令牌；为空时 Bearer 一律不通过。
	EnrollmentToken string
	// CookieName 是会话 cookie 名称；为空时回落到 DefaultCookieName。
	CookieName string
	// LocalAdmin 表示本机回环请求直接视为管理员。
	LocalAdmin bool
	// SessionTTL 是滑动续期后的会话有效期；非正时不做续期（会话只在登录时
	// 设置过期时间），便于嵌入方显式关闭续期或使用自己的生命周期策略。
	SessionTTL time.Duration

	// touchSession 是续期写库的入口；生产保持 nil，由 renewSession 回落到
	// Store.TouchSession。测试注入必定失败的实现，用来证明续期失败不会影响
	// 本次已经通过校验的请求。
	touchSession func(ctx context.Context, sessionID string, expiresAt, lastSeen time.Time) error
}

// Resolve 返回请求对应的 Principal：未认证是 (zero, false, nil)，基础设施故障
// 是 (zero, false, err)。
//
// 凭证顺序是「本机直通 → 会话 cookie → Bearer」，其中 Bearer 依次按部署级
// enrollment token 与会话令牌解释：前者给脚本与旧部署，后者让没有 cookie 罐的
// 移动端复用登录得到的同一条会话 token。不写响应、不写日志，也不区分「没有
// 凭证」与「凭证无效」：调用方统一按未认证处理，既避免泄漏账号是否存在，也
// 避免在错误文本里带出令牌。
func (v Resolver) Resolve(r *http.Request) (Principal, bool, error) {
	if r == nil {
		return Principal{}, false, nil
	}

	if v.LocalAdmin && IsLocalRequest(r) {
		// 本机直通解析成真实管理员账号：本机的节点、偏好、移除操作都写入
		// 这个账号，手机用同一账号登录时看到的是同一份归属。查不到管理员
		// （被手动删除、库不可用）时回落到部署级 KindLocal，别把本机锁死。
		if v.Store != nil {
			if user, ok, err := v.Store.FirstAdminUser(r.Context()); err == nil && ok {
				return Principal{Kind: KindUser, UserID: user.ID, Role: roleAdmin}, true, nil
			}
		}
		return Principal{Kind: KindLocal}, true, nil
	}

	if raw, ok := sessionCookie(r, v.cookieName()); ok {
		principal, found, err := v.resolveSession(r.Context(), raw)
		if err != nil {
			return Principal{}, false, err
		}
		if found {
			return principal, true, nil
		}
	}

	if bearer, ok := bearerToken(r.Header.Get("Authorization")); ok && bearer != "" {
		if v.tokenMatches(bearer) {
			return Principal{Kind: KindAdminToken}, true, nil
		}
		// 共享令牌未命中时再按会话令牌试一次：移动端不保存 cookie，设计让它
		// 复用登录响应里的同一条 token 走 Bearer。复用 resolveSession 而不是
		// 复制一份查询，保证两条入口的过期、状态与角色判定永远一致。
		principal, found, err := v.resolveSession(r.Context(), bearer)
		if err != nil {
			return Principal{}, false, err
		}
		if found {
			return principal, true, nil
		}
	}

	return Principal{}, false, nil
}

// resolveSession 用会话令牌明文（cookie 或移动端 Bearer）查会话，再确认账号
// 仍然可用。
//
// 库里只有 sha256，明文在哈希之后立即失去用途。哈希未命中、会话过期、账号被
// 删除或停用都返回 found=false 而不是错误：这些是「凭证不再有效」的正常分支，
// 让 Resolve 继续尝试下一个凭证。真实故障才返回 error，由调用方回 500。
func (v Resolver) resolveSession(ctx context.Context, raw string) (Principal, bool, error) {
	if v.Store == nil {
		return Principal{}, false, errors.New("校验会话: 未配置存储")
	}

	session, ok, err := v.Store.SessionByTokenHash(ctx, authn.HashToken(raw))
	if err != nil {
		return Principal{}, false, fmt.Errorf("校验会话: %w", err)
	}
	if !ok {
		return Principal{}, false, nil
	}

	user, ok, err := v.Store.UserByID(ctx, session.UserID)
	if err != nil {
		return Principal{}, false, fmt.Errorf("读取会话账号: %w", err)
	}
	if !ok || user.Status != statusActive {
		return Principal{}, false, nil
	}

	// 续期是尽力而为：这次请求的凭证已经验证通过，不能因为续期写失败就把它
	// 变成未认证。详情见 renewSession。
	v.renewSession(ctx, session)

	return Principal{Kind: KindUser, UserID: user.ID, Role: user.Role, SessionID: session.ID}, true, nil
}

// renewSession 把会话的过期时间与活跃时间往后推，实现滑动续期。
//
// 只在 SessionTTL 为正且距上次续期超过 renewInterval 时写库：打开的浏览器
// 每个请求都写一次会把数据库写满，而小时级的活跃时间足够支撑「30 天不用才
// 过期」的语义。LastSeenAt 为零（历史行或刚创建未初始化）也视为需要续期，
// 避免这些会话永远停在零值上。
//
// 写库失败被刻意忽略：会话本身已经通过校验，把续期失败当成认证失败会让一次
// 数据库抖动踢掉所有在线用户；把错误向上抛则会让请求变成 500，结果相同。
// 下一次请求会重试；若直到过期时间仍未续上，会话自然失效，失败方向依然安全。
// 这里不记录任何 token 或会话 id 明文到日志。
func (v Resolver) renewSession(ctx context.Context, session store.UserSession) {
	if v.SessionTTL <= 0 {
		return
	}

	now := time.Now().UTC()
	if !session.LastSeenAt.IsZero() && now.Sub(session.LastSeenAt) < v.renewInterval() {
		return
	}

	touch := v.touchSession
	if touch == nil && v.Store != nil {
		touch = v.Store.TouchSession
	}
	if touch == nil {
		return
	}
	_ = touch(ctx, session.ID, now.Add(v.SessionTTL), now)
}

// renewInterval 返回两次续期写库之间的最小间隔：min(1h, SessionTTL/2)。
//
// 固定 1 小时的阈值只在 30 天会话上成立：若 SessionTTL 本身不超过 1 小时，
// last_seen_at 到过期都攒不满阈值，续期永远不会发生。取 TTL 的一半保证每个
// 会话寿命内至少有一个续期点。SessionTTL/2 为 0（纳秒级测试 TTL）时退回
// SessionTTL，宁可每请求写一次也不能静默关闭续期；SessionTTL<=0 由调用方
// 提前返回。
func (v Resolver) renewInterval() time.Duration {
	half := v.SessionTTL / 2
	if half <= 0 {
		return v.SessionTTL
	}
	if half < sessionRenewCeiling {
		return half
	}
	return sessionRenewCeiling
}

// SessionCookieName 返回实际使用的会话 cookie 名称。
//
// 导出它是为了让写响应的一方（登录、续期、登出）与解析方共用同一份判定：
// 自定义 CookieName 时若中间件仍按默认名回写，浏览器会同时持有两个 cookie。
func (v Resolver) SessionCookieName() string {
	return v.cookieName()
}

// SetSessionCookie 把会话令牌写进浏览器。
//
// 会话 cookie 的属性只在这里维护：登录时签发、中间件滑动续期时重签、登出时
// 清除都调用同一份实现，避免三处 MaxAge/Secure/SameSite 各自漂移后出现
// 「HTTPS 下 cookie 丢 Secure」或「续期后变成会话 cookie」。
//
// MaxAge 跟随 ttl；ttl <= 0 时不写 MaxAge（浏览器按会话 cookie 处理），而不是
// 传负数——负数会被序列化成 Max-Age=0，等于立即过期，语义正好相反。
// Secure 在请求本身是 TLS，或反向代理声明 X-Forwarded-Proto: https 时开启：
// 控制面自身不终止 TLS，只按 r.TLS 会让所有代理部署的 cookie 永远不带 Secure。
// 这里信任 X-Forwarded-Proto；直连客户端可以把它伪造成 https，但那只会让
// cookie 多一个 Secure 标记（在自己的明文连接上不会回传），不会降低安全性。
// 反向代理必须剥掉外部伪造的头并按实际协议重写它。
func SetSessionCookie(w http.ResponseWriter, r *http.Request, raw string, ttl time.Duration) {
	cookie := &http.Cookie{
		Name:     DefaultCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteStrictMode,
	}
	if ttl > 0 {
		cookie.MaxAge = int(ttl.Seconds())
	}
	http.SetCookie(w, cookie)
}

// secureRequest 判断会话 cookie 是否必须带 Secure。
//
// 请求本身是 TLS 时必然为真；否则取 X-Forwarded-Proto 的第一个逗号分隔值
// （代理链最左端，即客户端到第一跳的协议），去掉空白并忽略大小写比较。
// 没有请求对象（r == nil）时不加 Secure。
func secureRequest(r *http.Request) bool {
	if r == nil {
		return false
	}
	if r.TLS != nil {
		return true
	}
	proto := r.Header.Get("X-Forwarded-Proto")
	if idx := strings.IndexByte(proto, ','); idx >= 0 {
		proto = proto[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(proto), "https")
}

// ClearSessionCookie 让浏览器立即丢弃会话 cookie。
//
// 删除 cookie 不要求属性完全一致：浏览器按 (name, domain, path) 匹配，Secure
// 与 SameSite 只影响发送条件。因此这里不需要请求对象，也就不会因为某个调用方
// 拿不到 *http.Request 而无法登出。
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     DefaultCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// cookieName 返回实际使用的 cookie 名称。
func (v Resolver) cookieName() string {
	if name := strings.TrimSpace(v.CookieName); name != "" {
		return name
	}
	return DefaultCookieName
}

// tokenMatches 以恒定时间比较候选令牌与 enrollment token。
//
// 先各自取 sha256 再比较定长摘要：直接比较原文时长度不同会立刻返回，攻击者
// 能据此逐位试探令牌长度。哈希把两侧都变成 32 字节，比较只剩「是否相等」
// 这一点信息。空令牌不参与比较——没有配置密钥就没有可泄漏的内容。
func (v Resolver) tokenMatches(candidate string) bool {
	if v.EnrollmentToken == "" || candidate == "" {
		return false
	}
	want := sha256.Sum256([]byte(v.EnrollmentToken))
	got := sha256.Sum256([]byte(candidate))
	return subtle.ConstantTimeCompare(got[:], want[:]) == 1
}

// IsLocalRequest 判断请求是否直接来自本机回环。
//
// 只看 RemoteAddr：它由 HTTP 服务器填写，客户端无法伪造。转发头存在时一律
// 不算本机——反向代理常与控制面同机部署，那时所有外部请求的 RemoteAddr 都是
// 回环，若还认作本机管理员就等于取消了鉴权。地址解析失败按非本机处理，绝不
// 因格式怪异的 RemoteAddr 而 panic。
//
// 导出它是为了让鉴权中间件复用同一份判定：中间件还要在「尚未创建任何账号」
// 时给本机请求补一条直通，两处各写一份回环判定迟早会漂移。
func IsLocalRequest(r *http.Request) bool {
	if hasForwardingHeaders(r) {
		return false
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hasForwardingHeaders 判断请求是否携带任一代理转发头。
//
// 只看头是否存在而不看值：代理写入空值同样说明请求经过了转发，不能据此把
// 外部请求当作本机。
func hasForwardingHeaders(r *http.Request) bool {
	for _, name := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"} {
		if len(r.Header.Values(name)) > 0 {
			return true
		}
	}
	return false
}

// sessionCookie 读取会话 cookie；空值视为没有 cookie。
func sessionCookie(r *http.Request, name string) (string, bool) {
	cookie, err := r.Cookie(name)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

// bearerToken 从 Authorization 头解析 Bearer 令牌，方案名大小写不敏感。
func bearerToken(header string) (string, bool) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// NodeAllowed 回答「p 能不能操作 nodeID 指向的节点」。
//
// 只有 Kind == user 需要查归属表：admin_token 是部署级共享密钥，local 就是
// 部署者本人，二者都能操作任意节点。未知 Kind（包括零值）一律拒绝，也就是
// fail closed——新增凭证类型必须显式接进来，绝不能默认放行。
func NodeAllowed(ctx context.Context, st *store.Store, p Principal, nodeID string) (bool, error) {
	switch p.Kind {
	case KindAdminToken, KindLocal:
		return true, nil
	case KindUser:
		if st == nil {
			return false, errors.New("节点授权: 未配置存储")
		}
		_, ok, err := st.NodeRole(ctx, nodeID, p.UserID)
		if err != nil {
			return false, fmt.Errorf("查询节点 %q 的归属: %w", nodeID, err)
		}
		return ok, nil
	default:
		return false, nil
	}
}

// VisibleNodeIDs 返回 p 能看到的全部节点 ID，用于列表过滤与 SSE 订阅。
//
// Kind == user 永远包含 node_members 里列出的节点；角色为 admin 的账号再额外
// 并入「未认领」的历史节点——升级前登记的节点没有归属行，不并进来就没人能在
// 界面上认领它们。但即使是 admin 也看不到别人已认领的节点：「各自管各自的」
// 不能因为管理角色退化成互相可见，因此 admin 的全量访问仍然只属于 admin_token
// 与本机直通。
//
// admin_token / local 返回全部节点：未认领的历史节点本来就在 nodes 表里，是
// 「全部节点」的子集，所以这两条分支不需要再单独查一次 ListUnclaimedNodes。
//
// 未知 Kind 返回空集合而不是 nil：调用方拿到 nil 时容易误当成「不过滤」。
func VisibleNodeIDs(ctx context.Context, st *store.Store, p Principal) (map[string]struct{}, error) {
	if st == nil {
		return nil, errors.New("节点可见范围: 未配置存储")
	}

	switch p.Kind {
	case KindAdminToken, KindLocal:
		nodes, err := st.ListNodes(ctx)
		if err != nil {
			return nil, fmt.Errorf("列出全部节点: %w", err)
		}
		return nodeIDSet(nodes), nil
	case KindUser:
		nodes, err := st.ListNodesForUser(ctx, p.UserID)
		if err != nil {
			return nil, fmt.Errorf("列出账号所属节点: %w", err)
		}
		ids := nodeIDSet(nodes)
		if p.Role != roleAdmin || strings.TrimSpace(p.UserID) == "" {
			return ids, nil
		}

		unclaimed, err := st.ListUnclaimedNodes(ctx)
		if err != nil {
			return nil, fmt.Errorf("列出未认领节点: %w", err)
		}
		for _, n := range unclaimed {
			ids[n.ID] = struct{}{}
		}
		return ids, nil
	default:
		return map[string]struct{}{}, nil
	}
}

// nodeIDSet 把节点列表折叠成集合，方便调用方做 O(1) 过滤。
func nodeIDSet(nodes []store.Node) map[string]struct{} {
	out := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		out[n.ID] = struct{}{}
	}
	return out
}
