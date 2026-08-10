# Leros Helm Chart

在 k3s / Kubernetes 集群中部署 Leros AI Agent 平台。覆盖 Server、Worker 运行所需的
ConfigMap / Secret / RBAC，可选的 PostgreSQL、NATS JetStream、Traefik、Web 前端，
以及对外访问路由。

## 架构概览

| 组件 | 类型 | 说明 |
|------|------|------|
| **Leros Server** | Deployment + Service | HTTP API 服务（端口 8080） |
| **Worker 基础设施** | ConfigMap + Secret | 无常驻 Deployment，由 Server 内置 reconciler 按需创建 |
| **PostgreSQL** | StatefulSet + Service | 内置数据库（默认开启，走 hostPath） |
| **NATS JetStream** | StatefulSet + Service | 内置消息队列（默认开启，走 hostPath） |
| **MySQL** | StatefulSet + Service | 内置数据库（account 服务用，默认关闭） |
| **Redis** | StatefulSet + Service | 内置缓存（account 服务用，默认关闭） |
| **Account** | Deployment + Service + ConfigMap + Job | 统一登录账号服务（依赖 MySQL/Redis；初始化 Job 走 hook，默认关闭） |
| **Web 前端** | Deployment + Service | 可选（默认关闭） |
| **Traefik** | 子 chart | 可选 Ingress Controller（默认关闭，复用 k3s 自带） |
| **ImagePullSecret** | Secret | 私有镜像仓库认证 |
| **Ingress** | Ingress | 对外访问路由（默认关闭） |

> **Worker 工作方式**：Server 启动后内置 reconciler，按需在本命名空间创建
> `leros-worker-o<OrgID>-w<WorkerID>` 的 Deployment 运行 worker。worker 通过
> ConfigMap / Secret / hostPath 挂载配置/密钥/工作空间与存储。Server 的
> ServiceAccount 自动被 RBAC 授予在本命名空间管理 Deployment 的权限。

## 前置条件

- k3s / Kubernetes >= 1.24
- Helm 3
- 已构建并推送 `leros` / `leros-worker` 镜像到可访问的镜像仓库
- 一台可用节点（所有组件通过 hostPath 共享数据，需固定到同一节点）

## 快速开始（最小部署）

**1. 生成配置（随机密钥自动产生）**

```bash
cd deployments/helm/leros
./gen-values.sh --registry-user <your-user> --registry-pass <your-pass> -f my-values.yaml
```

**2. 编辑 `my-values.yaml`，只需填两处**：

```yaml
# ⓵ 固定到某节点（hostPath 数据要落在同一节点）
nodeSelector:
  kubernetes.io/hostname: <节点名>

# ② LLM API Key（其他 llm 字段已给默认值）
llm:
  apiKey: <你的模型 API Key>
```

> `postgresql/nats/server/worker` 四处 `nodeSelector` 自动回退到顶层 `nodeSelector`,
> 无需重复填。
> `server`/`worker`/`web` 的镜像默认 `latest`，部署时如要固定版本，改对应
> `server.image`/`worker.image`/`web.image` 为完整镜像地址即可。

**3. 安装**

```bash
helm install leros ./deployments/helm/leros \
  -n leros --create-namespace \
  -f my-values.yaml
```

**4. 验证**

```bash
kubectl -n leros get pods
kubectl -n leros logs deployment/leros
```

## 对外访问（三选一）

默认不对外暴露。按需选一种即可：

### 方式 1：NodePort（无域名、最简）

```yaml
server:
  service:
    type: NodePort
    nodePort: 30080     # → http://<节点IP>:30080
```

### 方式 2：复用 k3s 自带 Traefik

k3s 默认已装 Traefik，开启 Ingress 即用：

```yaml
ingress:
  enabled: true
  server:
    host: leros.corp.local      # 有域名填入；无域名留空用 IP 访问
```

### 方式 3：本 chart 部署 Traefik（非 k3s 集群，或想用新版）

```yaml
traefik:
  enabled: true                 # 自动改名 leros-traefik + 错端口 8080/8443 与 k3s 共存
ingress:
  enabled: true
  server:
    host: ""                     # 无域名留空 → http://<节点IP>:8080
```

> 用其他 Controller（如 nginx-ingress）时：`traefik.enabled: false`，
> `ingress.className: nginx`。
>
> Traefik 启用时模板会强制校验"改名 + 错端口"避免与 k3s 自带实例冲突；
> 想接管 80/443 而不改名，请装 k3s 时加 `--disable traefik` 由本 chart 接管。

