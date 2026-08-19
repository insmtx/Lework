# 账号认证体系收敛技术方案

> 设计日期：2026-07-09
> 涉及项目：Lework、identity-platform (iam)
> 状态：v2（根据 review 修订）

---

## 1. 背景与目标

### 1.1 现状

Lework、roc、identity-platform 三个项目各有独立的账号认证体系：

- **identity-platform**（`corekg/identity-platform`） — 从 roc 剥离的精简身份平台，提供密码/微信/企微登录 + JWT 签发 + 组织管理
- **Lework**（`corekg/Lework-fork`） — AI 自动化平台，自实现邮箱/手机/Worker Token 认证
- **roc**（`corekg/roc`） — 功能最全的业务平台，含 RBAC/教育/实名认证（本次改造不涉及）

三项目的核心差异：

| 维度 | identity-platform | Lework |
|------|-------------------|--------|
| 用户身份标识 | `UIN = UserIdentification.ID`（一个用户可有多 UIN） | `Uin = UserOrg.ID`（用户在每个组织下一个 Uin） |
| JWT claims | `{Uin, Issuer, IssuedAt, ExpiresAt, LoginWay, Audience}`，json tag 为 `c/t/e/i/a/l` | `{Uin}` + StandardClaims，json tag 为 `uin` |
| JWT Secret | 按 `Issuer`（域名）隔离，走 yg-go 配置中心 | 单一全局 Secret |
| 登录请求 | 要求 `domain_name`/`issuer` 区分租户 | 只有 `{email, password, org_id?}` |
| 组织模型 | `Company`（多租户，含版本/配额/认证） | `Organization`（轻量级） |

### 1.2 目标

1. **SaaS 版本**：Lework 将账号认证委托给 identity-platform，统一认证入口
2. **开源版本**：Lework 保留内置认证实现，可独立部署运行
3. **Adapter 模式**：通过编译时选择（build tags）区分两套实现，共享同一份接口契约

### 1.3 约束

- identity-platform 和 Lework 为**两个独立仓库**，各自演进
- 两服务间通过 **HTTP API** 调用
- Lework **直接验证 identity-platform 签发的 JWT**，不做二次签发
- **有存量用户**需要在线迁移
- Lework `AGENTS.md` 禁止 `map[string]interface{}` 传递业务数据 —— adapter 必须使用强类型 struct

---

## 2. 总体架构

### 2.1 组件关系

```
┌─────────────────────────────────────────────────────────────────┐
│                        Lework                                    │
│                                                                  │
│  ┌─────────────┐    ┌──────────────────────────────────────┐    │
│  │   Router    │───▶│      adapter.NewAuthService()         │    │
│  │             │    │      (factory, build tag 切换)        │    │
│  └─────────────┘    └──────────┬───────────────────────────┘    │
│                                │                                 │
│                                ▼ contract.AuthService            │
│                 ┌──────────────┴──────────────┐                  │
│                 ▼                              ▼                 │
│        ┌────────────────┐          ┌──────────────────┐         │
│        │ BuiltinAdapter │          │ IdentityAdapter   │         │
│        │ (!saas tag)    │          │ (saas tag)        │         │
│        └────────────────┘          └────────┬─────────┘         │
│                                              │                   │
│  ┌─────────────┐    ┌──────────────────┐    │                   │
│  │ Middleware  │───▶│  TokenParser     │◀───┘                   │
│  │             │    │  (factory, tag)  │                        │
│  └─────────────┘    └──────────────────┘                        │
│       开源版：无外部依赖          SaaS 版：HTTP 调用             │
└──────────────────────────────────────────────┼───────────────────┘
                                               │ HTTP
                                               ▼
┌──────────────────────────────────────────────────────────────────┐
│                    identity-platform                             │
│                                                                  │
│  ┌──────────────┐    ┌──────────────────┐    ┌───────────────┐  │
│  │ Auth APIs    │───▶│  AccountService  │───▶│  MySQL/Redis  │  │
│  │ (Gin)        │    │  (业务逻辑)       │    │               │  │
│  └──────────────┘    └──────────────────┘    └───────────────┘  │
│                                                                  │
│  新增 API: RegisterByEmail, SendPhoneCode, LoginByPhoneCode      │
│  新增 API: RefreshToken 扩展, ImportUser                         │
└──────────────────────────────────────────────────────────────────┘
```

