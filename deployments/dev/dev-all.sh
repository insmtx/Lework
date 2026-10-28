#!/usr/bin/env bash
# 一键启停本地开发栈（server + worker + frontend）。
# 基础设施（PostgreSQL/NATS）沿用 deployments/dev/*.config.yaml 指向的实例，
# 本脚本不负责启动 docker-compose。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_DIR="$SCRIPT_DIR/logs"
PID_DIR="$LOG_DIR/pids"

SERVER_PORT="${DEV_SERVER_PORT:-8080}"
WORKER_PORT="${DEV_WORKER_PORT:-8081}"
WEB_PORT="${DEV_WEB_PORT:-3005}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

if [ -f "$SCRIPT_DIR/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  source "$SCRIPT_DIR/.env"
  set +a
fi

port_busy() { nc -z 127.0.0.1 "$1" >/dev/null 2>&1; }
pid_alive() { [ -f "$PID_DIR/$1.pid" ] && kill -0 "$(cat "$PID_DIR/$1.pid")" 2>/dev/null; }

kill_tree() {
  local pid="$1" child
  for child in $(pgrep -P "$pid" 2>/dev/null || true); do
    kill_tree "$child"
  done
  kill "$pid" 2>/dev/null || true
}

start_bg() {
  local name="$1" cmd="$2"
  nohup bash -c "cd \"$ROOT_DIR\" && $cmd" >"$LOG_DIR/$name.log" 2>&1 &
  echo $! >"$PID_DIR/$name.pid"
}

start_one() {
  local name="$1" port="$2" cmd="$3"
  if pid_alive "$name"; then
    echo -e "${YELLOW}$name 已在运行 (pid $(cat "$PID_DIR/$name.pid"))${NC}"
    return 0
  fi
  if port_busy "$port"; then
    echo -e "${YELLOW}:$port 已被其他进程占用，跳过 $name${NC}"
    return 0
  fi
  echo -e "${BLUE}启动 $name (:${port})${NC}"
  start_bg "$name" "$cmd"
}

wait_port() {
  local name="$1" port="$2" timeout="$3" i
  for ((i = 0; i < timeout; i++)); do
    if port_busy "$port"; then
      return 0
    fi
    sleep 1
  done
  echo -e "${RED}$name 未在 ${timeout}s 内监听 :$port${NC}"
  tail -n 40 "$LOG_DIR/$name.log" 2>/dev/null || true
  return 1
}

cmd_start() {
  mkdir -p "$PID_DIR"

  # 先编译一次，避免 server/worker 同时启动时争抢构建。
  if ! pid_alive server && ! port_busy "$SERVER_PORT"; then
    echo -e "${BLUE}编译 bundles/leros ...${NC}"
    (cd "$ROOT_DIR" && go build -o ./bundles/leros ./backend/cmd/leros/)
  fi

  start_one server "$SERVER_PORT" "./deployments/dev/dev-server.sh"
  start_one worker "$WORKER_PORT" "./deployments/dev/dev-worker.sh"
  if command -v pnpm >/dev/null 2>&1; then
    start_one web "$WEB_PORT" "cd frontend && pnpm run dev:web"
  else
    start_one web "$WEB_PORT" "cd frontend && npm run dev:web"
  fi

  local failed=0
  wait_port server "$SERVER_PORT" 120 || failed=1
  wait_port worker "$WORKER_PORT" 120 || failed=1
  wait_port web "$WEB_PORT" 240 || failed=1

  echo ""
  if [ "$failed" != 0 ]; then
    echo -e "${RED}部分服务启动失败，日志在 $LOG_DIR${NC}"
    exit 1
  fi
  echo -e "${GREEN}Leros dev stack is up${NC}"
  echo -e "  Web:    ${BLUE}http://localhost:${WEB_PORT}${NC}"
  echo -e "  API:    ${BLUE}http://localhost:${SERVER_PORT}/v1${NC}"
  echo -e "  Worker: ${BLUE}http://localhost:${WORKER_PORT}/health${NC}"
  echo -e "  Logs:   $LOG_DIR (make dev-logs)"
}

cmd_stop() {
  local name i
  for name in web worker server; do
    if pid_alive "$name"; then
      echo -e "${BLUE}停止 $name (pid $(cat "$PID_DIR/$name.pid"))${NC}"
      kill_tree "$(cat "$PID_DIR/$name.pid")"
    fi
    rm -f "$PID_DIR/$name.pid"
  done
  for ((i = 0; i < 10; i++)); do
    if ! port_busy "$SERVER_PORT" && ! port_busy "$WORKER_PORT" && ! port_busy "$WEB_PORT"; then
      break
    fi
    sleep 1
  done
  if port_busy "$SERVER_PORT" || port_busy "$WORKER_PORT" || port_busy "$WEB_PORT"; then
    echo -e "${YELLOW}仍有端口被占用（可能不是 dev-up 启动的进程）:${NC}"
    for name in "$SERVER_PORT" "$WORKER_PORT" "$WEB_PORT"; do
      port_busy "$name" && echo "  :$name"
    done
    return 1
  fi
  echo -e "${GREEN}dev stack stopped${NC}"
}

cmd_status() {
  local spec name port state
  for spec in "server:$SERVER_PORT" "worker:$WORKER_PORT" "web:$WEB_PORT"; do
    name="${spec%%:*}"; port="${spec##*:}"
    if port_busy "$port"; then state="${GREEN}up${NC}"; else state="${RED}down${NC}"; fi
    printf "%-7s :%-5s %b" "$name" "$port" "$state"
    if pid_alive "$name"; then printf "  pid=%s" "$(cat "$PID_DIR/$name.pid")"; fi
    printf "\n"
  done
}

cmd_logs() {
  local name
  for name in server worker web; do
    echo -e "${BLUE}===== $name ($LOG_DIR/$name.log) =====${NC}"
    tail -n 30 "$LOG_DIR/$name.log" 2>/dev/null || echo "(no log yet)"
  done
}

case "${1:-start}" in
  start) cmd_start ;;
  stop) cmd_stop ;;
  status) cmd_status ;;
  logs) cmd_logs ;;
  *) echo "usage: $0 {start|stop|status|logs}" >&2; exit 2 ;;
esac
