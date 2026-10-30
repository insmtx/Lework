# Lework 一体机（AIO）

一台机器、一条命令跑起 Lework：PostgreSQL + NATS + Server + Worker + Web + 网关全部在同一个 Compose 栈里。

适用场景：

- 私有化/离线交付；
- 演示与 POC；
- 小团队单机部署（≤ 20 人）。

> 当前一体机默认单组织、单节点。多组织、HA、k8s 属于企业版范围。

## 组成

| 服务 | 作用 | 数据 |
|---|---|---|
| `postgres` | 业务库 | 命名卷 `postgres_data` |
| `nats` | 事件/任务队列（JetStream） | 命名卷 `nats_data` |
| `leros` | Server + 内置 Worker 进程 | `./data/app/{storage,workspace,logs}` |
| `web` | Next.js 控制台 | 无 |
| `gateway` | Nginx 同源网关（Web + `/v1` + SSE） | 无 |

```
浏览器 :80
  └─ gateway
       ├─ /            → web:3000
       ├─ /v1/*        → leros:8080
       ├─ /v1/SessionEvents, /v1/GlobalEvents → SSE（禁缓冲）
       └─ /leros/*     → 本地文件预签名读写
```

`leros` 容器使用 **worker 镜像** 运行 server。Server 的 `process` scheduler 会在同一容器内拉起 `/leros worker`，因此文档、浏览器、CLI 工具链都可用，不需要挂 Docker socket，也不需要第二个 worker 容器。

## 前置条件

- Linux x86_64 / arm64
- Docker Engine 24+
- Docker Compose v2
- 4 vCPU / 8 GB RAM / 40 GB 磁盘起步（使用文档/浏览器工具链建议 8 vCPU / 16 GB）

## 快速开始

### 1. 准备镜像

二选一。

**A. 使用已发布镜像**

```bash
# 默认使用 registry.yygu.cn/insmtx/{worker,web}:latest
# 如需更换，编辑 .env 中的 LEROS_IMAGE / LEROS_WEB_IMAGE
```

**B. 从源码构建（开发机/交付机）**

```bash
cd /path/to/Lework

# 先构建 worker 基础镜像（含文档/浏览器/CLI 工具链，耗时较长）
make docker-build-worker-base

# 构建 server/worker/web
make docker-build-worker
make docker-build-web DOCKER_BUILD_ARGS="--build-arg NEXT_PUBLIC_LEROS_API_BASE_URL=/v1"
```

然后回到 `deployments/aio`，安装时加 `--build`，或把 `.env` 的镜像名指向本地 tag。

### 2. 安装

```bash
cd deployments/aio
./install.sh
```

脚本会：

1. 生成 `.env`（数据库密码、JWT 密钥、存储签名密钥、管理员初始密码）；
2. 渲染 `config/server.config.yaml` 与 `config/worker.config.yaml`；
3. 创建 `./data/app` 并修正权限；
4. 若存在 `leros-aio-images.tar`，自动 `docker load`；
5. `docker compose up -d`；
6. 等待 `/healthz`，打印访问地址与初始管理员密码。

访问 `http://<机器IP>`，用脚本打印的账号登录。

> 首次登录后立即修改管理员密码。`.env` 权限为 600，请一并保管。

## 离线部署

在交付机上准备镜像：

```bash
# 在构建机
docker pull postgres:17
docker pull nats:2.12
docker pull nginx:1.27-alpine
docker save -o leros-aio-images.tar \
  registry.yygu.cn/insmtx/worker:latest \
  registry.yygu.cn/insmtx/web:latest \
  postgres:17 nats:2.12 nginx:1.27-alpine

# 拷贝整个 deployments/aio 目录 + leros-aio-images.tar 到一体机
./install.sh
```

脚本检测到 `leros-aio-images.tar` 会自动导入。镜像 tag 必须与 `.env` 中的 `LEROS_IMAGE` / `LEROS_WEB_IMAGE` 一致。