### 2.2 构建时选择机制

Lework 当前 `router.go:79` 写死了 `service.NewAuthServiceWithProvisioning`，无法通过 build tag 切换。修正方案是引入 **adapter factory 层**：

```go
// adapter/factory.go
//go:build !saas
package adapter

import (
    "github.com/insmtx/Leros/backend/internal/adapter/builtin"
    "github.com/insmtx/Leros/backend/internal/api/contract"
)

type Deps struct {
    DB                 *gorm.DB
    Config             config.Config
    WorkerProvisioning *service.WorkerProvisioningService
}

// NewAuthService 返回开源版的 AuthService 实现
func NewAuthService(deps Deps) contract.AuthService {
    return builtin.NewAuthService(deps.DB, deps.Config, deps.WorkerProvisioning)
}

// NewTokenParser 返回开源版的 TokenParser 实现
func NewTokenParser(deps Deps) middleware.TokenParser {
    return builtin.NewTokenParser(deps.DB, deps.Config.Server.JWT.Secret)
}
```

```go
// adapter/factory.go
//go:build saas
package adapter

import (
    "github.com/insmtx/Leros/backend/internal/adapter/identity"
    "github.com/insmtx/Leros/backend/internal/api/contract"
)

// NewAuthService 返回 SaaS 版的 AuthService 实现
func NewAuthService(deps Deps) contract.AuthService {
    return identity.NewAuthService(deps.DB, deps.Config, deps.WorkerProvisioning)
}

// NewTokenParser 返回 SaaS 版的 TokenParser 实现
func NewTokenParser(deps Deps) middleware.TokenParser {
    return identity.NewTokenParser(deps.DB, deps.Config)
}
```

**Router 改造**（`backend/internal/api/router.go`）：

```go
// 改造前
authService := service.NewAuthServiceWithProvisioning(db, cfg.Server.JWT.Secret, cfg.Aliyun, workerProvisioningService)
handler.RegisterAuthRoutes(v1, authService)

// 改造后
deps := adapter.Deps{DB: db, Config: cfg, WorkerProvisioning: workerProvisioningService}
authService := adapter.NewAuthService(deps)
handler.RegisterAuthRoutes(v1, authService)
```

**中间件改造**（`backend/internal/api/router.go:40`）：

```go
// 改造前
r.Use(middleware.CallerMiddleware(cfg.Server.JWT.Secret, db))

// 改造后
tokenParser := adapter.NewTokenParser(deps)
r.Use(middleware.CallerMiddleware(tokenParser, db))
```

### 2.3 构建命令

```bash
# 开源版
go build -o ./bundles/leros ./backend/cmd/leros/

# SaaS 版
go build -tags saas -o ./bundles/leros ./backend/cmd/leros/
```

### 2.4 配置结构

```yaml
# config.yaml
auth:
  # 软切换开关，用于生产环境灰度切换（LEROS_DEV 不应作为生产回滚）
  mode: "builtin"  # builtin | identity
  identity_platform:
    base_url: "https://iam.example.com/v5"
    api_key: "..."                  # 服务间调用凭证
    jwt_secret: "..."               # 验证 identity-platform 签发的 JWT
    issuer: "lework"                # 在 identity-platform 中注册的租户标识
    domain_name: "lework.example.com"  # identity LoginByPassword 必填字段
    timeout: "5s"                   # HTTP 调用超时
```

**说明**：
- `auth.mode` 仅用于运行时灰度切换和日志标记，实际 adapter 的选择由 build tag 决定
- `issuer` 和 `domain_name` 是 identity-platform 框架的强制要求，SaaS 版 Lework 作为一个租户在 identity-platform 中注册
- SaaS 版二进制中 `auth.identity_platform` 为必填，开源版该字段被忽略

---

## 3. Lework 侧改造

### 3.1 AuthService 接口（不变）

`backend/internal/api/contract/auth.go` 保持现有接口不变，两套 adapter 都实现同一接口：

