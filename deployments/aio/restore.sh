#!/usr/bin/env bash
# 恢复备份。用法:
#   ./restore.sh data/backups/leros-db-20260101-120000.sql.gz [data/backups/leros-app-....tar.gz]
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

[[ -f .env ]] || { echo "缺少 .env，请先执行 ./install.sh" >&2; exit 1; }
[[ $# -ge 1 ]] || { echo "用法: $0 <db.sql.gz> [app.tar.gz]" >&2; exit 1; }
[[ -f "$1" ]] || { echo "备份文件不存在: $1" >&2; exit 1; }

DB_FILE="$1"
APP_FILE="${2:-}"

# shellcheck disable=SC1091
set -a; source .env; set +a

echo "[aio] 停止应用（保留 PostgreSQL/NATS）"
docker compose stop leros web gateway

echo "[aio] 恢复数据库"
gunzip -c "$DB_FILE" | docker compose exec -T postgres psql -U "$LEROS_DB_USER" -d "$LEROS_DB_NAME"

if [[ -n "$APP_FILE" ]]; then
  [[ -f "$APP_FILE" ]] || { echo "工作区备份不存在: $APP_FILE" >&2; exit 1; }
  echo "[aio] 恢复工作区与文件"
  tar -xzf "$APP_FILE" -C "$SCRIPT_DIR/data/app"
  if [[ "$(id -u)" == "0" ]]; then
    chown -R 1000:1000 "$SCRIPT_DIR/data/app" || true
  fi
fi

echo "[aio] 启动应用"
docker compose start web gateway
docker compose up -d --no-build leros

echo "[aio] 恢复完成，请验证: docker compose ps"