## 常用配置

### 宿主机数据目录

所有组件通过 hostPath 共享数据，默认根目录 `/data/leros`。改一处即全局调整：

```yaml
dataHostPath: /opt/leros-data
```

| 组件 | 默认路径 | 覆盖字段 |
|------|----------|----------|
| PostgreSQL | `<dataHostPath>/postgresql` | `postgresql.hostPath` |
| NATS | `<dataHostPath>/nats` | `nats.hostPath` |
| MySQL | `<dataHostPath>/mysql` | `mysql.hostPath` |
| Redis | `<dataHostPath>/redis` | `redis.hostPath` |
| Worker workspace | `<dataHostPath>/workspace` | `worker.workspaceHostPathRoot` |
| Leros 存储 | `<dataHostPath>/storage` | `storage.hostPath` |

### 镜像与凭证

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `imagePullSecret.enabled` | `true` | chart 是否创建 ImagePullSecret |
| `imagePullSecret.registry/username/password` | - | dockerconfigjson 所需 |
| `imagePullSecrets` | `[]` | 引用已存在的 ImagePullSecret |
| `server.image` | `registry.yygu.cn/insmtx/leros:latest` | Server 镜像 |
| `worker.image` | `registry.yygu.cn/insmtx/leros-worker:latest` | Worker 镜像 |
| `web.image` | `registry.yygu.cn/insmtx/leros-web:latest` | Web 镜像（默认关闭） |
| `mysql.image` | `registry.yygu.cn/library/mysql:8.4` | MySQL 镜像（默认关闭） |
| `redis.image` | `registry.yygu.cn/library/redis:7.4` | Redis 镜像（默认关闭） |
| `account.image` | `registry.yygu.cn/ygapp/account-api:v0.1.0` | Account 镜像（默认关闭） |
| `worker.workspaceInitImage` | `busybox_1.36.1` | worker init 容器镜像 |

### 数据库与消息队列

默认内置部署，连接地址由 chart 自动用集群内部 Service 名计算。使用外部实例时：

```yaml
postgresql:
  enabled: false
  external:
    url: "postgres://user:pass@ext-host:5432/leros"
nats:
  enabled: false
  external:
    url: "nats://ext-host:4222"
```

### Account 服务

Account 是独立账号服务，用于统一登录，依赖 MySQL 与 Redis。默认关闭。支持两种部署情况，
通过 `account.reuseCorekg` 切换：

#### 情况 1：复用 corekg（reuseCorekg=true）

corekg 已部署时，不部署内置 MySQL/Redis，account 直接连接 corekg 命名空间下的
MySQL/Redis Service；此时**不做初始化**（数据由 corekg 侧负责）：

```yaml
account:
  enabled: true
  reuseCorekg: true
  image: registry.yygu.cn/ygapp/account-api:v0.1.0
  corekg:
    namespace: corekg          # corekg 所在命名空间
    mysqlService: corekg-mysql # corekg 的 MySQL Service 名
    redisService: corekg-redis # corekg 的 Redis Service 名
    # 凭据留空回退到 mysql/redis 顶层值；corekg 凭据不同时显式覆盖：
    # mysqlUsername: "xxx"
    # mysqlPassword: "xxx"
    # mysqlDatabase: "xxx"
    # redisPassword: "xxx"
mysql:
  enabled: false
redis:
  enabled: false
```

#### 情况 2：独立部署（默认，reuseCorekg=false）

corekg 未部署时，随 chart 部署内置 MySQL/Redis，并通过**初始化 Job**（`post-install`/
`post-upgrade` hook）依次执行 `--migrate-db` 建表与 `init` 种子数据：

```yaml
account:
  enabled: true
  reuseCorekg: false                 # 默认
  initialize: true                   # 是否执行初始化 Job
  init:
    issuer: "yygu"                   # IAM_INIT_ISSUER
    jwtSecret: "<≥32位密钥，生产必改>"   # IAM_INIT_JWT_SECRET
    company: "default-company"
    adminEmail: "admin@admin.com"
    domain: "localhost"
mysql:
  enabled: true
redis:
  enabled: true
```