```go
type AuthService interface {
    RegisterByEmail(ctx context.Context, req *RegisterByEmailRequest) (*AuthTokenResponse, error)
    LoginByEmail(ctx context.Context, req *LoginByEmailRequest) (*AuthTokenResponse, error)
    SendPhoneLoginCode(ctx context.Context, req *SendPhoneLoginCodeRequest) (*SendPhoneLoginCodeResponse, error)
    LoginByPhoneCode(ctx context.Context, req *LoginByPhoneCodeRequest) (*AuthTokenResponse, error)
    RefreshToken(ctx context.Context, req *RefreshTokenRequest) (*AuthTokenResponse, error)
    SwitchOrganization(ctx context.Context, req *SwitchOrganizationRequest) (*AuthTokenResponse, error)
    CreateOrganization(ctx context.Context, req *CreateOrganizationRequest) (*AuthTokenResponse, error)
    AuthSession(ctx context.Context) (*AuthSessionResponse, error)
}
```

### 3.2 目录结构调整

```
backend/internal/
├── api/
│   ├── contract/
│   │   ├── auth.go          # AuthService 接口（不变）
│   │   └── auth_type.go     # 请求/响应类型（不变）
│   └── handler/
│       └── auth_handler.go  # Handler（不变，依赖接口）
├── adapter/                 # 新增
│   ├── factory.go           # !saas build tag — 工厂函数
│   ├── factory.go           # saas build tag — 工厂函数
│   ├── deps.go              # 共享的 Deps 结构体（无 build tag）
│   ├── builtin/
│   │   ├── auth.go          # 开源版实现（从 service/auth_service.go 迁来）
│   │   └── token_parser.go  # 开源版 TokenParser
│   └── identity/
│       ├── auth.go          # SaaS 版 AuthService 实现
│       ├── token_parser.go  # SaaS 版 TokenParser
│       ├── client.go        # identity-platform HTTP 客户端
│       ├── claims.go        # identity JWT claims 结构体
│       └── mapper.go        # 用户/组织数据映射
├── service/                 # auth_service.go 迁移到 adapter/builtin/
├── middleware/
│   ├── identify.go          # 改造：接收 TokenParser 接口
│   └── token_parser.go      # 新增 TokenParser 接口定义
└── ...
```

### 3.3 TokenParser 接口抽象

当前 `middleware/identify.go` 的 `CallerMiddleware` 只接收一个 `jwtSecret`，SaaS 模式下需要同时支持 identity JWT（identity Secret）和 Lework Worker Token（Lework Secret）。

**修正方案**：引入 `TokenParser` 接口，中间件对认证方式无感知：

```go
// middleware/token_parser.go
package middleware

import "github.com/insmtx/Leros/backend/types"

// TokenParser 抽象 JWT 解析逻辑，由 adapter factory 按 build tag 注入实现
type TokenParser interface {
    // ParseUser 解析用户 token，返回 Caller（可能为 failed 状态）
    ParseUser(ctx context.Context, tokenStr string) (*types.Caller, error)
    // ParseWorker 解析 worker token
    ParseWorker(ctx context.Context, tokenStr string) (*types.Caller, error)
}
```

**中间件改造**（`middleware/identify.go`）：

```go
// 改造前
func CallerMiddleware(jwtSecret string, database *gorm.DB) gin.HandlerFunc {
    return func(ctx *gin.Context) {
        caller := parseCallerFromRequest(ctx, jwtSecret, database, reqID)
    }
}

// 改造后
func CallerMiddleware(parser TokenParser, database *gorm.DB) gin.HandlerFunc {
    return func(ctx *gin.Context) {
        // ... request ID / trace ID 逻辑不变

        // LEROS_DEV 模式保持不变（仅开发环境）
        if os.Getenv("LEROS_DEV") == "true" {
            return // 固定身份
        }

        authHeader := ctx.Request.Header.Get("Authorization")
        if authHeader == "" {
            return // 匿名
        }
        tokenStr := extractTokenFromHeader(authHeader)

        // 优先尝试 User Token 解析
        userCaller, err := parser.ParseUser(ctx.Request.Context(), tokenStr)
        if err == nil && userCaller.State == types.AuthStateSucc {
            injectCaller(ctx, userCaller)
            return
        }

        // 回退到 Worker Token
        workerCaller, err := parser.ParseWorker(ctx.Request.Context(), tokenStr)
        if err == nil && workerCaller.State == types.AuthStateSucc {
            injectCaller(ctx, workerCaller)
            return
        }

        injectCaller(ctx, failedCaller())
    }
}
```

**开源版 TokenParser**（`adapter/builtin/token_parser.go`）：

