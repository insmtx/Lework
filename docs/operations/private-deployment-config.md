# 私有化配置清单

本文档汇总 Leros 私有化部署涉及的配置项，覆盖 Helm `values.yaml`、服务端 `config.yaml` 与 Worker 配置。配置结构与代码定义一一对应，来源为 `backend/config/` 及 `deployments/helm/leros/values.yaml.template`。部署时优先通过 Helm values 管理，`config.yaml` 用于在 Chart 未覆盖时注入底层配置。

## 1. 配置载体与生成

| 载体 | 路径 | 说明 |
|------|------|------|
| Helm Values | `deployments/helm/leros/values.yaml.template` | 主配置模板，占位符由 `gen-values.sh` 替换后生成 `values.yaml` |
| 生成脚本 | `deployments/helm/leros/gen-values.sh` | 替换随机口令占位符（PG/NATS/JWT/存储签名）；`--registry/--user/--pass` 用于需镜像仓库拉取凭证的场景，私有化预导入镜像时通常不用传 |
| Server config | `deployments/k3s/server.config.example.yaml` | k3s 部署的 Server ConfigMap 配置模板 |
| 完整示例 | `config.example.yaml` | 根目录完整示例配置 |
| 最小示例 | `minimal-config.yaml` | 快速启动最小配置 |

Helm values 占位符：

| 占位符 | 生成方式 |
|--------|----------|
| `@PG_PASSWORD@` | 随机生成 |
| `@NATS_PASSWORD@` | 随机生成 |
| `@JWT_SECRET@` | 随机生成 |
| `@STORAGE_SIGN_SECRET@` | 随机生成 |
| `@MYSQL_PASSWORD@` / `@MYSQL_ROOT_PASSWORD@` / `@REDIS_PASSWORD@` | 随机生成 |
| `@REGISTRY@` / `@REGISTRY_USER@` / `@REGISTRY_PASS@` | `gen-values.sh` 命令行传入 |
| 顶层 `llm.apiKey` | **不自动生成，需手动填写** |

## 2. 全局配置（values.yaml 顶层）

| 字段 | 说明 | 默认 |
|------|------|------|
| `dataHostPath` | 宿主机数据根目录，替换所有组件 `<dataHostPath>` 占位符 | `/data/leros` |
| `nodeSelector` | 全局节点选择器，私有化须固定组件到同一节点 | `{}` |
| `imagePullSecret` | 私有镜像仓库凭证（创建 dockerconfigjson Secret）。镜像预导入节点本地时**通常不需**，设 `enabled: false` | `enabled: true` |
| `imagePullSecrets` | 额外引用已存在的镜像拉取 Secret（从内网仓库拉取时用） | `[]` |

## 3. 数据库与中间件

### 3.1 PostgreSQL（Leros 业务库）

| 字段 | 说明 |
|------|------|
| `postgresql.enabled` | 是否部署内置 PostgreSQL |
| `postgresql.image` | 镜像（默认 `registry.yygu.cn/library/postgres:18.4`） |
| `postgresql.username` / `password` / `database` | 账号口令库名 |
| `postgresql.hostPath` | 数据宿主机目录 |
| `postgresql.nodeSelector` / `resources` | 节点选择与资源 |
| `postgresql.external.url` | 复用外部数据库时的连接串 |

> 改用外部 PostgreSQL 时 `postgresql.enabled=false` 并填 `external.url`。相应 `database.url`（见 Server 配置）指向外部库。

### 3.2 NATS（JetStream 消息队列）

| 字段 | 说明 |
|------|------|
| `nats.enabled` | 是否部署内置 NATS |
| `nats.image` | 镜像（默认 `nats:2.12.7`） |
| `nats.hostPath` / `nodeSelector` / `resources` | 数据目录 / 节点 / 资源 |
| `nats.auth` | 是否启用用户名口令认证（默认开启，用户 `leros`） |
| `nats.external.url` | 复用外部 NATS 的连接串 |

