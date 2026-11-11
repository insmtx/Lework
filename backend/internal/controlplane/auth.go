package controlplane

import (
	"crypto/subtle"
	"io"
	"net"
	"net/http"
	"strings"
)

const (
	// sessionCookieName 是浏览器登录后持有的会话 cookie 名称。
	sessionCookieName = "lework_session"

	// unauthorizedMessage 是拒绝访问时的固定正文，使用占位符而非真实令牌，
	// 避免任何响应泄漏注册令牌。
	unauthorizedMessage = "unauthorized: open /?token=<token> in a browser"
)

// authHandler 为非本机请求提供单一共享令牌鉴权；本机请求直接放行。
//
// 控制面默认只监听回环，一旦暴露到网络，节点管理、会话、文件与 PTY 接口
// 都必须先通过注册令牌校验。令牌值只参与比较与 cookie 写入，不进入日志、
// 错误信息或响应体。
type authHandler struct {
	next  http.Handler
	token string
}

// newAuthHandler 用注册令牌包装整个 HTTP 处理器。
func newAuthHandler(next http.Handler, token string) http.Handler {
	return &authHandler{next: next, token: token}
}

// ServeHTTP 按“豁免 → 本机 → 查询令牌 → cookie → Bearer”的顺序鉴权。
func (h *authHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if isAuthExemptPath(r.URL.Path) {
		h.next.ServeHTTP(w, r)
		return
	}

	// 反向代理常与浏览器同机部署，此时 RemoteAddr 是回环；只要出现转发头就
	// 说明请求可能来自外部，必须走令牌校验，避免绕过。
	if isLocalRequest(r) && !hasForwardingHeaders(r) {
		h.next.ServeHTTP(w, r)
		return
	}

	if token := r.URL.Query().Get("token"); token != "" && h.tokenMatches(token) {
		h.setSessionCookie(w, r)
		redirectWithoutToken(w, r)
		return
	}

	if cookie, err := r.Cookie(sessionCookieName); err == nil && h.tokenMatches(cookie.Value) {
		h.next.ServeHTTP(w, r)
		return
	}

	if bearer, ok := bearerToken(r.Header.Get("Authorization")); ok && h.tokenMatches(bearer) {
		h.next.ServeHTTP(w, r)
		return
	}

	h.deny(w)
}

// tokenMatches 以常量时间比较候选令牌，空令牌不参与匹配。
func (h *authHandler) tokenMatches(candidate string) bool {
	if h.token == "" || candidate == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(candidate), []byte(h.token)) == 1
}

// setSessionCookie 把已校验的令牌写入浏览器会话 cookie。
func (h *authHandler) setSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    h.token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

// deny 返回 401，正文只含固定提示，不含令牌。
func (h *authHandler) deny(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, unauthorizedMessage+"\n")
}

// isLocalRequest 判断请求是否直接来自本机回环。
//
// 只信任 RemoteAddr：它由 HTTP 服务器填写，客户端无法伪造。
func isLocalRequest(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// hasForwardingHeaders 判断请求是否携带任一代理转发头。
//
// 只看头是否存在而不看值：即使代理写入空值，也不能据此把外部请求当作本机。
func hasForwardingHeaders(r *http.Request) bool {
	for _, name := range []string{"X-Forwarded-For", "X-Real-IP", "Forwarded"} {
		if len(r.Header.Values(name)) > 0 {
			return true
		}
	}
	return false
}

// isAuthExemptPath 判断路径是否豁免鉴权，只做精确匹配以免前缀绕过。
//
// /healthz 用于探活；/api/agent/connect 由 agent 网关自行校验注册令牌。
func isAuthExemptPath(path string) bool {
	switch path {
	case "/healthz", "/api/agent/connect":
		return true
	default:
		return false
	}
}

// bearerToken 从 Authorization 头解析 Bearer 令牌，方案名大小写不敏感。
func bearerToken(header string) (string, bool) {
	scheme, value, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	return strings.TrimSpace(value), true
}

// redirectWithoutToken 种下 cookie 后跳回同一个地址，并从查询串中移除令牌，
// 同时禁止 Referer 泄漏；其余查询参数保持不变。
func redirectWithoutToken(w http.ResponseWriter, r *http.Request) {
	target := *r.URL
	query := target.Query()
	query.Del("token")
	target.RawQuery = query.Encode()

	w.Header().Set("Referrer-Policy", "no-referrer")
	http.Redirect(w, r, target.RequestURI(), http.StatusFound)
}