```go
//go:build !saas
package builtin

type builtinTokenParser struct {
    db        *gorm.DB
    jwtSecret string
}

func (p *builtinTokenParser) ParseUser(ctx context.Context, tokenStr string) (*types.Caller, error) {
    claims, err := localauth.ParseUserToken(tokenStr, p.jwtSecret)
    if err != nil {
        return nil, err
    }
    userOrg, err := db.GetUserOrgByUin(ctx, p.db, claims.Uin)
    // ... 原有逻辑
}
```

**SaaS 版 TokenParser**（`adapter/identity/token_parser.go`）：

```go
//go:build saas
package identity

type identityTokenParser struct {
    db            *gorm.DB
    identityCfg   config.IdentityPlatformConfig
    workerSecret  string  // Lework 自己的 Worker Secret
}

func (p *identityTokenParser) ParseUser(ctx context.Context, tokenStr string) (*types.Caller, error) {
    // 用 identity claims 结构解析（不是 Lework UserClaims）
    claims, err := parseIdentityJWT(tokenStr, p.identityCfg.JWTSecret)
    if err != nil {
        return nil, err
    }
    // claims.Uin 是 identity 的 UIN ID，不是 Lework 的 Uin
    // 必须通过 ExternalUin 查本地映射
    userOrg, err := db.GetUserOrgByExternalUin(ctx, p.db, claims.Uin)
    if err != nil || userOrg == nil {
        return &types.Caller{State: types.AuthStateFailed}, nil
    }
    return &types.Caller{
        Uin:   userOrg.Uin,      // Lework 的 Uin（本地）
        OrgID: userOrg.OrgID,    // Lework 的 OrgID（本地）
        Kind:  types.CallerKindUser,
        State: types.AuthStateSucc,
    }, nil
}

func (p *identityTokenParser) ParseWorker(ctx context.Context, tokenStr string) (*types.Caller, error) {
    // Worker Token 仍用 Lework Secret 验证
    claims, err := localauth.ParseWorkerToken(tokenStr, p.workerSecret)
    // ... 原有逻辑
}
```

### 3.4 身份解析时序（关键 — 避免语义混淆）

**identity UIN ID 和 Lework Uin 是完全不同的概念**，必须通过映射表关联，不能直接使用。

```
┌─────────────────────────────────────────────────────────────────┐
│ 完整身份解析链路（SaaS 模式）                                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                  │
│  1. 客户端请求携带 Authorization: Bearer <identity-jwt>         │
│                                                                  │
│  2. TokenParser.ParseUser()                                      │
│     ├── 用 identity Secret + identity claims 解析 JWT           │
│     │   claims.Uin = 42  ← 这是 identity-platform 的 UIN ID     │
│     │                                                           │
│     3. db.GetUserOrgByExternalUin(external_uin=42)              │
│        ├── 查询 leros_user_org 表 WHERE external_uin = 42       │
│        └── 返回 UserOrg{Uin: 7, OrgID: 3}  ← Lework 本地标识    │
│                                                                  │
│     4. 返回 Caller{                                              │
│            Uin:   7,    ← Lework Uin（来自 UserOrg.Uin）         │
│            OrgID: 3,    ← Lework OrgID（来自 UserOrg.OrgID）     │
│            Kind:  user,                                          │
│            State: succ,                                          │
│        }                                                         │
│                                                                  │
│  5. 业务层通过 auth.FromContext(ctx) 获取 Caller                │
│     └── Caller.Uin=7, Caller.OrgID=3  ← 全部是 Lework 本地值     │
│                                                                  │
└─────────────────────────────────────────────────────────────────┘
```

**关键约定**：
- `identity claims.Uin` → `UserOrg.ExternalUin`（映射用，不直接使用）
- `UserOrg.Uin` → `Caller.Uin`（Lework 业务层使用的身份标识）
- 任何时候业务层看到的 `Caller.Uin` 都是 Lework 本地值，不是 identity 的 UIN ID

### 3.5 SaaS Adapter 核心逻辑

#### 3.5.1 JWT claims 适配

identity-platform 的 JWT claims 使用 yg-go 框架定义的 `UserClaims`，json tag 为短命名：