> 开启认证时 `nats.external` 需填带口令的连接 URL；Server/Worker 的 `nats.url` 需与之匹配。

### 3.3 MySQL / Redis / account（可选，企业版统一登录）

| 字段 | 说明 |
|------|------|
| `mysql.enabled` | 是否部署内置 MySQL（account 使用） |
| `redis.enabled` | 是否部署内置 Redis（account 使用） |
| `mysql.external.url` / `redis.external.url` | 复用外部中间件的连接串 |
| `account.enabled` | 是否启用 account/IAM 服务 |
| `account.reuseCorekg` | 是否复用 corekg 命名空间下的 MySQL/Redis（`false` 时随 Chart 部署内置中间件并执行初始化 Job） |
| `account.init` | `issuer`、`jwtSecret`（≥32 字符）、`company`、`adminEmail`（必填）、`domain` 等初始化参数 |
| `account.env` | account 运行环境（`test`/`prod`） |

## 4. Server 配置

### 4.1 Helm values 层

| 字段 | 说明 |
|------|------|
| `server.image` / `replicas` / `imagePullPolicy` | 镜像、副本数、拉取策略 |
| `server.env` | 环境（`prod`） |
| `server.logLevel` | 日志级别（`info`） |
| `server.jwtSecret` | JWT 签名密钥（默认由 `gen-values.sh` 生成） |
| `server.baseUrl` | 对外基础 URL |
| `server.ginMode` | Gin 运行模式（`release`） |
| `server.resources` | 资源限制 |
| `server.service` | Service 类型/端口/NodePort/LoadBalancer |
| `server.rbac` | 是否创建 RBAC |
| `server.extraEnv` / `logger` | 额外环境变量与日志配置 |

### 4.2 `config.yaml`（`server.config.example.yaml` → ConfigMap `leros-server-config`）

与代码 `backend/config.Config` 对应：

| 顶层键 | 子键 | 说明 |
|--------|------|------|
| `server` | `port` | HTTP 端口 |
| | `disable_event_consumers` | 是否禁用后台事件消费者 |
| | `jwt.secret` | JWT 签名密钥 |
| `env` | — | 运行环境 |
| `workspace_root` | — | 工作空间根目录 |
| `log.level` / `logger` | — | 日志级别与配置 |
| `nats.url` | — | NATS 连接地址 |
| `database.url` / `debug` | — | PostgreSQL 连接串与 SQL 日志开关 |
| `llm` | `provider` / `api_key` / `model` / `base_url` / `vision` | 默认 LLM |
| | `top_p` / `frequency_penalty` / `presence_penalty` | 采样参数（仅 opencode runtime） |
| | `limit.context` / `limit.output` | 上下文/输出 token 上限 |
| | `translation` | 内置翻译模型的 provider/api_key/model/base_url/is_default |
| `scheduler` | `mode` | 调度模式：`process`/`docker-cli`/`docker-api`/`k8s` |
| | `namespace` / `server_addr` | k8s 调度的命名空间与 server 地址 |
| | `worker_image` / `image_pull_secret` | Worker 镜像与拉取 Secret |
| | `workspace_init_image` | workspace 初始化镜像 |
| | `config_map` / `secret` | Worker ConfigMap/Secret 名 |
| | `workspace_host_path_root` / `workspace_mount_root` | Worker 工作空间宿主机/容器路径 |
| | `storage_host_path` / `storage_mount_path` | Worker 存储宿主机/容器路径 |
| | `node_selector` | Worker Pod 节点选择 |
| | `worker_resources` | Worker 容器资源限制（`limits`/`requests`） |
| `storage` | `driver` / `local_dir` / `endpoint` / `access_key` / `secret_key` / `use_ssl` / `bucket` / `base_url` / `url_style` / `sign_secret` | 文件存储（`local`/`s3`/`minio`） |
| `gitea` | `enabled` / `endpoint` / `access_token` / `owner` | 外部 Gitea |
| `worker_auth` | `bootstrap_tokens`（`org_id`/`worker_id`/`token`）/ `token_ttl_seconds` | Worker 引导令牌认证 |
| `aliyun` | `access_key_id` / `access_key_secret` / `sign_name` / `template_code` / `region_id` / `default_code` | 阿里云短信验证码 |
| `client_update` | `desktop` / `web` 的 `min_supported_version` / `latest_version` / `update_url` / `force_message` | 客户端版本策略 |
| `feishu` | `enabled` / `app_id` / `app_secret` / `app_token` / `table_id` | 飞书反馈 Bitable 同步 |
| `auth` | `base_url` / `domain_name` / `phone_code_login_enabled` | IAM 认证（enterprise 版） |
| `automation_scheduler` | `enabled` / `planner_interval` | 自动化定时调度 |
| `mcp_connectors` | `channel` / `name` / `description` / `status` / `transport` / `url` / `headers` / `bindings` / `auth` | 系统级 MCP 连接器模板 |