## 数据与备份

| 路径 | 内容 |
|---|---|
| `./data/app/storage` | 上传文件、artifact |
| `./data/app/workspace` | 项目 Git 工作区 |
| `./data/app/logs` | 服务日志 |
| `./data/backups` | 备份输出 |
| 命名卷 `postgres_data` | 数据库 |
| 命名卷 `nats_data` | JetStream 持久化 |

备份：

```bash
./backup.sh
# data/backups/leros-db-<ts>.sql.gz
# data/backups/leros-app-<ts>.tar.gz
```

恢复：

```bash
./restore.sh data/backups/leros-db-<ts>.sql.gz data/backups/leros-app-<ts>.tar.gz
```

建议把 `./data/backups` 定时同步到外部存储。

## 升级

```bash
# 在线：拉取新镜像
./upgrade.sh

# 离线：把新 leros-aio-images.tar 放到本目录
./upgrade.sh
```

数据库迁移由 server 启动时自动执行（GORM AutoMigrate + 显式迁移）。升级前先 `./backup.sh`。

## 配置

- `config/server.config.yaml`：由 `server.config.yaml.tpl` 渲染，修改模板后重跑 `./install.sh` 会覆盖；直接改生成文件则重装会丢失。
- `config/worker.config.yaml`：worker 的 NATS / workspace 配置；org_id / worker_id 由 process scheduler 通过命令行覆盖。
- `.env`：端口、镜像、管理员、密钥。修改后 `docker compose up -d`。

常用环境变量见 `.env.example`。

### 修改对外地址

文件预览/下载链接基于 `storage.base_url`。如果机器 IP 变化，更新 `.env` 的 `LEROS_PUBLIC_BASE_URL`，重新渲染配置并重启：

```bash
# 编辑 .env
./install.sh   # 会重新渲染配置并重启
```

## 开机自启

```bash
sudo cp systemd/leros-aio.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now leros-aio
```

默认假定安装目录为 `/opt/leros`；如不同，修改 unit 的 `WorkingDirectory`。

## 安全说明

- `LEROS_ENV=production` 时，首次启动必须提供 `LEROS_ADMIN_PASSWORD`，否则服务拒绝初始化。
- 默认只暴露网关 80；PostgreSQL / NATS / Server 均不映射到宿主机。
- 建议在一体机前置一层 HTTPS（Nginx/Caddy/硬件网关），并把 `LEROS_PUBLIC_BASE_URL` 改为 `https://...`。
- 模型 API Key 在控制台配置，不写入 `.env`；后续版本会加密落库。
- 一体机默认单组织；不要直接暴露到公网。

## 故障排查

```bash
docker compose ps
docker compose logs -f leros
docker compose logs -f gateway
docker compose logs -f web
```

常见问题：

| 现象 | 排查 |
|---|---|
| `leros` 反复重启 | `docker compose logs leros`；多为数据库连接或管理员密码缺失 |
| 页面 502 | `web` 未就绪；`docker compose logs web` |
| 登录后接口 404 | 网关未代理 `/v1`；检查 `nginx/nginx.conf` 与 `docker compose ps` |
| 文件预览失败 | `LEROS_PUBLIC_BASE_URL` 与实际访问地址不一致 |
| 权限错误 | `sudo chown -R 1000:1000 data/app` |
| worker 未就绪 | 查看日志中的 `worker deployment`；确认 `scheduler.mode=process` 与 `/leros` 存在 |

## 已知限制

- 单节点，无 HA；PostgreSQL/NATS 重启期间任务会短暂不可用。
- 单组织；组织切换/创建未实现。
- 模型 Key 当前明文落库（后续版本加密）。
- 未内置 HTTPS；请使用前置代理。
- 未内置离线模型；可接 OpenAI 兼容网关或本机 Ollama（在控制台配置 base_url）。
