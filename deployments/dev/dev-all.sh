#!/usr/bin/env bash
# 一键重启本地开发栈（server + worker + frontend）。
# 基础设施（PostgreSQL/NATS）沿用 deployments/dev/*.config.yaml 指向的实例。
#
# 启动前：停掉自己启动的进程与占用端口的 dev 残留进程。
# 启动后：检测 .next 缓存是否异常增大；前端 HTTP 探活失败时清缓存并重启一次。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_DIR="$SCRIPT_DIR/logs"
PID_DIR="$LOG_DIR/pids"
NEXT_DIR="$ROOT_DIR/frontend/apps/web/.next"

SERVER_PORT="${DEV_SERVER_PORT:-8080}"
WORKER_PORT="${DEV_WORKER_PORT:-8081}"
WEB_PORT="${DEV_WEB_PORT:-3005}"
NEXT_CACHE_MAX_MB="${DEV_NEXT_CACHE_MAX_MB:-512}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'

if [ -f "$SCRIPT_DIR/.env" ]; then
  set -a
  # shellcheck disable=SC1091
  source "$SCRIPT_DIR/.env"
  set +a
fi

port_busy() { nc -z 127.0.0.1 "$1" >/dev/null 2>&1; }
pid_alive() { [ -f "$PID_DIR/$1.pid" ] && kill -0 "$(cat "$PID_DIR/$1.pid")" 2>/dev/null; }
port_pids() { command -v lsof >/dev/null 2>&1 && lsof -tiTCP:"$1" -sTCP:LISTEN 2>/dev/null || true; }

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

stop_tracked() {
  local name
  for name in web worker server; do
    if pid_alive "$name"; then
      echo -e "${BLUE}停止 $name (pid $(cat "$PID_DIR/$name.pid"))${NC}"
      kill_tree "$(cat "$PID_DIR/$name.pid")"
    fi
    rm -f "$PID_DIR/$name.pid"
  done
}

wait_ports_free() {
  local i
  for ((i = 0; i < 10; i++)); do
    if ! port_busy "$SERVER_PORT" && ! port_busy "$WORKER_PORT" && ! port_busy "$WEB_PORT"; then
      return 0
    fi
    sleep 1
  done
  return 1
}

# 停掉端口上的残留进程；只杀 dev 相关进程，避免误杀其他应用。
reap_port() {
  local name="$1" port="$2" pid cmd
  for pid in $(port_pids "$port"); do
    cmd="$(ps -o command= -p "$pid" 2>/dev/null || true)"
    case "$cmd" in
      *leros*|*next*|*turbo*|*pnpm*)
        echo -e "${YELLOW}清理 :$port 残留 $name (pid $pid)${NC}"
        kill_tree "$pid"
        ;;
      *)
        echo -e "${RED}:$port 被非 dev 进程占用，拒绝自动清理：${NC}$cmd"
        return 1
        ;;
    esac
  done
  return 0
}

next_cache_mb() {
  du -sm "$NEXT_DIR" 2>/dev/null | awk '{print $1+0}'
}

clean_next_cache() {
  if [ -d "$NEXT_DIR" ]; then
    echo -e "${BLUE}清理 Next 缓存 $NEXT_DIR${NC}"
    rm -rf "$NEXT_DIR"
  fi
}

