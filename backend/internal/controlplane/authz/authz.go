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
}

// Resolve 返回请求对应的 Principal：未认证是 (zero, false, nil)，基础设施故障
// 是 (zero, false, err)。
//
// 不写响应、不写日志，也不区分「没有凭证」与「凭证无效」：调用方统一按未认证
// 处理，既避免泄漏账号是否存在，也避免在错误文本里带出令牌。
func (v Resolver) Resolve(r *http.Request) (Principal, bool, error) {
	if r == nil {
		return Principal{}, false, nil
	}

	if v.LocalAdmin && isLocalRequest(r) {
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

	if bearer, ok := bearerToken(r.Header.Get("Authorization")); ok && v.tokenMatches(bearer) {
		return Principal{Kind: KindAdminToken}, true, nil
	}

	return Principal{}, false, nil
}

// resolveSession 用 cookie 明文查会话，再确认账号仍然可用。
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

	return Principal{Kind: KindUser, UserID: user.ID, Role: user.Role}, true, nil
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

// isLocalRequest 判断请求是否直接来自本机回环。
//
// 只看 RemoteAddr：它由 HTTP 服务器填写，客户端无法伪造。转发头存在时一律
// 不算本机——反向代理常与控制面同机部署，那时所有外部请求的 RemoteAddr 都是
// 回环，若还认作本机管理员就等于取消了鉴权。地址解析失败按非本机处理，绝不
// 因格式怪异的 RemoteAddr 而 panic。
func isLocalRequest(r *http.Request) bool {
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
// Kind == user 只返回 node_members 里列出的节点：账号之间互不可见是这套节点
// 归属设计的核心承诺，角色为 admin 的普通账号也一样——全量访问必须显式使用
// admin_token 或本机直通。admin_token / local 返回全部节点：未认领的历史节点
// 本来就在 nodes 表里，是「全部节点」的子集，因此不需要再单独并一次
// ListUnclaimedNodes，也就不会因两次查询之间的写入而给出自相矛盾的结果。
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
		return nodeIDSet(nodes), nil
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
