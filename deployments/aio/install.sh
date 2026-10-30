#!/usr/bin/env bash
# Lework 一体机安装脚本。
#
# 设计目标：一台干净的 Linux + Docker，执行 ./install.sh 后即可访问。
# 数据全部落在本目录 ./data，升级/备份/迁移不依赖容器内部状态。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

ENV_FILE="$SCRIPT_DIR/.env"
CONFIG_DIR="$SCRIPT_DIR/config"
DATA_DIR="$SCRIPT_DIR/data/app"

log() { printf '\033[1;32m[aio]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[aio]\033[0m %s\n' "$*" >&2; }
die() { printf '\033[1;31m[aio]\033[0m %s\n' "$*" >&2; exit 1; }

BUILD=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --build) BUILD=1; shift ;;
    -h|--help)
      cat <<'EOF'
用法: ./install.sh [--build]

  --build   从当前源码构建镜像（需要 Docker + 构建网络；离线部署请先 docker load）

环境变量（可选）:
  LEROS_HTTP_PORT        对外 HTTP 端口，默认 80
  LEROS_BIND_ADDR        监听地址，默认 0.0.0.0
  LEROS_PUBLIC_BASE_URL  对外访问地址，用于文件预览/下载链接
  LEROS_ADMIN_EMAIL      管理员邮箱，默认 admin@leros.local
  LEROS_ADMIN_PASSWORD   管理员初始密码；不填则自动生成
  LEROS_IMAGE            Server/Worker 镜像
  LEROS_WEB_IMAGE        Web 镜像
EOF
      exit 0
      ;;
    *) die "未知参数: $1" ;;
  esac
done

command -v docker >/dev/null 2>&1 || die "未找到 docker，请先安装 Docker Engine"
docker compose version >/dev/null 2>&1 || die "未找到 docker compose v2"

gen_secret() {
  local bytes="${1:-24}"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    head -c "$bytes" /dev/urandom | od -An -tx1 | tr -d ' \n'
  fi
}

detect_host() {
  if [[ -n "${LEROS_PUBLIC_HOST:-}" ]]; then
    printf '%s' "$LEROS_PUBLIC_HOST"
    return
  fi
  local ip
  ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
  printf '%s' "${ip:-localhost}"
}

public_base_url() {
  local host port scheme
  host="$(detect_host)"
  port="${LEROS_HTTP_PORT:-80}"
  scheme="${LEROS_PUBLIC_SCHEME:-http}"
  if [[ "$scheme" == "https" || "$port" == "80" ]]; then
    printf '%s://%s' "$scheme" "$host"
  else
    printf '%s://%s:%s' "$scheme" "$host" "$port"
  fi
}

