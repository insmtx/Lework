# Leros 私有化部署手册

本手册给出 Leros 平台在客户内网的 **k3s + Helm** 私有化部署完整流程，从前置准备、镜像同步、配置生成到安装验证、升级与运维。私有化建议优先使用 Helm Chart（`deployments/helm/leros`），由 Server 内置 reconciler 按需拉起 Worker。

配套文档：资源与容量见`private-deployment-resources.md`，全部配置项见`private-deployment-config.md`。

## 1. 前置条件

| 项 | 要求 |
|----|------|
| 集群 | k3s / Kubernetes ≥ 1.24，单节点即可 |
| 工具 | Helm 3、kubectl |
| 镜像 | `leros` / `leros-worker` 及中间件镜像已预导入节点（内网同步/本地导入） |
| 节点 | 所有组件通过 hostPath 共享数据，**须固定到同一节点** |
| GPU | vLLM 模型推理用 GPU 节点（可与 k3s 数据节点分离） |
| 外部网络 | 私有化模型本地部署后可无外网（仅需开机导入所需的模型权重已就位） |

### 1.1 安装 k3s（单机）

```bash
curl -sfL https://get.k3s.io | sh -

# 查看节点名（用于后续 nodeSelector）
kubectl get nodes -o wide
```

> 本手册采用本 Chart 部署独立 Traefik（`traefik.enabled: true`），错开 k3s 自带的 80/443 端口；无需禁用 k3s 自带 Traefik。若要由本 Chart 完全接管 80/443，可安装时加 `--disable traefik`。

## 2. 镜像准备

### 2.1 构建（如需自建）

```bash
# Server 镜像
make build                                                # ./bundles/leros

# Worker 基础镜像（私有化完整版建议）
make docker-build-worker-base-private                     # tag :private

# Worker 镜像（指定 private base）
make docker-build-worker WORKER_BASE_IMAGE=registry.yygu.cn/insmtx/leros-worker-base:private

# 前端
#  见 frontend 目录构建文档
```

版次差异（saas vs private）见 `deployments/build/README.md` 与资源清单。

### 2.2 镜像加载/导入至节点（私有化默认方式）

私有化一般将镜像**预导入节点本地**，节点无外网也能拉取（`imagePullPolicy` 建议 `IfNotPresent`），此时无需镜像拉取凭证，关闭 `imagePullSecret`。

**方式 1：镜像 tar 包 + 本地导入（推荐）**

在可联网环境导出：

```bash
# 用 crane/skopeo 或 docker save 导出 tar
skopeo copy docker://registry.yygu.cn/insmtx/leros:latest docker-archive:leros.tar
```

将 tar 拷贝到节点后导入（二选一，依 k3s 使用 docker/containerd）：

```bash
# containerd（k3s 默认）
sudo ctr -n k8s.io images import leros.tar

# docker 运行时
docker load -i leros.tar
```

导入前在 `values.yaml` 关闭凭证并改拉取策略：

```yaml
imagePullSecret:
  enabled: false
imagePullSecrets: []
server:
  imagePullPolicy: IfNotPresent
worker:
  imagePullPolicy: IfNotPresent
```

**方式 2：内网镜像仓库拉取**

客户已有内网仓库时，同步镜像并配置仓库与凭证：

```bash
# 用 skopeo 从公网/公司仓库同步到客户仓库
for img in \
  registry.yygu.cn/insmtx/leros:latest \
  registry.yygu.cn/insmtx/leros-worker:latest \
  registry.yygu.cn/insmtx/leros-worker-base:private \
  registry.yygu.cn/insmtx/leros-web:latest \
  registry.yygu.cn/library/postgres:18.4 \
  registry.yygu.cn/library/nats:2.12.7 \
  registry.yygu.cn/library/mysql:8.4 \
  registry.yygu.cn/library/redis:7.4 \
  registry.yygu.cn/ygapp/account-api:v0.1.0 \
  registry.cn-beijing.aliyuncs.com/yygu/corekg:busybox_1.36.1 \
; do
  skopeo copy docker://${img} docker://${MIRROR_REPO}/${img##*/}
done

# values.yaml 中配置仓库地址与凭证
imagePullSecret:
  enabled: true
  registry: <内网仓库>
  username: <user>
  password: <pass>
```

> `worker-base:private` 较大（约 4–5 GB），提前确认 tar 传输带宽与内网仓库容量。

### 2.3 私有化模型（vLLM + Qwen3.6-27B）

私有化要求模型数据不出内网，需本地部署大模型。完整方案与显卡配置见 `private-deployment-model.md`，本处以 vLLM + NVIDIA GPU 承载 `Qwen/Qwen3.6-27B`（BF16 权重加载约 51.1 GiB，**单卡 A100-80G 即可运行**）：