> 初始化 Job 的 `image` 复用 `account.image`，二进制由 `account.initCommand.binary`
> 指定（默认 `account`）；如需自定义 `core_settings.yaml`，设置
> `account.initCommand.settingFile` 与 `account.initCommand.settingsConfigMap`。

也可给两种情况的 account 都改用**外部实例**（此时不部署内置中间件，也不做初始化）：

```yaml
mysql:
  enabled: false
  external:
    url: "mysql://user:pass@host:3306/db?charset=utf8mb4&parseTime=true&loc=Local"
redis:
  enabled: false
  external:
    url: "redis://:pass@host:6379/0"
```

对外暴露 Account（可选，复用 ingress 链路）：
```yaml
ingress:
  enabled: true
  account:
    enabled: true
    host: ""                     # 有域名填入；默认如下路径前缀
    paths:
      - path: /v5/account
        pathType: Prefix
```

### LLM

`llm.apiKey` 必填。`model` / `baseUrl` 已给 DeepSeek 的默认值，换 OpenAI 改：

```yaml
llm:
  apiKey: <key>
  provider: openai
  model: gpt-4o
  baseUrl: "https://api.openai.com/v1"
```

### 敏感信息（自动生成）

`gen-values.sh` 自动随机生成并通过 Secret 注入：

| 环境变量 | 来源 | 说明 |
|----------|------|------|
| `LEROS_JWT_SECRET` | `server.jwtSecret` | JWT 签名密钥 |
| `LEROS_DATABASE_URL` | 自动计算 / `postgresql.external.url` | 数据库连接串 |
| `LEROS_NATS_URL` | 自动计算 / `nats.external.url` | NATS 连接串 |
| `LLM_API_KEY` | `llm.apiKey` | LLM 密钥 |
| `LEROS_STORAGE_SIGN_SECRET` | `storage.signSecret` | 预签名 URL 校验密钥 |
| `LEROS_BASE_URL` | `server.baseUrl` | 留空自动用集群内部 Service 地址 |
| MySQL Password | `mysql.password` | MySQL 业务用户密码（`gen-values.sh` 随机生成） |
| MySQL Root Password | `mysql.rootPassword` | MySQL 根密码（`gen-values.sh` 随机生成） |
| Redis Password | `redis.password` | Redis 访问密码（`gen-values.sh` 随机生成） |

> Account 的连接串与 `account.init.jwtSecret` 直接写入 ConfigMap（明文），非自动生成，
> 由部署者填写；生产环境请自行用外部 Secret/加密方案加固，勿在仓库中提交真实密钥。


## 升级

```bash
helm upgrade leros ./deployments/helm/leros -n leros -f my-values.yaml
```

> `helm upgrade` 会用 values 重新渲染 Server ConfigMap，从而覆盖
> `scheduler.worker_image`。若通过 CI 脚本单独更新过该字段，升级前请同步更新 `worker.image`。

## 卸载

```bash
helm uninstall leros -n leros
# 可选：清理宿主机数据目录（数据将丢失，路径取决于 dataHostPath，默认 /data/leros）
# ssh <node> 'rm -rf /data/leros'
```

## 与现有 k3s 部署兼容

本 chart 默认使用 `<release>-leros` 风格命名。如需与现有 `deployments/k3s/`
脚本（固定名 `leros` / `leros-server-config` / `leros-worker-config` / `leros-secret`
等）兼容，用 `*NameOverride` 字段指定：

```yaml
server:
  serviceNameOverride: leros
  configMapNameOverride: leros-server-config
  deploymentNameOverride: leros
worker:
  configMapNameOverride: leros-worker-config
  secretNameOverride: leros-secret
imagePullSecret:
  name: insmtx-registry
```

## 故障排查

- **worker Pod 一直 Pending**：检查顶层 `nodeSelector` 指定的节点是否存在、hostPath
  目录是否可创建。
- **Server 日志报 `database connection refused`**：确认 `postgresql.enabled=true` 时
  `leros-postgresql` 已就绪；外部数据库时确认 `postgresql.external.url` 正确且可达。
- **worker 无法拉取镜像**：确认 `imagePullSecret` 已创建且 `scheduler.image_pull_secret`
  名称与 chart 渲染一致。
- **`imagePullSecret.enabled=true` 安装报错**：必须同时提供 `username` 与 `password`。
- **`traefik.enabled=true` 渲染失败**：类名不得为 `traefik`、端口不得为 `80/443`——
  按错误提示改即可。
