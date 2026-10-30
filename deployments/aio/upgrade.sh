#!/usr/bin/env bash
# 升级一体机：拉取/加载新镜像后滚动重启。数据库迁移由 server 启动时自动执行。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

[[ -f .env ]] || { echo "缺少 .env，请先执行 ./install.sh" >&2; exit 1; }

# shellcheck disable=SC1091
set -a; source .env; set +a

if [[ "${1:-}" == "--build" ]]; then
  echo "[aio] 从源码构建镜像"
  docker compose build
fi

if [[ -f "$SCRIPT_DIR/leros-aio-images.tar" ]]; then
  echo "[aio] 导入离线镜像"
  docker load -i "$SCRIPT_DIR/leros-aio-images.tar"
else
  echo "[aio] 拉取镜像（离线环境可忽略失败）"
  docker compose pull || true
fi

echo "[aio] 重启服务"
docker compose up -d --no-build --remove-orphans

echo "[aio] 等待健康检查"
for _ in $(seq 1 60); do
  if curl -fsS "http://127.0.0.1:${LEROS_HTTP_PORT}/healthz" >/dev/null 2>&1; then
    echo "[aio] 升级完成"
    exit 0
  fi
  sleep 2
done

echo "[aio] 健康检查未通过，请查看: docker compose logs -f leros" >&2
exit 1
