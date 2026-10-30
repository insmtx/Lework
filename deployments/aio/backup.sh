#!/usr/bin/env bash
# 备份数据库与项目工作区/文件。默认写到 ./data/backups。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

[[ -f .env ]] || { echo "缺少 .env，请先执行 ./install.sh" >&2; exit 1; }

# shellcheck disable=SC1091
set -a; source .env; set +a

TS="$(date +%Y%m%d-%H%M%S)"
OUT_DIR="$SCRIPT_DIR/data/backups"
mkdir -p "$OUT_DIR"

DB_FILE="$OUT_DIR/leros-db-$TS.sql.gz"
APP_FILE="$OUT_DIR/leros-app-$TS.tar.gz"

echo "[aio] 备份数据库 -> $DB_FILE"
docker compose exec -T -e PGPASSWORD="$LEROS_DB_PASSWORD" postgres \
  pg_dump -U "$LEROS_DB_USER" --clean --if-exists "$LEROS_DB_NAME" | gzip >"$DB_FILE"

echo "[aio] 备份工作区与文件 -> $APP_FILE"
tar -czf "$APP_FILE" -C "$SCRIPT_DIR/data/app" storage workspace

echo "[aio] 完成"
printf '  %s\n  %s\n' "$DB_FILE" "$APP_FILE"