1. **准备权重**：在可联网环境从 ModelScope / HF 下载 `Qwen/Qwen3.6-27B`（约 55.6 GB）到内网 GPU 机器，避免模型外泄。
2. **启动 vLLM**（单卡 A100-80G 示例，与实测命令一致）：
   ```bash
   vllm serve /model/Qwen3.6-27B/ \
     --tensor-parallel-size 1 \
     --trust-remote-code \
     --served-model-name Qwen3.6-27B \
     --gpu-memory-utilization 0.925 \
     --default-chat-template-kwargs '{"enable_thinking": false}' \
     --max-model-len=65536 \
     --enable-prefix-caching \
     --enable-auto-tool-choice \
     --tool-call-parser qwen3_coder \
     --dtype bfloat16 \
     --port 8080
   ```
3. **验证**：`curl http://<vllm-host>:8080/v1/models` 返回包含 `Qwen3.6-27B` 的模型列表。

> **上下文 × 并发权衡**：单卡下 `--max-model-len` 越大并发越低（128K 约 2.3×）。并发优先时降低上下文（如 32K/16K）以 KV Cache 换并发；多卡（2×80/4×48）可显著提升并发与上下文。显卡数量须为 2 的幂（`--tensor-parallel-size` 1/2/4/8）、不做量化（BF16 保证质量）。GPU 可独立于 k3s 数据节点；复用 k3s 节点需装 `nvidia-container-toolkit`，不将模型与普通 worker 混部。

## 3. 生成配置

基于 Chart 的 `gen-values.sh` 生成 `values.yaml`，随机密钥自动产生：

```bash
cd deployments/helm/leros

# 私有化预导入镜像时无需 --registry/--user/--pass
./gen-values.sh -f my-values.yaml
```

> 若走内网镜像仓库拉取，才需补传 `--registry-user <user> --registry-pass <pass>`（及 `--registry`）生成镜像拉取 Secret。

### 3.1 必填项

编辑 `my-values.yaml`，至少填写：

```yaml
# ① 固定到同一节点（hostPath 数据共享）
nodeSelector:
  kubernetes.io/hostname: <节点名>

# ② LLM 指向本地方私有化模型（见 2.3 vLLM）
llm:
  provider: openai
  model: Qwen3.6-27B              # 与 vLLM --served-model-name 一致
  baseUrl: "http://<vllm-host>:8080/v1"
  apiKey: "not-needed"            # vLLM 无鉴权时填占位串即可
  limit:
    context: 65536                # 64K，与 --max-model-len 一致；并发优先可调小
    output: 8192                 # 单次输出上限，按任务规模调整；越大越占 KV Cache

# ③ 如需固定版本，覆盖镜像标签（默认 latest）
server:
  image: <内网仓库>/leros:1.2.3
worker:
  image: <内网仓库>/leros-worker:1.2.3
```

> `postgresql/nats/server/worker` 的 `nodeSelector` 自动回退到顶层 `nodeSelector`，无需重复填。
> 私有化预导入镜像走本地拉取，一般无需 `imagePullSecret`；仅走内网镜像仓库拉取时才需填凭证（参考 2.2 方式 2）。
> 模型接入为 **外挂依赖**：先按 2.3 完成 vLLM 部署，再让 `llm.baseUrl` 指向其 `:8080/v1`。

### 3.2 对外访问（独立 Traefik + NodePort 38081）

本方案使用 `values.yaml.template` 默认改动：部署独立 Traefik（`leros-traefik`，NodePort 38081），通过 Ingress 暴露 Server 与 account，无域名时用 `http://<节点IP>:38081` 访问。

```yaml
# 已在 charts/values.yaml.template 按以下默认生效，无需重复填；如需调整再覆盖：
traefik:
  enabled: true
  service:
    type: NodePort
  ports:
    web:
      hostPort: 38081     # http://<节点IP>:38081
ingress:
  enabled: true
  server:
    enabled: true
    host: ""              # 有域名填入；默认 http://<节点IP>:38081
  account:
    enabled: true
    host: ""
    paths:
      - path: /v5/account
        pathType: Prefix
```

> `ingress.account.enabled: true` 依赖 account 服务，需同时启用 `account.enabled: true`（见 3.3）。
> 独立 Traefik 部署时模板会强制校验"改名（`leros-traefik`）+ 错端口（38081/8443）"以避免与 k3s 自带实例冲突。

### 3.3 可选组件

- **Web 前端软件包**：生产环境不部署 Web 镜像，改为向终端用户下发前端软件包；`web` 组件**仅用于测试**。测试时才需 `web.enabled: true`。
- **account 统一登录（企业版认证）**：本方案 `ingress.account.enabled: true` 已暴露 account，需设置 `account.enabled: true`；同时确保对应版本构建为 `enterprise` 并在 `auth` 配置中指向该 account/IAM，参考配置清单相关章节。

