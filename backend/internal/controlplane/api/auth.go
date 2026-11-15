package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

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

	// registrationModeInvite 等是 RegistrationMode 的合法取值；空值按 invite
	// 处理，未知值失败关闭（拒绝注册）。
	registrationModeInvite = "invite"
	registrationModeOpen   = "open"
	registrationModeClosed = "closed"

	// roleAdmin/roleUser/statusActive 与 store 中的取值保持一致：账号只有
	// active 状态可以登录，状态字段漏填的账号必须被拒绝而不是默认放行。
	roleAdmin    = "admin"
	roleUser     = "user"
	statusActive = "active"

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
	// RegistrationMode 是注册策略：invite / open / closed；空值按 invite 处理。
	RegistrationMode string
}

// loginRequest 是 POST /api/auth/login 的请求体。
type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// bootstrapRequest 是 POST /api/auth/bootstrap 的请求体。
type bootstrapRequest struct {
	Token    string `json:"token"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// registerRequest 是 POST /api/auth/register 的请求体。
type registerRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	InviteCode string `json:"invite_code"`
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

// registrationMode 返回规范化后的注册策略；空值按默认的 invite 处理。
func (a *API) registrationMode() string {
	mode := strings.ToLower(strings.TrimSpace(a.authCfg.RegistrationMode))
	if mode == "" {
		return registrationModeInvite
	}
	return mode
}

// handleLogin 校验邮箱密码并签发会话。
//
// 「账号不存在」与「密码错误」走同一条拒绝路径：响应体与状态码完全一致，
// 未知账号也跑一次 argon2 校验，避免从耗时上区分两者。
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
	writeJSON(w, http.StatusOK, body)
}

// handleBootstrap 用 enrollment token 创建第一个管理员。
//
// 只允许在没有任何账号时调用：否则任何拿到令牌的人都能再造一个 admin。
// 已初始化时先返回 409，连令牌都不比较，避免把初始化状态变成令牌校验旁路。
func (a *API) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req bootstrapRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}
	email, ok := validateNewAccount(req.Email, req.Password)
	if !ok {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	count, err := a.store.CountUsers(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	if count > 0 {
		writeAPIError(w, http.StatusConflict, "already_initialized", "already initialized")
		return
	}
	if !enrollmentTokenMatches(req.Token, a.resolver.EnrollmentToken) {
		writeAPIError(w, http.StatusUnauthorized, "invalid_token", "invalid_token")
		return
	}

	hash, err := authn.HashPassword(req.Password)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}
	user, err := a.store.CreateUser(r.Context(), store.User{
		Email:        email,
		PasswordHash: hash,
		Role:         roleAdmin,
		Status:       statusActive,
	})
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	// 初始化不需要登录态；客户端随后用同一组邮箱密码登录即可。
	writeJSON(w, http.StatusOK, authResponse{User: toAuthUser(user)})
}

// handleRegister 按注册策略创建账号并直接登录。
//
// invite 模式下先消费邀请码、再查重与建号：反过来的顺序会在消费失败时留下
// 一个「没被授权却已存在」的账号，而且会让无效邀请码也能通过 409/403 的差异
// 探测邮箱是否已注册。代价是合法邀请码遇上重复邮箱会被标记使用（需要补发），
// 以及建号失败时邀请码已被消费——这些失败只可能来自并发重复邮箱或数据库
// 故障，不影响安全，只是需要人工补发邀请码。
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")

	var req registerRequest
	if err := decodeRequest(w, r, &req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_request", "invalid request")
		return
	}

	mode := a.registrationMode()
	switch mode {
	case registrationModeInvite, registrationModeOpen:
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
	inviteCode := strings.TrimSpace(req.InviteCode)
	if mode == registrationModeInvite && inviteCode == "" {
		writeAPIError(w, http.StatusForbidden, "invalid_invite", "invalid invite")
		return
	}

	hash, err := authn.HashPassword(req.Password)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	}

	// 预生成用户 ID：ConsumeInvite 需要未来用户 ID，先消费再建号才能保证
	// 「邀请码已用」一定对应一个真实账号。
	userID := uuid.NewString()
	if mode == registrationModeInvite {
		consumed, err := a.store.ConsumeInvite(r.Context(), authn.HashToken(inviteCode), userID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
			return
		}
		if !consumed {
			writeAPIError(w, http.StatusForbidden, "invalid_invite", "invalid invite")
			return
		}
	}

	// 查重放在邀请码校验之后：否则任何人拿一个无效邀请码就能靠 409 与 403
	// 的差异枚举已注册邮箱。代价是「邀请码有效但邮箱已占用」会消耗掉邀请码，
	// 需要管理员补发——宁可让邀请码作废，也不能泄漏账号存在性。
	if _, found, err := a.store.UserByEmail(r.Context(), email); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "internal error")
		return
	} else if found {
		writeAPIError(w, http.StatusConflict, "email_taken", "email already registered")
		return
	}

	user, err := a.store.CreateUser(r.Context(), store.User{
		ID:           userID,
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

	clearSessionCookie(w, r)
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

	http.SetCookie(w, &http.Cookie{
		Name:     authz.DefaultCookieName,
		Value:    raw,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(a.sessionTTL().Seconds()),
	})
	return authResponse{User: toAuthUser(user), Token: raw}, true
}

// clearSessionCookie 让浏览器立即丢弃会话 cookie。
func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     authz.DefaultCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
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

// enrollmentTokenMatches 以恒定时间比较提交的令牌与 enrollment token。
//
// 两侧先各自取 sha256：长度不同时直接比较会立刻返回，攻击者能据此试探长度，
// 定长摘要把比较收敛成「是否相等」。空令牌不参与比较。
func enrollmentTokenMatches(candidate, want string) bool {
	if want == "" || candidate == "" {
		return false
	}
	got := sha256.Sum256([]byte(candidate))
	expected := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(got[:], expected[:]) == 1
}

// writeInvalidCredentials 写出固定的登录失败响应；坏密码与未知账号共用它。
func writeInvalidCredentials(w http.ResponseWriter) {
	writeAPIError(w, http.StatusUnauthorized, "invalid_credentials", "invalid_credentials")
}
