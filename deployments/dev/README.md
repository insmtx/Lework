# Leros Development Environment

Docker Compose-based development environment for Leros with offset ports and independent component management.

## Prerequisites

- Docker
- Docker Compose (`docker-compose`)
- Go
- Node.js with pnpm or npm (for local frontend development)

## Quick Start

### Initial Setup (First Time Only)

```bash
cd deployments/dev
./dev-setup.sh
```

This creates `.env`, `server.config.yaml`, and `worker.config.yaml` from the example templates if they do not already exist, then starts PostgreSQL and NATS.

Edit `.env` and set your `LLM_API_KEY`. Review `server.config.yaml` and `worker.config.yaml` before starting the application components.

### Start Infrastructure

Start infrastructure (PostgreSQL, NATS):

```bash
docker-compose -f docker-compose.dev.yml up -d
```

### Start Individual Components

After starting infrastructure, start components independently:

```bash
# Start server
./dev-server.sh

# Start worker
./dev-worker.sh

# Start frontend (requires Node.js)
./dev-frontend.sh
```

### One Command

Start server + worker + frontend together (infrastructure must already be running):

```bash
make dev-up       # alias: make dev-all
make dev-status   # 查看 8080/8081/3005 状态
make dev-logs     # 查看三个服务的最近日志
make dev-down     # 停止 dev-up 启动的进程
```

日志与 pid 文件在 `deployments/dev/logs/`。若前端首次请求长时间无响应，可删除 `frontend/apps/web/.next` 后重试（Next dev 缓存问题）。

### Windows Quick Start

If you are developing on Windows, use the tracked PowerShell/CMD wrappers under `deployments/dev/`:

```powershell
# Start Docker deps + server + worker + frontend
.\deployments\dev\start-dev.cmd

# Rebuild and restart backend only
.\deployments\dev\restart-backend.cmd

# Stop frontend + backend + Docker deps
.\deployments\dev\stop-dev.cmd
```

Notes for Windows:

- `stop-dev.cmd` will auto request administrator permission when needed, because Windows may block killing the frontend process tree under normal permission.
- `start-dev.cmd` keeps using the repo's normal frontend `dev:web` flow, so frontend still hot reloads as usual.
- `start-dev.cmd` waits for PostgreSQL/NATS health checks, server/worker/frontend ports, and syncs `~/.leros/config.yaml` before reporting ready.
- If `bundles/leros.exe` is missing, `start-dev.cmd` will rebuild it automatically.
- If the local database contains duplicate user-org UINs and the backend migration cannot create its unique index, `start-dev.cmd` will explain the conflict and ask whether to reset the local PostgreSQL/NATS volumes. The reset requires explicit `y` confirmation.

The server and worker scripts support `--build` (or `-b`) to rebuild `./bundles/leros` before starting. The scripts load `deployments/dev/.env` before reading YAML config, so values like `${LLM_API_KEY}` in config files are resolved from `.env`.

### View Logs

```bash
# All services
docker-compose -f docker-compose.dev.yml logs -f

# Specific service
docker-compose -f docker-compose.dev.yml logs -f postgresql
```

### Stop Environment

```bash
docker-compose -f docker-compose.dev.yml down
```

## Service Ports

| Service      | Host Port | Container Port |
| ------------ | --------- | -------------- |
| API Server   | 8080      | local process  |
| Worker HTTP  | 8081      | local process  |
| PostgreSQL   | 5433      | 5432           |
| NATS         | 14223     | 4222           |
| NATS Mon.    | 8223      | 8222           |
| Web Frontend | 3005      | local process  |

## Configuration Files

- `.env.example` - Environment variables template (copy to `.env`)
- `server.config.example.yaml` - Server config template
- `worker.config.example.yaml` - Worker config template

> `dev-setup.sh`, `dev-server.sh`, and `dev-worker.sh` will automatically copy `.example.yaml` to the corresponding `.config.yaml` if the config file does not exist. You'll be prompted to review and edit the config before the component starts.

## Architecture

The dev environment separates infrastructure from application services:

1. **Infrastructure** (docker-compose): PostgreSQL, NATS
2. **Application** (individual scripts): Server, Worker, Frontend

This allows developers to:

- Run infrastructure in containers
- Run application code locally for debugging
- Start/stop components independently

## Makefile Commands

From project root:

```bash
make dev-setup     # Initial setup
make dev-server    # Start server
make dev-worker    # Start worker
make dev-frontend  # Start frontend
```