## 4. 安装

```bash
helm install leros ./deployments/helm/leros \
  -n leros --create-namespace \
  -f my-values.yaml
```

### 4.1 验证

```bash
kubectl -n leros get pods
kubectl -n leros get svc
kubectl -n leros logs deployment/leros
```

预期状态：
- `leros-postgresql-*`、`leros-nats-*` 为 `Running`
- `leros-*`（Server）及（若启用）`leros-account-*` 为 `Running`（Web 前端软件包仅测试时启用）
- Worker 不常驻：由 Server 启动后按需创建 `leros-worker-o<OrgID>-w<WorkerID>` Deployment

### 4.2 端到端验证

1. Web/接口可访问（依 3.2 选定的访问方式）。
2. `curl <vllm-host>:8080/v1/models` 确认本地模型已就绪。
3. 创建一个数字员工，发起一次任务，确认 Worker 被拉起、模型走本地 vLLM、任务执行成功且产物落盘。
4. 检查 `leros-storage` 与 `leros-workspace` 目录有产出。

> 生产环境终端用户使用下发的前端软件包（Web/桌面）访问后端接口；Web 镜像仅测试用途。

## 5. 备份与恢复

私有化数据位于宿主机 `<dataHostPath>`（默认 `/data/leros`）。关键数据：

| 目录/数据 | 说明 |
|-----------|------|
| `<dataHostPath>/postgresql` | 业务数据库（最关键，建议 PostgreSQL 逻辑备份 + 文件备份双保险） |
| `<dataHostPath>/nats` | 消息队列持久化 |
| `<dataHostPath>/storage` | 产物/文件存储 |
| `<dataHostPath>/workspace` | 工作空间 |
| GPU 机器 `~/models` | 模型权重（`Qwen3.6-27B`，损坏后需重新下载，建议保留 tar 备份） |

**PostgreSQL 逻辑备份**（以 leros 业务库为例）：

```bash
kubectl -n leros exec -it deploy/leros -- bash -c \
  'pg_dump postgres://leros_user:<pass>@leros-postgresql:5432/leros_db > /backup/dump.sql'
```

> 亦可直接在 Pod 内执行或使用外部备份工具。恢复时将 dump 导入同名库 `leros_db`，随后重启 Server 触发迁移。

出问题时可参考 `docs/operations/troubleshooting.md` 与 `deployments/helm/leros/README.md` 的故障排查章节。

## 6. 升级

```bash
helm upgrade leros ./deployments/helm/leros -n leros -f my-values.yaml
```

> `helm upgrade` 用 values 重渲 Server ConfigMap 会覆盖 `scheduler.worker_image`；如需固定 worker 镜像版本，升级前在 `values.yaml` 同步更新 `worker.image`。
>
> 私有化升级需先导入新版本镜像（参考 2.2 方式 1），再到 `values.yaml` 更新 `server.image` / `worker.image` 后执行 `helm upgrade`。
>
> 模型升级（vLLM 镜像或 Qwen 权重）为独立操作：更新 GPU 节点上的 vLLM 镜像/权重后重启容器即可，不涉及 Helm（见 `private-deployment-model.md`）。

数据库 Schema 变更由启动时 AutoMigrate / 迁移逻辑自动处理（见 `AGENTS.md` 的 DB 迁移约束），升级新版本镜像后重启即自动执行。

## 7. 卸载

```bash
helm uninstall leros -n leros
# 可选：清理宿主机数据目录（数据将永久丢失，路径取决于 dataHostPath）
# ssh <node> 'rm -rf /data/leros'
```

## 附录：与现有 k3s 脚本部署兼容

如需与 `deployments/k3s/` 脚本（固定名 `leros` / `leros-server-config` 等）兼容，使用 `*NameOverride`：

```yaml
server:
  serviceNameOverride: leros
  configMapNameOverride: leros-server-config
  deploymentNameOverride: leros
worker:
  configMapNameOverride: leros-worker-config
  secretNameOverride: leros-secret
imagePullSecret:
  enabled: false          # 私有化预导入镜像时关闭；走内网仓库拉取时改为引用已有 Secret
  name: insmtx-registry
```

> 私有化镜像预导入节点后，此处的 `imagePullSecret` 可关闭；仅在内网镜像仓库拉取方案下，通过 `imagePullSecrets` 引用已存在的镜像拉取 Secret。

旧式 k3s 手工部署（非 Helm）仍可按 `deployments/k3s/README.md` 流程操作（Server ConfigMap 方式），但新交付推荐统一走 Helm Chart。