```go
// adapter/identity/claims.go
package identity

// identityClaims 对应 identity-platform 的 JWT 结构
// json tag 与 yg-goapis/runtime/auth/claims.go 完全一致
type identityClaims struct {
    Uin       uint   `json:"c,omitempty"`      // identity UIN ID
    IssuedAt  int64  `json:"t,omitempty"`
    ExpiresAt int64  `json:"e,omitempty"`
    Issuer    string `json:"i,omitempty"`
    Audience  string `json:"a,omitempty"`
    LoginWay  int    `json:"l,omitempty"`
}

func parseIdentityJWT(tokenStr string, secret string) (*identityClaims, error) {
    claims := &identityClaims{}
    token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
        }
        return []byte(secret), nil
    })
    if err != nil {
        return nil, err
    }
    if !token.Valid {
        return nil, errors.New("invalid identity token")
    }
    return claims, nil
}
```

**重要**：不能用 Lework 的 `localauth.ParseUserToken` 解析 identity JWT —— 两者的 claims 结构、json tag、校验逻辑都不同。

#### 3.5.2 注册/登录流程

```
客户端 → Lework: POST /v1/LoginByEmail {email, password}
  ↓
IdentityAdapter.LoginByEmail()
  ├── 构造 identity 请求（自动注入 domain_name/issuer）
  ├── Lework → identity: POST /v5/account.LoginByPassword
  │     {account: email, password, domain_name: cfg.IdentityPlatform.DomainName}
  ↓
identity → Lework: {jwt_token, user_info, uin[]}
  ↓
IdentityAdapter 处理响应：
  ├── 验证 JWT 签名（用 identity claims 结构 + identity Secret）
  ├── 从 claims.Uin 获取 identity UIN ID
  ├── 查询/创建本地映射：
  │     ├── User 表：通过 email 查找，找到则更新 ExternalID
  │     ├── UserOrg 表：通过 ExternalUin 查找，未找到则创建
  │     └── Organization 表：如需要同步创建
  └── 组装 Lework 响应（透传 identity JWT）
  ↓
Lework → 客户端: {jwt_token, user_info, org}
```

**关键点**：
- Lework 直接透传 identity-platform 的 JWT 给客户端，不自己签发
- Adapter 调用 identity API 时自动注入 `domain_name` 和 `issuer`，客户端无感
- 本地只保留用户映射表，不存密码

#### 3.5.3 用户数据映射

| identity-platform 返回 | Lework 本地字段 | 说明 |
|-----------------------|----------------|------|
| `user_info.id` | `User.ExternalID` | identity User.ID |
| `user_info.name/email/phone/avatar_url` | `User.Name/Email/Phone/AvatarURL` | 透传 |
| `uin[].uin.ID` | `UserOrg.ExternalUin` | identity UIN ID（映射用） |
| `company.id` | `Organization.ExternalCompanyID` | identity Company.ID |

**User 表新增字段**：

```go
type User struct {
    gorm.Model
    ExternalID     uint   `gorm:"index"`  // identity-platform 的 User.ID
    PublicID    string
    // 其余字段不变...
}
```

**UserOrg 表新增字段**：

```go
type UserOrg struct {
    gorm.Model
    ExternalUin uint  `gorm:"index"`  // identity-platform 的 UIN.ID（映射用）
    Uin         uint                  // Lework 本地 Uin（业务层使用）
    UserID      uint
    OrgID       uint
}
```

**Organization 表新增字段**：

```go
type Organization struct {
    gorm.Model
    ExternalCompanyID uint  `gorm:"index"`  // identity-platform 的 Company.ID
    // 其余字段不变...
}
```

### 3.6 Worker Token

Worker Token 是 Lework 独有的概念，identity-platform 没有对应功能。

**SaaS 模式下 Worker Token 仍由 Lework 自己签发**，使用 Lework 配置中的 Worker Secret（不是 identity-platform 的 Secret）。原因：
- Worker 认证不依赖于用户认证体系，属于 Lework 内部概念
- 避免在 identity-platform 增加不必要的 Worker Token 抽象

```go
func (s *IdentityAdapter) IssueWorkerToken(ctx context.Context, req *IssueWorkerTokenRequest) (*AuthTokenResponse, error) {
    // 与 builtin 版相同的 Worker Token 签发逻辑
    // 使用 Lework 配置中的 WorkerSecret
}
```

**Token 区分策略**：中间件通过 claims 结构差异区分 token 类型，而非无脑 fallback：
- identity JWT 的 json tag 是 `c/t/e/i/a/l`（短命名）
- Lework Worker Token 的 json tag 是标准 `jwt.StandardClaims`

---

## 4. Handler 层

