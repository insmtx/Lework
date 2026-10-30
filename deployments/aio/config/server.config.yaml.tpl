env: production
workspace_root: /data/workspace

server:
  port: 8080
  jwt:
    secret: "__JWT_SECRET__"

automation_scheduler:
  enabled: true
  planner_interval: 30

database:
  url: "postgres://__DB_USER__:__DB_PASSWORD__@postgres:5432/__DB_NAME__?sslmode=disable"
  debug: false

nats:
  url: "nats://nats:4222"

log:
  level: info

logger:
  default:
    - writer: console
      level: info
    - writer: file
      level: info
      filename: /app/logs/leros-server.log
      maxsize: 20
      maxage: 14
      maxbackups: 5
      localtime: true
      compress: true

storage:
  driver: local
  local_dir: /data/storage
  bucket: leros
  sign_secret: "__STORAGE_SECRET__"
  base_url: "__PUBLIC_BASE_URL__"

# 一体机不在容器外再起 worker：server 通过 process scheduler 在容器内拉起
# /leros worker，复用 worker 镜像中的文档/浏览器/CLI 工具链。
scheduler:
  mode: process
  worker_binary: /leros
  working_dir: /app
  worker_config: /app/config/worker.config.yaml
  server_addr: 127.0.0.1:8080
  env:
    LEROS_WORKER_LISTEN_ADDR: 127.0.0.1:18181
    LEROS_WORKSPACE_ROOT: /data/workspace
