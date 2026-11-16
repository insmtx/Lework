package api

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/insmtx/lework/backend/internal/controlplane/authn"
	"github.com/insmtx/lework/backend/internal/controlplane/authz"
	"github.com/insmtx/lework/backend/internal/controlplane/store"
)

const (
	// minPasswordLength 是创建账号时的密码下限。登录不做这项检查：老账号可能
	// 是更早的规则下创建的，且登录失败必须与「账号不存在」完全一致，任何额外
	// 分支都会变成账号存在性的旁路。
	minPasswordLength = 8

	// defaultSessionTTL 在配置缺失时兜底，避免签发一条立刻过期的会话。
	defaultSessionTTL = 30 * 24 * time.Hour

	// registrationModeOpen 等是 RegistrationMode 的合法取值；空值按 open
	// 处理，未知值失败关闭（拒绝注册）。
	registrationModeOpen   = "open"
	registrationModeClosed = "closed"

	// roleAdmin/roleUser/statusActive 与 store 中的取值保持一致：账号只有
	// active 状态可以登录，状态字段漏填的账号必须被拒绝而不是默认放行。
	roleAdmin    = "admin"
	roleUser     = "user"
	statusActive = "active"

	// loginAttemptsPerIdentity 是同一「邮箱 + 来源 IP」在一个窗口内允许的
	// 失败登录次数；超过后返回 429。与认领码的猜码限流同形，防止在线撞库
	// 与单点暴力破解。
	loginAttemptsPerIdentity = 10

	// registerAttemptsPerIP 是同一来源 IP 在一分钟内允许的注册次数。开放
	// 注册让这个端点是公开写接口，没有预算就等于允许批量灌账号。
	registerAttemptsPerIP = 10
	// loginAttemptWindow 是登录失败计数的时间窗，从第一次失败开始计算。
	loginAttemptWindow = time.Minute

	// dummyPasswordHash 是一个固定口令的合法 argon2id PHC 串。账号不存在时
	// 仍用它跑一次校验，让「未知账号」与「密码错误」耗时相近，避免通过响应
	// 时间判断邮箱是否已注册。
	dummyPasswordHash = "argon2id$v=19$m=65536,t=3,p=2$eaZz/Kaxr2G6f1y4k2UKJw$5q96Dm+btFE6+dWZtCccKQ04TXpO17TqZwihy/D0lQA"
)

// AuthConfig 是认证与注册接口需要的运行配置。
//
// 不直接引用 controlplane/config：api 包只关心这两个值，保持窄依赖后单测
// 不必装配整份服务配置。
type AuthConfig struct {
	// SessionTTL 是签发会话的有效期；非正时回落到 30 天。
	SessionTTL time.Duration
	// RegistrationMode 是注册策略：open / closed；空值按 open 处理。
	RegistrationMode string
}

// loginRequest 是 POST /api/auth/login 的请求体。
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// registerRequest 是 POST /api/auth/register 的请求体。
type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authUser 是账号的对外投影：只暴露界面需要的字段，密码哈希永不出现在响应里。
type authUser struct {
	ID     string `json:"id"`
	Email  string `json:"email"`
	Role   string `json:"role"`
	Status string `json:"status"`
}

// authResponse 是登录与注册成功后的响应。
//
// Token 与 cookie 里的值相同：浏览器靠 HttpOnly cookie，移动端没有 cookie 罐，
// 只能把同一个 token 放进 Authorization: Bearer。明文只在这里出现一次。
type authResponse struct {
	User  authUser `json:"user"`
	Token string   `json:"token,omitempty"`
}

// meResponse 是 GET /api/me 的响应。local 与 admin_token 没有对应账号，User 为 null。
type meResponse struct {
	Kind string    `json:"kind"`
	Role string    `json:"role"`
	User *authUser `json:"user"`
}

// requireUser 取出中间件放进 context 的身份；缺失时写 401 并返回 false。
//
// local 与 admin_token 同样算已认证：它们是部署级身份，后续节点授权会据此
// 放行全部节点。这里只回答「有没有身份」，不做节点级判断。
func (a *API) requireUser(w http.ResponseWriter, r *http.Request) (authz.Principal, bool) {
	principal, ok := authz.PrincipalFrom(r.Context())
	if !ok {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return authz.Principal{}, false
	}
	return principal, true
}

// sessionTTL 返回实际使用的会话有效期。
func (a *API) sessionTTL() time.Duration {
	if a.authCfg.SessionTTL > 0 {
		return a.authCfg.SessionTTL
	}
	return defaultSessionTTL
}