Handler 层完全不需要改动。`AuthHandler` 依赖的是 `contract.AuthService` 接口，两套 adapter 的切换由 router 中的 `adapter.NewAuthService()` 调用决定，handler 代码对 adapter 无感知。

---

## 5. identity-platform 侧扩展

### 5.1 新增 API 清单

| API | 说明 | 优先级 |
|-----|------|--------|
| `account.RegisterByEmail` | 邮箱密码注册 | P0 |
| `account.SendPhoneCode` | 发送手机验证码 | P0 |
| `account.LoginByPhoneCode` | 手机验证码登录（含自动注册） | P0 |
| `account.RefreshToken` 扩展 | 支持 JWT 续期（非 Redis 5min） | P0 |
| `account.ImportUser` | 存量用户导入（绕过前端加密，直接写入 bcrypt 哈希） | P1 |
| `account.GetUserByExternal` | 外部系统查询用户 | P1 |

### 5.2 RegisterByEmail

```go
type RegisterByEmailRequest struct {
    Email    string `json:"email"`
    Password string `json:"password"`  // 前端加密传输
    Name     string `json:"name"`
}
```

业务逻辑：校验邮箱格式 + 密码强度 → 检查邮箱唯一性 → bcrypt 哈希密码 → 创建 User + UserIdentification（个人 UIN）→ 签发 JWT → 关联默认组织。

### 5.3 SendPhoneCode / LoginByPhoneCode

```go
type SendPhoneCodeRequest struct {
    Phone string `json:"phone"`
}

type LoginByPhoneCodeRequest struct {
    Phone string `json:"phone"`
    Code  string `json:"code"`
}
```

**SendPhoneCode**：校验手机号 → 检查重发间隔 → 生成验证码 → 阿里云 SMS 发送（dev 模式默认码）→ SHA256 哈希存储。

**LoginByPhoneCode**：验证码比对 → 标记已使用 → 用户不存在时自动注册 → 签发 JWT。

### 5.4 RefreshToken 扩展

当前 identity-platform 的 Refresh Token 仅用于多 UIN 选择（Redis 5min）。需要扩展为 DB 级长周期：

- 新增 `refresh_token` 数据库表（SHA256 哈希，7 天过期）
- 单次消费，消费后立即失效
- 签发新 JWT + 新 Refresh Token

### 5.5 ImportUser（存量迁移专用）

identity-platform 的注册流程会对密码做前端加密 + `DecryptMD` 中间件解密。存量迁移时已有 bcrypt 哈希，不走这个流程。需要提供专门的导入接口：

```go
type ImportUserRequest struct {
    Email          string  `json:"email"`
    Phone          *string `json:"phone"`
    Name           string  `json:"name"`
    PasswordHash   string  `json:"password_hash"`  // 直接传入 bcrypt 哈希
    AvatarURL      string  `json:"avatar_url"`
}

type ImportUserResponse struct {
    UserID  uint   `json:"user_id"`
    UinID   uint   `json:"uin_id"`   // 创建的 UserIdentification.ID
}
```

业务逻辑：校验邮箱唯一性 → 直接写入 `user.password` 字段（跳过 bcrypt 重新哈希）→ 创建 UserIdentification → 返回 UIN ID。不签发 JWT（由 Lework 控制后续流程）。

---

## 6. 存量用户迁移

### 6.1 迁移策略：双写 + 逐步切换

```
Phase 1: 双写
  用户注册/信息更新 → 同时写入 Lework 本地表 + identity-platform
  用户登录 → 仍使用 Lework 本地认证
  存量密码 → 通过 ImportUser 接口批量导入 bcrypt 哈希

Phase 2: 数据校验
  定时任务对比两个系统数据，标记不一致记录

Phase 3: 切换认证源（灰度）
  通过 auth.mode 配置切换，新登录走 identity-platform
  存量用户通过首次登录触发自动建立 ExternalUin 映射

Phase 4: 下线本地认证（仅 SaaS）
  清理本地密码字段（保留映射表）
```

### 6.2 自动迁移流程

用户首次通过 identity-platform 登录时：

```
Lework 调 identity-platform 认证成功
→ 查询 Lework 本地 User 表（通过 email）
  ├── 找到 → 建立 ExternalID 映射
  │     └── 通过 identity UIN ID 查/创建 UserOrg.ExternalUin 映射
  └── 未找到 → 说明双写阶段遗漏，记录告警日志
→ 返回 JWT
```