## 5. Worker 配置

对应代码 `backend/config.WorkerConfig`、`RunConfig`、`CLIEnginesConfig`：

| 字段 | 说明 | 默认 |
|------|------|------|
| `org_id` / `worker_id` | Worker 绑定到组织与数字员工 | — |
| `server_addr` | Worker Server 地址 | — |
| `auth_token` / `bootstrap_token` | Worker 认证令牌 | — |
| `workspace_root` | Worker 工作空间根目录 | — |
| `env` / `log` / `logger` | 环境与日志 | — |
| `nats.url` | NATS 连接地址 | — |
| `cli.default` | 外部 CLI 引擎（`claude`/`codex`/`opencode`） | — |
| `cli.mcp.url` / `bearer_token` | 外部 CLI 的 MCP 注册 | — |
| `run.max_concurrency` | 实际可占用计算资源的并发任务数 | `10` |
| `run.max_inflight` | Worker 准入并发上限 | `20` |
| `run.max_interaction_waits` | 最大并发交互等待数量 | `10` |
| `run.debounce_ms` | trailing debounce 窗口（毫秒） | `1500` |
| `run.interaction_timeout_seconds` | 审批/问题等待硬超时（秒） | `600` |
| `run.max_queued_commands` | 本地队列允许的非终态命令上限 | `1000` |
| `run.queue_retry_seconds` | 队列满载时向 JetStream 延迟重投秒数 | `15` |
| `run.queue_start_timeout_seconds` | 命令允许开始执行的最长等待 | `1800` |
| `run.max_run_duration_seconds` | 单个 Run 的硬超时 | `14400` |

> `RunConfig` 的归一化默认值统一在 `backend/config/worker.go` 的 `Effective()` 中处理，缺省项按上表回填。

### Helm values 层（`worker` 块）

| 字段 | 说明 |
|------|------|
| `worker.image` / `imagePullPolicy` | Worker 镜像与拉取策略 |
| `worker.modelrouterDebug` | 是否开启 ModelRouter 调试 |
| `worker.workspaceInitImage` | workspace 初始化镜像 |
| `worker.config.logLevel` / `cli.default` | 日志级别与默认 CLI 引擎 |
| `worker.workspaceHostPathRoot` / `workspaceMountRoot` | 工作空间宿主机/容器路径 |
| `worker.storageHostPath` / `storageMountPath` | 存储宿主机/容器路径 |
| `worker.nodeSelector` / `resources` | 节点选择与资源 |

## 6. Web 前端（前端软件包，仅用于测试）

> 生产私有化不部署 Web 镜像，改为下发前端软件包给终端用户；`web` 组件仅用于测试验证。

| 字段 | 说明 |
|------|------|
| `web.enabled` | 是否部署前端（仅测试用） |
| `web.image` / `replicas` / `imagePullPolicy` | 镜像、副本、拉取策略 |
| `web.apiBaseUrl` | 后端 API 地址 |
| `web.resources` / `service` | 资源与 Service |

