# Leros Helm Chart（k3s 私有化部署）

本 Chart 用于在 k3s / Kubernetes 集群中部署 Leros AI Agent 平台，涵盖 Server、
Worker 运行所需的 ConfigMap/Secret/RBAC、可选的 PostgreSQL 与 NATS JetStream、
可选的 Web 前端，以及 Ingress 路由。

## 架构概览

部署后包含以下资源（`各组件均可通过 values 开关`）：

| 组件 | 类型 | 说明 |
|------|------|------|
| **Leros Server** | Deployment + Service | HTTP API 服务（端口 8080），常驻运行 |
| **Worker 基础设施** | ConfigMap + Secret | Worker 无常驻 Deployment，由 Server 通过 k8s scheduler 动态创建 |
| **PostgreSQL** | StatefulSet + Service | 可选，内置数据库（默认开启，数据走 hostPath） |
| **NATS JetStream** | StatefulSet + Service | 可选，内置消息队列（默认开启，数据走 hostPath） |
| **Web 前端** | Deployment + Service | 可选，Next.js 前端（默认关闭） |
| **ImagePullSecret** | Secret | 私有镜像仓库认证 |
| **Ingress** | Ingress | 路由（k3s 默认 Traefik） |

> **Worker 工作方式**：Server 启动后内置 reconciler，按需在本命名空间创建
> `leros-worker-o<OrgID>-w<WorkerID>` 的 Deployment 来运行 worker。worker 通过
> ConfigMap（`<fullname>-worker-config`，data key 为 `config.yaml`）挂载配置，
> 通过 Secret（`<fullname>-secret`，key `LLM_API_KEY`）注入 LLM 密钥，通过
> hostPath 挂载工作空间与存储目录。因此 Server 的 ServiceAccount 必须具备在本
> 命名空间管理 Deployment 的权限（本 Chart 通过 RBAC 自动授予）。

## 前置条件

- k3s / Kubernetes >= 1.24
- Helm 3
- 已构建并推送 `leros` / `leros-worker`（及可选 `leros-web`）镜像到可访问的镜像仓库
- 所有组件通过 hostPath 挂载宿主机目录，需指定节点（`nodeSelector`）让数据落于同一节点

## 快速安装（无域名私有化部署）

私有化部署默认无域名，所有服务间依赖用集群内部 Service 地址，Ingress 默认关闭。
只需提供镜像凭证和敏感密钥即可部署：

1. 准备 `my-values.yaml`：

```yaml
imagePullSecret:
  enabled: true
  name: leros-registry
  registry: registry.yygu.cn
  username: <your-registry-user>
  password: <your-registry-pass>

server:
  image: registry.yygu.cn/insmtx/leros:<tag>
  jwtSecret: <强随机字符串>
  # baseUrl 默认用集群内部 http://<release>:8080，无需填写

worker:
  image: registry.yygu.cn/insmtx/leros-worker:<tag>

# 所有组件的 hostPath 默认基于 dataHostPath（/data/leros）拼接，需固定同一节点
# 统一指定节点（postgresql/nats/server/worker 都会落到此节点）
postgresql:
  nodeSelector:
    kubernetes.io/hostname: <节点名>
nats:
  nodeSelector:
    kubernetes.io/hostname: <节点名>
worker:
  nodeSelector:
    kubernetes.io/hostname: <节点名>

storage:
  signSecret: <强随机字符串>

llm:
  apiKey: <LLM API Key>

# 无域名时无需 Ingress，默认关闭。通过 NodePort/LoadBalancer 或 kubectl port-forward 访问
# 有域名时取消注释：
# ingress:
#   enabled: true
#   server:
#     host: leros.corp.local
```

> 也可以通过 `dataHostPath: /opt/leros-data` 修改宿主机数据根目录，所有组件
> （postgresql/nats/workspace/storage）自动在 `<dataHostPath>/` 下创建子目录。

2. 安装：

```bash
helm install leros ./deployments/helm/leros \
  -n leros --create-namespace \
  -f my-values.yaml
```

3. 验证：

```bash
kubectl -n leros get pods
kubectl -n leros logs deployment/leros
```

## 关键配置说明

### 镜像与凭证

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `imagePullSecret.enabled` | `true` | 是否由 chart 创建 ImagePullSecret |
| `imagePullSecret.name` | `leros-registry` | Secret 名称，同时写入 `scheduler.image_pull_secret` |
| `imagePullSecret.registry/username/password` | - | 创建 dockerconfigjson 所需 |
| `imagePullSecrets` | `[]` | 引用已存在的 ImagePullSecret |
| `server.image` | `registry.yygu.cn/insmtx/leros:latest` | Server 镜像 |
| `worker.image` | `registry.yygu.cn/insmtx/leros-worker:latest` | Worker 镜像 |
| `worker.workspaceInitImage` | `busybox_1.36.1` | worker 初始化容器镜像（改 workspace 目录权限） |
| `dataHostPath` | `/data/leros` | 宿主机数据根目录，所有组件 hostPath 默认基于此前缀拼接 |