### 6.3 离线批量迁移脚本

```bash
./migrate-users \
  --from "mysql://lework_db" \
  --to "https://iam.example.com/v5/account.ImportUser" \
  --api-key "..." \
  --batch-size 100
```

脚本逻辑：
1. 从 Lework `leros_user` 表读取存量用户（email, password hash, name, avatar）
2. 调用 identity-platform `ImportUser` 接口批量导入
3. 记录 `user.id ↔ identity user_id` 的映射关系到本地 `leros_user.external_id`
4. 为每个用户创建 `UserOrg.ExternalUin` 映射

### 6.4 回滚方案

**严禁在生产环境使用 `LEROS_DEV=true` 作为回滚手段** —— 该开关会绕过所有认证，暴露全部 API。

正确的回滚方式：
1. **软切换**：通过 `auth.mode` 配置项切换 `identity → builtin`，重启服务即可回滚
2. **数据保留**：迁移期间保留 Lework 本地用户表完整数据（含密码哈希），不删除
3. **双写兜底**：Phase 3 切换后，双写模式继续运行一段时间，确保可随时切回

---

## 7. API 映射矩阵

| Lework API | Builtin 实现 | SaaS Adapter 实现 |
|-----------|-------------|-------------------|
| `POST /v1/RegisterByEmail` | 本地创建 User + 签发 JWT | 调 identity `RegisterByEmail` |
| `POST /v1/LoginByEmail` | 本地验证密码 + 签发 JWT | 调 identity `LoginByPassword`（注入 domain_name） |
| `POST /v1/SendPhoneLoginCode` | 本地 SMS 发送 | 调 identity `SendPhoneCode` |
| `POST /v1/LoginByPhoneCode` | 本地验证码校验 + 签发 JWT | 调 identity `LoginByPhoneCode` |
| `POST /v1/RefreshToken` | 本地 DB 校验 | 调 identity `RefreshToken` |
| `POST /v1/SwitchOrganization` | 本地 UserOrg 切换 | 调 identity `SwitchLogin` |
| `POST /v1/AuthSession` | 本地 User/UserOrg 查询 | 调 identity `Profile` + `ListUin` |
| `POST /v1/CreateOrganization` | 本地创建 Organization | 调 identity `CreateCompany` |
| `POST /v1/WorkerAuth/Token` | 本地 Worker JWT 签发 | **Lework 本地处理**（不调 identity） |

---

## 8. 数据库变更汇总

### 8.1 Lework 数据库

```sql
-- User 表：新增 external_id
ALTER TABLE leros_user ADD COLUMN external_id BIGINT NULL COMMENT 'identity-platform user.id';
CREATE INDEX idx_user_external_id ON leros_user(external_id);

-- UserOrg 表：新增 external_uin
ALTER TABLE leros_user_org ADD COLUMN external_uin BIGINT NULL COMMENT 'identity-platform uin.id';
CREATE INDEX idx_user_org_external_uin ON leros_user_org(external_uin);

-- Organization 表：新增 external_company_id
ALTER TABLE leros_organization ADD COLUMN external_company_id BIGINT NULL COMMENT 'identity-platform company.id';
CREATE INDEX idx_org_external_company_id ON leros_organization(external_company_id);
```

### 8.2 identity-platform 数据库

```sql
CREATE TABLE refresh_token (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT NOT NULL,
    token_hash VARCHAR(64) NOT NULL,
    expires_at DATETIME NOT NULL,
    revoked BOOLEAN DEFAULT FALSE,
    created_at DATETIME,
    updated_at DATETIME,
    INDEX idx_token_hash (token_hash),
    INDEX idx_user_id (user_id)
);

CREATE TABLE phone_verification_code (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    phone VARCHAR(20) NOT NULL,
    code_hash VARCHAR(64) NOT NULL,
    expires_at DATETIME NOT NULL,
    used BOOLEAN DEFAULT FALSE,
    created_at DATETIME,
    INDEX idx_phone (phone)
);
```

---

## 9. 安全与可靠性

### 9.1 安全

