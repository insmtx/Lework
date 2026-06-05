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
| NATS         | 4223      | 4222           |
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