## 7. LLM

| 字段 | 说明 |
|------|------|
| `llm.apiKey` | **必填**，模型 API Key（私有化 vLLM 无鉴权时可填占位串） |
| `llm.provider` | `openai` / `anthropic` / `deepseek` 等（vLLM 用 `openai`） |
| `llm.model` | 模型名（私有化时与 vLLM `--served-model-name` 一致，如 `Qwen3.6-27B`） |
| `llm.baseUrl` | 模型服务地址（私有化时指向 vLLM `http://<vllm-host>:8080/v1`） |
| `llm.vision` | 默认模型是否支持多模态输入 |
| `llm.top_p` / `frequency_penalty` / `presence_penalty` | 采样参数（仅 opencode runtime 生效，留空走默认） |
| `llm.limit.context` / `limit.output` | 上下文/输出 token 上限；`context` 须与 vLLM `--max-model-len` 一致，`output` 为单次生成上限（按任务规模调，越大越占 KV Cache）。如 `65536/8192` |
| `llm.translation` | 翻译模型开关与配置 |

私有化模型本地部署的完整接入见 `private-deployment-model.md`，配置示例：

```yaml
llm:
  provider: openai
  model: Qwen3.6-27B
  baseUrl: "http://<vllm-host>:8080/v1"
  apiKey: "not-needed"
  # 上下文须与 vLLM --max-model-len 一致；并发优先时调小（KV Cache 换并发）
  limit:
    context: 65536
    output: 8192             # 单次输出上限，按任务规模调整；越大越占 KV Cache
```

## 8. Enterprise IAM（企业版认证，`-tags enterprise` 构建）

| 字段 | 说明 |
|------|------|
| `auth.enabled` | 是否启用 IAM 认证 |
| `auth.baseUrl` | IAM 服务地址 |
| `auth.domainName` | 域名，用于登录配置查询 |
| `auth.phoneCodeLoginEnabled` | 是否开启手机号验证码登录 |

> 对应代码 `backend/config.IAMConfig`。仅企业版构建（`backend/internal/adapter/account/enterprise/`）使用；OSS 版使用内置认证（`backend/internal/adapter/account/oss/`）。

## 9. 文件存储

| 字段 | 说明 |
|------|------|
| `storage.driver` | `local` / `s3` / `minio` |
| `storage.localDir` | 本地存储目录 |
| `storage.hostPath` | 宿主机存储目录 |
| `storage.bucket` | 桶名 |
| `storage.signSecret` | 预签名 URL 签名密钥 |
| `storage.endpoint` / `accessKey` / `secretKey` / `useSSL` | 对象存储端点与凭据 |

## 10. 客户端版本策略（可选）

| 字段 | 说明 |
|------|------|
| `clientUpdate.desktop` | `minSupportedVersion` / `latestVersion` / `updateUrl` / `forceMessage` |
| `clientUpdate.web` | 同上 |

## 11. Ingress / 流量

本方案默认部署独立 Traefik（`traefik.enabled: true`，Service 为 NodePort）+ 启用 Ingress。

| 字段 | 说明 |
|------|------|
| `traefik.enabled` | `true`（部署独立 Traefik `leros-traefik`，错开 k3s 自带 80/443） |
| `traefik.service.type` | `NodePort` |
| `traefik.ports.web.hostPort` | `38081`（HTTP 访问端口） |
| `traefik.ports.websecure.hostPort` | `8443`（HTTPS） |
| `ingress.enabled` | `true`（启用 Ingress 暴露） |
| `ingress.className` / `tls` | IngressClass 与证书（留空自动跟随） |
| `ingress.server.enabled` / `host` | 后端访问；`host` 留空用 `http://<节点IP>:38081` |
| `ingress.account.enabled` / `host` | 暴露 account（路径 `/v5/account`）；启用需 `account.enabled: true` |