1. **JWT Secret 分发**：通过配置中心/环境变量/密钥管理服务传递，不硬编码
2. **服务间认证**：Lework 调用 identity-platform 时使用 `api_key` 认证，不暴露公网
3. **网络隔离**：两服务部署在同一内网
4. **存量密码**：通过 `ImportUser` 接口直接导入 bcrypt 哈希，不传输明文密码
5. **Fail-Open 策略**：identity-platform 不可用时返回 503，不降级到 builtin

### 9.2 可靠性

| 机制 | 配置 | 说明 |
|------|------|------|
| **HTTP 超时** | `timeout: 5s` | Lework → identity 调用超时，快速失败 |
| **重试策略** | 不重试 | 认证失败快速返回，避免雪崩 |
| **健康检查** | `/v5/health` 定时探活 | identity 不可用时提前标记 |
| **熔断** | 连续 5 次失败后熔断 30s | 避免持续打挂 identity |

### 9.3 日志与审计

SaaS 模式下认证链路跨两个服务，需要关联日志：

1. Lework 调用 identity API 时传递 `X-Request-ID` / `X-Trace-ID`（已有基础设施）
2. Adapter 记录 identity 返回的 `user_id` 和 `uin_id` 到结构化日志
3. 出现认证失败时，可通过 `Request-ID` 关联两个服务的日志

---

## 10. 实施计划

| Phase | 内容 | 时间 |
|-------|------|------|
| 1 | identity-platform 扩展（4 个 API + ImportUser） | 3周 |
| 2 | Lework adapter + 工厂层 + 中间件改造 | 3周 |
| 3 | 存量用户迁移（双写 + 校验 + 切换） | 2周 |

### Phase 1 任务分解

| 任务 | 估算 |
|------|------|
| RegisterByEmail API | 3天 |
| SendPhoneCode API | 2天 |
| LoginByPhoneCode API | 3天 |
| RefreshToken 扩展（数据库版） | 3天 |
| ImportUser API | 2天 |
| API 测试 | 3天 |

### Phase 2 任务分解

| 任务 | 估算 |
|------|------|
| contract/auth.go 接口确认 | 1天 |
| adapter/factory.go（双 build tag） | 1天 |
| adapter/builtin 迁移（从 service/ 拆分） | 3天 |
| middleware/token_parser.go 接口定义 | 1天 |
| middleware/identify.go 改造（接收 TokenParser） | 2天 |
| router.go 改造（通过 factory 注入） | 1天 |
| adapter/identity/claims.go | 1天 |
| adapter/identity/token_parser.go | 2天 |
| adapter/identity/client.go | 2天 |
| adapter/identity/auth.go 实现 | 3天 |
| adapter/identity/mapper.go | 2天 |
| 配置定义 + 注入 | 1天 |
| SaaS 端到端测试 | 3天 |
| 开源版回归测试 | 2天 |

### Phase 3 任务分解

| 任务 | 估算 |
|------|------|
| 双写模式实现 | 3天 |
| 数据校验脚本 | 2天 |
| 自动迁移（首次登录触发） | 3天 |
| 离线批量迁移脚本 | 2天 |
| 灰度切换功能（auth.mode 软切换） | 2天 |

---

## 11. 风险与缓解

| 风险 | 影响 | 概率 | 缓解 |
|------|------|------|------|
| identity-platform 缺少邮箱注册能力 | 高 | 高 | 已纳入 Phase 1 扩展 |
| 存量用户密码哈希无法迁移 | 高 | 中 | 新增 ImportUser 接口直接导入 bcrypt 哈希 |
| Lework 和 identity JWT 格式差异 | 中 | 高 | 独立 claims 结构 + parseIdentityJWT 函数 |
| identity-platform 不可用影响 SaaS | 高 | 低 | 独立部署 + 健康检查 + 503 + 熔断 |
| 两套 adapter 代码漂移 | 中 | 中 | 共享 contract + CI 双构建 |
| 组织模型映射复杂 | 中 | 中 | Phase 2 提前确认映射规则 |
| UIN 语义混淆导致身份解析错误 | 高 | 中 | 身份解析时序图 + ExternalUin 映射表 |
| domain_name 缺失导致 SaaS 登录失败 | 高 | 高 | AuthConfig 配置 issuer + domain_name |

---

## 12. 未纳入范围

- **roc 项目** — 本次不涉及
- **RBAC 权限系统** — Lework 当前无使用，后续单独设计
- **OAuth2 外部账号绑定** — identity-platform 暂无，Lework 暂无需要
- **实名认证** — 非核心认证需求，后续再议