### 宿主机数据目录（统一前缀）

所有组件通过 hostPath 挂载宿主机目录，路径统一基于 `dataHostPath`（默认
`/data/leros`）拼接，无需逐个配置：

| 组件 | 默认路径 | 覆盖字段 |
|------|----------|----------|
| PostgreSQL | `<dataHostPath>/postgresql` | `postgresql.hostPath` |
| NATS | `<dataHostPath>/nats` | `nats.hostPath` |
| Worker workspace | `<dataHostPath>/workspace` | `worker.workspaceHostPathRoot` |
| Leros 存储 | `<dataHostPath>/storage` | `storage.hostPath` / `worker.storageHostPath` |

留空对应字段即自动拼接；填入绝对路径则单独覆盖。所有组件需通过 `nodeSelector`
固定到同一节点，使 hostPath 数据共享。

### 数据库与消息队列

默认内置部署 PostgreSQL 与 NATS，连接地址用集群内部 Service 名由 chart 自动计算。
使用外部实例时关闭内置组件并提供连接地址：

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

### Worker 调度与存储

worker Pod 通过 hostPath 共享宿主机目录（路径自动基于 `dataHostPath`），必须用
`nodeSelector` 固定到含数据的节点。Server Deployment 同样继承 `worker.nodeSelector`
以共享本地存储：

```yaml
worker:
  # 以下字段留空自动基于 dataHostPath 拼接
  workspaceHostPathRoot: ""    # → <dataHostPath>/workspace
  storageHostPath: ""          # → <dataHostPath>/storage
  workspaceMountRoot: /workspace
  storageMountPath: /leros-storage
  nodeSelector:
    kubernetes.io/hostname: node-1   # 必须指定，否则 hostPath 找不到数据
```

### 敏感信息

敏感值通过 Secret 注入，配置文件中以 `${VAR}` 占位符引用（Server 启动时由
`os.ExpandEnv` 替换）：

| 环境变量 | 来源 | 说明 |
|----------|------|------|
| `LEROS_JWT_SECRET` | `server.jwtSecret` | JWT 签名密钥 |
| `LEROS_DATABASE_URL` | 自动计算 / `postgresql.external.url` | 数据库连接串（含密码） |
| `LEROS_NATS_URL` | 自动计算 / `nats.external.url` | NATS 连接串 |
| `LLM_API_KEY` | `llm.apiKey` | LLM 密钥，server 与 worker 共用 |
| `LEROS_STORAGE_SIGN_SECRET` | `storage.signSecret` | 预签名 URL 校验密钥 |
| `LEROS_BASE_URL` | `server.baseUrl` | 留空自动用集群内部 Service 地址 |

### Ingress（有域名时开启）

私有化无域名场景默认关闭 Ingress。有域名时开启：

```yaml
ingress:
  enabled: true
  className: traefik            # k3s 内置；nginx 集群填 nginx
  tls: []                       # [{ secretName: leros-tls, hosts: [leros.corp.local] }]
  server:
    enabled: true
    host: leros.corp.local
```

如需直接对外暴露端口而不走 Ingress，可将 `server.service.type` 设为
`LoadBalancer` 或 `NodePort`。

## 升级

```bash
helm upgrade leros ./deployments/helm/leros -n leros -f my-values.yaml
```

> 注意：`helm upgrade` 会用 values 重新渲染 Server ConfigMap，从而覆盖
> `scheduler.worker_image`。若通过 CI 脚本（`update-leros-images.sh`）单独更新过
> 该字段，升级前请同步更新 `worker.image`，否则会被回退。

## 卸载

```bash
helm uninstall leros -n leros
# 可选：清理宿主机上的数据目录（数据将丢失，路径取决于 dataHostPath，默认 /data/leros）
# ssh <node> 'rm -rf /data/leros'
```

## 资源命名与现有 k3s 部署兼容

本 chart 默认使用 `<release>-leros` 风格命名。如需与现有 `deployments/k3s/`
脚本（`leros` / `leros-server-config` / `leros-worker-config` / `leros-secret`
等固定名）兼容，可使用 `*NameOverride` 字段指定：

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

- **worker Pod 一直 Pending**：检查 `nodeSelector` 指定的节点是否存在、hostPath
  目录是否可创建。
- **Server 日志报 `database connection refused`**：确认
  `postgresql.enabled=true` 时 `leros-postgresql` 已就绪；外部数据库时确认
  `postgresql.external.url` 正确且网络可达。
- **worker 无法拉取镜像**：确认 `imagePullSecret` 已创建且 `scheduler.secret`/
  `image_pull_secret` 名称与 chart 渲染一致（见 Server ConfigMap）。
- **`imagePullSecret.enabled=true` 安装报错**：必须同时提供 `username` 与
  `password`。