// registrationMode 返回规范化后的注册策略；空值按默认的 open 处理。
func (a *API) registrationMode() string {
	mode := strings.ToLower(strings.TrimSpace(a.authCfg.RegistrationMode))
	if mode == "" {
		return registrationModeOpen
	}
	return mode
}

// handleLogin 校验邮箱密码并签发会话。
//
// 「账号不存在」与「密码错误」走同一条拒绝路径：响应体与状态码完全一致，
// 未知账号也跑一次 argon2 校验，避免从耗时上区分两者。
//
// 失败尝试按「规范化邮箱 + 来源 IP」限流（10 次/分钟）。成功登录会归还本次
// 预占的额度；空密码、未知账号、密码错误与停用账号都保留额度，因为它们都
// 是真实的失败尝试。
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req loginRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	attemptKey := loginAttemptKey(email, r)
	if !a.loginAttempts.allow(attemptKey, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeAPIError(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts, try again later")
		return
	}

	if req.Password == "" {
		writeInvalidCredentials(w)
		return
	}

	user, found, err := a.store.UserByEmail(r.Context(), email)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !found {
		// 结果一定为 false，但这次计算让未知账号的耗时与真实账号同量级。
		_ = authn.VerifyPassword(dummyPasswordHash, req.Password)
		writeInvalidCredentials(w)
		return
	}
	// 先校验密码再看状态：短路顺序反过来会给停用账号一个可测量的快速失败。
	if !authn.VerifyPassword(user.PasswordHash, req.Password) || user.Status != statusActive {
		writeInvalidCredentials(w)
		return
	}

	body, ok := a.issueSession(w, r, user)
	if !ok {
		return
	}
	// 成功不是失败尝试：归还 allow 预占的那一格，避免合法用户被之前输错
	// 的几次挤掉额度（但不会重置已累计的失败）。
	a.loginAttempts.succeed(attemptKey)
	writeJSON(w, http.StatusOK, body)
}

// loginAttemptKey 返回登录限流的计数键：规范化邮箱 + 来源 IP。
//
// 只按邮箱计数会让任何人锁死别人的账号；只按 IP 又会让同一出口后的所有
// 用户共享预算。两者组合仍能挡住单点暴力破解，同时限制「同一账号换 IP 试」
// 的成本。
func loginAttemptKey(email string, r *http.Request) string {
	return strings.ToLower(strings.TrimSpace(email)) + "|" + requestSourceIP(r)
}

// requestSourceIP 取连接的来源 IP，用作登录失败的限流维度。
//
// 只读 r.RemoteAddr，不信任 X-Forwarded-For：本包没有「可信代理」配置，
// 直接采信可伪造的头等于让攻击者自带无限预算。代价是反向代理后所有客户端
// 共享代理 IP，此时预算退化为「每邮箱每分钟 10 次」——邮箱那半个键仍然
// 生效，不会让单个客户端无限尝试。
func requestSourceIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// authMethodsResponse 是登录页判断「显示什么按钮」的最小信息集。
//
// 只暴露两项：有没有账号、注册是否开放。不返回账号名、节点数或版本——登录页
// 在鉴权之前可访问，任何多余字段都是给陌生人的情报。
type authMethodsResponse struct {
	Initialized      bool   `json:"initialized"`
	RegistrationMode string `json:"registration_mode"`
}

// handleAuthMethods 返回登录页所需的公开信息。
func (a *API) handleAuthMethods(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	count, err := a.store.CountUsers(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	writeJSON(w, http.StatusOK, authMethodsResponse{
		Initialized:      count > 0,
		RegistrationMode: a.registrationMode(),
	})
}

// handleRegister 按注册策略创建账号并直接登录。
//
// open 模式下按来源 IP 限流：注册是公开写接口，成功也必须计数，否则批量
// 注册不受任何约束。closed 与未知策略一律拒绝（未知值失败关闭，避免配置
// 写错时意外开放注册）。
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req registerRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	mode := a.registrationMode()
	switch mode {
	case registrationModeOpen:
	case registrationModeClosed:
		writeAPIError(w, http.StatusForbidden, "registration_closed", "registration is closed")
		return
	default:
		// 未知策略失败关闭：配置写错时不能意外开放注册。
		writeAPIError(w, http.StatusForbidden, "registration_closed", "registration is closed")
		return
	}

	email, ok := validateNewAccount(req.Email, req.Password)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	// 限流在解码、模式与输入校验之后：坏请求不算一次注册尝试。成功后不
	// 调用 succeed——开放注册的成本就在账号本身，成功必须占预算。
	ip := requestSourceIP(r)
	if !a.registerAttempts.allow(ip, time.Now()) {
		w.Header().Set("Retry-After", "60")
		writeAPIError(w, http.StatusTooManyRequests, "too_many_attempts", "too many attempts, try again later")
		return
	}

	hash, err := authn.HashPassword(req.Password)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	if _, found, err := a.store.UserByEmail(r.Context(), email); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	} else if found {
		writeAPIError(w, http.StatusConflict, "email_taken", "email already registered")
		return
	}

	user, err := a.store.CreateUser(r.Context(), store.User{
		Email:        email,
		PasswordHash: hash,
		Role:         roleUser,
		Status:       statusActive,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	body, ok := a.issueSession(w, r, user)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, body)
}

