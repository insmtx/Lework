package controlplane

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/insmtx/lework/backend/internal/controlplane/authz"
)

// authMiddleware 把每个请求解析成 authz.Principal 放进 context。
//
// 鉴权只在这一处发生：API handler 从 context 读身份，不再各自解释 cookie 或
// Bearer。未通过时写固定的 JSON 401，绝不回显任何凭据。
type authMiddleware struct {
	next     http.Handler
	resolver authz.Resolver
}

// newAuthMiddleware 用 authz.Resolver 包装整个 HTTP 处理器。
func newAuthMiddleware(next http.Handler, resolver authz.Resolver) http.Handler {
	return &authMiddleware{next: next, resolver: resolver}
}

// authErrorResponse 是中间件拒绝请求时的响应体。
type authErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// ServeHTTP 先放行豁免路径，再解析身份；失败区分「未认证」与「基础设施故障」。
//
// 把 500 与 401 分开是有意的：数据库抖动时回 401 会让浏览器把所有人的会话
// 当成失效并清掉 cookie，而真实原因是服务端暂时不可用。
func (m *authMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if authExemptPath(r.URL.Path) {
		m.next.ServeHTTP(w, r)
		return
	}

	principal, ok, err := m.resolver.Resolve(r)
	if err != nil {
		writeAuthError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !ok && m.firstRunLocalRequest(r) {
		// 设计 §8：本机直通只在「没有 User 表内容」或 local_admin 开启时生效。
		// Resolver 已覆盖后者，这里补前者——否则首次部署且关闭 local_admin 时
		// 连登录页都打不开初始化接口之外的本机页面。
		principal = authz.Principal{Kind: authz.KindLocal, Role: "admin"}
		ok = true
	}
	if !ok {
		writeAuthError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}

	m.refreshSessionCookie(w, r, principal)

	m.next.ServeHTTP(w, r.WithContext(authz.WithPrincipal(r.Context(), principal)))
}

// refreshSessionCookie 在请求靠会话 cookie 通过校验时重签同一个 cookie。
//
// 服务端只在 renewSession 里推后 expires_at，浏览器却按登录那一刻的 MaxAge
// 计算存活期：不重签的话，一直活跃的浏览器仍会在登录 30 天后被清掉 cookie，
// 用户被迫重新登录，滑动续期只做了一半。这里不需要从 resolver 拿「本次是否
// 真的续期了」的标志：重签是无状态的，值原样回写，MaxAge 设为 SessionTTL，
// 最坏情况是多写一个 Set-Cookie 头；若改成只在续期时重签，就得把内部时序判断
// 穿过 Resolver.Resolve 的返回值，接口为了一个响应头变复杂。
//
// 只在「请求确实带了会话 cookie 且身份是账号」时做：Bearer（移动端）没有
// cookie 罐，local/admin_token 也没有对应会话，给它们签发 cookie 会凭空造出
// 一条不存在的会话。SessionTTL<=0 表示显式关闭续期，此时也不该把登录时写入的
// 持久 cookie 改成会话 cookie。
func (m *authMiddleware) refreshSessionCookie(w http.ResponseWriter, r *http.Request, principal authz.Principal) {
	if principal.Kind != authz.KindUser || m.resolver.SessionTTL <= 0 {
		return
	}

	cookie, err := r.Cookie(m.resolver.SessionCookieName())
	if err != nil || cookie.Value == "" {
		return
	}

	authz.SetSessionCookie(w, r, cookie.Value, m.resolver.SessionTTL)
}

// firstRunLocalRequest 判断是否「本机请求且还没有任何账号」。
//
// 存储不可用时返回 false：无法确认「没有账号」就不该直通，宁可让用户登录。
func (m *authMiddleware) firstRunLocalRequest(r *http.Request) bool {
	if m.resolver.Store == nil || !authz.IsLocalRequest(r) {
		return false
	}
	count, err := m.resolver.Store.CountUsers(r.Context())
	if err != nil {
		return false
	}
	return count == 0
}

// authExemptPath 判断路径是否豁免鉴权。
//
// /api 之外的路径全部放行：登录页与静态资源必须在认证之前就能加载，否则
// 用户永远看不到登录框。 /api 下只放行显式端点——探活、agent 网关（自行校验
// 节点凭证）与三个认证入口；其余一律要求身份，新增端点默认受保护。
func authExemptPath(path string) bool {
	if !strings.HasPrefix(path, "/api/") {
		return true
	}
	switch path {
	case "/api/agent/connect", "/api/auth/methods", "/api/auth/login", "/api/auth/register":
		return true
	default:
		return false
	}
}

// writeAuthError 写出固定错误体；调用方保证 code 与 message 都是常量，
// 不拼接任何用户输入或凭据。
func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(authErrorResponse{Code: code, Message: message})
}