maybe_clean_next_cache() {
  local mb
  mb="$(next_cache_mb)"
  mb="${mb:-0}"
  if [ "$mb" -ge "$NEXT_CACHE_MAX_MB" ]; then
    echo -e "${YELLOW}.next 已达 ${mb}MB（阈值 ${NEXT_CACHE_MAX_MB}MB），启动前清理${NC}"
    clean_next_cache
  fi
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

wait_http() {
  local port="$1" attempts="$2" i code
  for ((i = 0; i < attempts; i++)); do
    code="$(curl -s -o /dev/null -m 5 -w '%{http_code}' "http://127.0.0.1:$port/" 2>/dev/null || true)"
    if [ -n "$code" ] && [ "$code" != "000" ]; then
      return 0
    fi
    sleep 1
  done
  return 1
}

cmd_start() {
  mkdir -p "$PID_DIR"

  echo -e "${BLUE}停止旧实例...${NC}"
  stop_tracked
  reap_port server "$SERVER_PORT" || exit 1
  reap_port worker "$WORKER_PORT" || exit 1
  reap_port web "$WEB_PORT" || exit 1
  if ! wait_ports_free; then
    echo -e "${RED}端口仍被占用，无法重启${NC}"
    exit 1
  fi
  # 给 NATS durable consumer 一点退订时间，避免新 worker 启动即报 already bound。
  sleep 2

  maybe_clean_next_cache

  # 先编译一次，避免 server/worker 同时启动时争抢构建。
  echo -e "${BLUE}编译 bundles/leros ...${NC}"
  (cd "$ROOT_DIR" && go build -o ./bundles/leros ./backend/cmd/leros/)

  echo -e "${BLUE}启动 server (:${SERVER_PORT})${NC}"
  start_bg server "./deployments/dev/dev-server.sh"
  echo -e "${BLUE}启动 worker (:${WORKER_PORT})${NC}"
  start_bg worker "./deployments/dev/dev-worker.sh"
  if command -v pnpm >/dev/null 2>&1; then
    echo -e "${BLUE}启动 frontend (:${WEB_PORT})${NC}"
    start_bg web "cd frontend && pnpm run dev:web"
  else
    echo -e "${BLUE}启动 frontend (:${WEB_PORT})${NC}"
    start_bg web "cd frontend && npm run dev:web"
  fi

  local failed=0
  wait_port server "$SERVER_PORT" 120 || failed=1
  wait_port worker "$WORKER_PORT" 120 || failed=1
  wait_port web "$WEB_PORT" 240 || failed=1
  if [ "$failed" != 0 ]; then
    echo -e "${RED}部分服务启动失败，日志在 $LOG_DIR${NC}"
    exit 1
  fi

  # 端口监听不等于能出页面：Next 缓存异常时会出现「端口在、请求挂死」。
  if ! wait_http "$WEB_PORT" 12; then
    echo -e "${YELLOW}前端端口已监听但 HTTP 无响应，清理 .next 后重启一次${NC}"
    if pid_alive web; then kill_tree "$(cat "$PID_DIR/web.pid")"; fi
    rm -f "$PID_DIR/web.pid"
    for _ in $(seq 1 10); do port_busy "$WEB_PORT" || break; sleep 1; done
    clean_next_cache
    if command -v pnpm >/dev/null 2>&1; then
      start_bg web "cd frontend && pnpm run dev:web"
    else
      start_bg web "cd frontend && npm run dev:web"
    fi
    wait_port web "$WEB_PORT" 240 || { echo -e "${RED}前端重启失败${NC}"; exit 1; }
    wait_http "$WEB_PORT" 40 || { echo -e "${RED}前端清理缓存后仍无响应，见 $LOG_DIR/web.log${NC}"; exit 1; }
  fi

  echo ""
  echo -e "${GREEN}Leros dev stack is up${NC}"
  echo -e "  Web:    ${BLUE}http://localhost:${WEB_PORT}${NC}"
  echo -e "  API:    ${BLUE}http://localhost:${SERVER_PORT}/v1${NC}"
  echo -e "  Worker: ${BLUE}http://localhost:${WORKER_PORT}/health${NC}"
  echo -e "  Logs:   $LOG_DIR (make dev-logs)"
  echo -e "  Stop:   make dev-stop"
}

cmd_stop() {
  stop_tracked
  for ((i = 0; i < 10; i++)); do
    if ! port_busy "$SERVER_PORT" && ! port_busy "$WORKER_PORT" && ! port_busy "$WEB_PORT"; then
      echo -e "${GREEN}dev stack stopped${NC}"
      return 0
    fi
    sleep 1
  done
  echo -e "${YELLOW}仍有端口被占用（可能不是 dev-up 启动的进程）:${NC}"
  local spec
  for spec in "$SERVER_PORT" "$WORKER_PORT" "$WEB_PORT"; do
    port_busy "$spec" && echo "  :$spec"
  done
  return 1
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