// passwordRequest 是 POST /api/auth/password 的请求体。
type passwordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// handleChangePassword 校验旧密码后覆盖密码，并吊销其他设备的会话。
//
// 默认管理员密码是刻意的开箱配置；没有改密码路径，公网部署就等于人人管理员。
// 这里不要求邮箱：改的是「当前会话所属账号」，要求邮箱只会让攻击者多一个
// 枚举入口。
func (a *API) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if principal.Kind != authz.KindUser || principal.UserID == "" {
		writeAPIError(w, http.StatusForbidden, "user_required", "password change requires an account")
		return
	}

	var req passwordRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	if len(req.NewPassword) < minPasswordLength {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	user, found, err := a.store.UserByID(r.Context(), principal.UserID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !found {
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	if !authn.VerifyPassword(user.PasswordHash, req.CurrentPassword) {
		writeInvalidCredentials(w)
		return
	}

	hash, err := authn.HashPassword(req.NewPassword)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if err := a.store.UpdateUserPassword(r.Context(), user.ID, hash); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if err := a.store.DeleteOtherUserSessions(r.Context(), user.ID, principal.SessionID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout 吊销当前会话并清掉浏览器 cookie。
//
// 同时注册为 POST /api/auth/logout（任务书与前端使用）与
// DELETE /api/auth/session（设计 §8 的写法）：两个入口共享同一实现，文档与
// 客户端不必为了一个路径名互相迁就，也不会出现「按设计文档实现返回 404」。
//
// 只删本次请求使用的那条会话：清空该账号全部会话会把手机端一起踢下线，
// 与用户点「退出登录」的预期不符。local / admin_token 没有会话行，只清 cookie。
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}
	if principal.Kind == authz.KindUser && principal.SessionID != "" {
		if err := a.store.DeleteSession(r.Context(), principal.SessionID); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
	}

	authz.ClearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleMe 返回当前身份；有账号时带上账号记录。
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	principal, ok := a.requireUser(w, r)
	if !ok {
		return
	}

	if principal.Kind != authz.KindUser {
		if !principal.IsAdmin() {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		writeJSON(w, http.StatusOK, meResponse{Kind: principal.Kind, Role: roleAdmin})
		return
	}

	user, found, err := a.store.UserByID(r.Context(), principal.UserID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if !found {
		// 会话有效但账号在请求之间被删除：按未认证处理，让客户端回到登录页。
		writeAPIError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
		return
	}
	projected := toAuthUser(user)
	writeJSON(w, http.StatusOK, meResponse{Kind: authz.KindUser, Role: user.Role, User: &projected})
}

// issueSession 生成会话令牌、落库哈希并写 cookie。
//
// 只把哈希交给存储；明文仅存在于 cookie 与响应体这两个即将离开服务端的载体。
func (a *API) issueSession(w http.ResponseWriter, r *http.Request, user store.User) (authResponse, bool) {
	raw, hash, err := authn.NewToken()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return authResponse{}, false
	}
	if _, err := a.store.CreateSession(r.Context(), user.ID, hash, r.UserAgent(), a.sessionTTL()); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return authResponse{}, false
	}

	authz.SetSessionCookie(w, r, raw, a.sessionTTL())
	return authResponse{User: toAuthUser(user), Token: raw}, true
}

// toAuthUser 把存储记录投影成对外结构，逐字段列出而不是直接编码 store.User，
// 避免将来新增敏感列时被自动带进响应。
func toAuthUser(user store.User) authUser {
	return authUser{
		ID:     user.ID,
		Email:  user.Email,
		Role:   user.Role,
		Status: user.Status,
	}
}

// validateNewAccount 校验创建账号的输入，返回去掉首尾空格的邮箱。
//
// 只在创建账号时使用；登录不校验长度，理由见 minPasswordLength。
func validateNewAccount(email, password string) (string, bool) {
	trimmed := strings.TrimSpace(email)
	if trimmed == "" {
		return "", false
	}
	if len(password) < minPasswordLength {
		return "", false
	}
	return trimmed, true
}

// writeInvalidCredentials 写出固定的登录失败响应；坏密码与未知账号共用它。
func writeInvalidCredentials(w http.ResponseWriter) {
	writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "invalid_credentials")
}
