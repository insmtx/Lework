#!/usr/bin/env bash
# 停止一体机。默认保留数据；--purge 会删除数据卷与 ./data。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

PURGE=0
while [[ $# -gt 0 ]]; do
  case "$1" in
    --purge) PURGE=1; shift ;;
    -h|--help) echo "用法: $0 [--purge]"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

echo "[aio] 停止服务"
docker compose down --remove-orphans

if [[ "$PURGE" == "1" ]]; then
  echo "[aio] 删除数据卷与本地数据"
  docker compose down -v --remove-orphans
  rm -rf "$SCRIPT_DIR/data"
  rm -f "$SCRIPT_DIR/.env" "$SCRIPT_DIR/config/server.config.yaml" "$SCRIPT_DIR/config/worker.config.yaml"
  echo "[aio] 已清理"
else
  echo "[aio] 数据保留在 $SCRIPT_DIR/data，重新启动执行 ./install.sh"
fi