validate_env_value() {
  local name="$1" value="$2"
  [[ "$value" =~ ^[A-Za-z0-9._@:/?=+!#%_-]+$ ]] || die "$name 含不支持的字符，请使用字母/数字/._@:/?=+!#%_-"
}

if [[ ! -f "$ENV_FILE" ]]; then
  log "首次安装：生成 .env 与随机密钥"

  ADMIN_EMAIL="${LEROS_ADMIN_EMAIL:-admin@leros.local}"
  ADMIN_PASSWORD="${LEROS_ADMIN_PASSWORD:-$(gen_secret 12)}"
  DB_PASSWORD="$(gen_secret 16)"
  JWT_SECRET="$(gen_secret 32)"
  STORAGE_SECRET="$(gen_secret 32)"
  PUBLIC_BASE_URL="${LEROS_PUBLIC_BASE_URL:-$(public_base_url)}"

  validate_env_value "LEROS_ADMIN_EMAIL" "$ADMIN_EMAIL"
  validate_env_value "LEROS_ADMIN_PASSWORD" "$ADMIN_PASSWORD"
  validate_env_value "LEROS_PUBLIC_BASE_URL" "$PUBLIC_BASE_URL"

  cat >"$ENV_FILE" <<EOF
LEROS_BIND_ADDR=${LEROS_BIND_ADDR:-0.0.0.0}
LEROS_HTTP_PORT=${LEROS_HTTP_PORT:-80}
LEROS_PUBLIC_BASE_URL=$PUBLIC_BASE_URL
LEROS_IMAGE=${LEROS_IMAGE:-registry.yygu.cn/insmtx/worker:latest}
LEROS_WEB_IMAGE=${LEROS_WEB_IMAGE:-registry.yygu.cn/insmtx/web:latest}
LEROS_POSTGRES_IMAGE=${LEROS_POSTGRES_IMAGE:-postgres:17}
LEROS_NATS_IMAGE=${LEROS_NATS_IMAGE:-nats:2.12}
LEROS_NGINX_IMAGE=${LEROS_NGINX_IMAGE:-nginx:1.27-alpine}
LEROS_ADMIN_EMAIL=$ADMIN_EMAIL
LEROS_ADMIN_PASSWORD=$ADMIN_PASSWORD
LEROS_DB_NAME=${LEROS_DB_NAME:-leros}
LEROS_DB_USER=${LEROS_DB_USER:-leros}
LEROS_DB_PASSWORD=$DB_PASSWORD
LEROS_JWT_SECRET=$JWT_SECRET
LEROS_STORAGE_SECRET=$STORAGE_SECRET
TZ=${TZ:-Asia/Shanghai}
EOF
  chmod 600 "$ENV_FILE"
else
  log "检测到已有 .env，保留现有配置"
fi

# shellcheck disable=SC1091
set -a
source "$ENV_FILE"
set +a

mkdir -p "$DATA_DIR/storage" "$DATA_DIR/workspace" "$DATA_DIR/logs" "$SCRIPT_DIR/data/backups" "$SCRIPT_DIR/data/postgres-backup"

# 离线包优先：把镜像 tar 放在本目录即可自动导入。
if [[ -f "$SCRIPT_DIR/leros-aio-images.tar" ]]; then
  log "导入离线镜像 leros-aio-images.tar"
  docker load -i "$SCRIPT_DIR/leros-aio-images.tar"
fi

# 容器内 worker 用户为 leros（uid 1000）；宿主机目录需可写。
if [[ "$(id -u)" == "0" ]]; then
  chown -R 1000:1000 "$DATA_DIR" || true
else
  warn "当前不是 root，若容器报权限错误，请执行: sudo chown -R 1000:1000 $DATA_DIR"
fi

render() {
  local src="$1" dst="$2"
  sed \
    -e "s|__DB_USER__|${LEROS_DB_USER}|g" \
    -e "s|__DB_PASSWORD__|${LEROS_DB_PASSWORD}|g" \
    -e "s|__DB_NAME__|${LEROS_DB_NAME}|g" \
    -e "s|__JWT_SECRET__|${LEROS_JWT_SECRET}|g" \
    -e "s|__STORAGE_SECRET__|${LEROS_STORAGE_SECRET}|g" \
    -e "s|__PUBLIC_BASE_URL__|${LEROS_PUBLIC_BASE_URL}|g" \
    "$src" >"$dst"
}

log "渲染配置"
render "$CONFIG_DIR/server.config.yaml.tpl" "$CONFIG_DIR/server.config.yaml"
render "$CONFIG_DIR/worker.config.yaml.tpl" "$CONFIG_DIR/worker.config.yaml"

if [[ "$BUILD" == "1" ]]; then
  log "构建镜像（首次构建耗时较长）"
  docker compose build
fi

log "启动服务"
docker compose up -d --no-build --remove-orphans

log "等待服务就绪"
HEALTH_URL="http://127.0.0.1:${LEROS_HTTP_PORT}/healthz"
READY=0
for _ in $(seq 1 90); do
  if command -v curl >/dev/null 2>&1; then
    if curl -fsS "$HEALTH_URL" >/dev/null 2>&1; then READY=1; break; fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -q -O - "$HEALTH_URL" >/dev/null 2>&1; then READY=1; break; fi
  else
    if docker compose exec -T gateway wget -q -O - http://127.0.0.1/healthz >/dev/null 2>&1; then READY=1; break; fi
  fi
  sleep 2
done

HOST="$(detect_host)"
if [[ "${LEROS_HTTP_PORT}" == "80" ]]; then
  ACCESS_URL="http://${HOST}"
else
  ACCESS_URL="http://${HOST}:${LEROS_HTTP_PORT}"
fi

if [[ "$READY" != "1" ]]; then
  warn "服务在 180 秒内未通过健康检查，请查看日志：docker compose logs -f leros"
else
  log "服务已就绪"
fi

cat <<EOF

============================================================
  Lework 一体机
============================================================
  访问地址 : ${ACCESS_URL}
  管理员   : ${LEROS_ADMIN_EMAIL}
  初始密码 : ${LEROS_ADMIN_PASSWORD}
  数据目录 : ${SCRIPT_DIR}/data
  配置文件 : ${SCRIPT_DIR}/config

  首次登录后请立即修改管理员密码。
  查看状态 : docker compose ps
  查看日志 : docker compose logs -f leros
  备份     : ./backup.sh
============================================================

EOF
