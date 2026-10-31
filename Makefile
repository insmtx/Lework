PROJECT ?= insmtx
APP?= lework
REGISTRY ?= registry.yygu.cn

.PHONY: build install uninstall docker-build-base docker-push-base docker-build docker-dev-build docker-push docker-compose-up docker-compose-down run run-foreground run-detached stop logs swagger swagger-clean dev-setup dev-server dev-worker dev-frontend dev-up dev-all dev-down dev-status dev-logs docker-build-worker-base docker-build-worker-base-private docker-push-worker-base docker-push-worker-base-private docker-build-worker docker-build-web docker-build-tag docker-push-tag image-tag aio-build aio-images aio-up aio-down aio-backup

# GO='GOOS=windows GOARCH=386 go'
VERSION := $(shell git describe --tags | sed 's/\(.*\)-.*/\1/')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo unsupported)
GO_VERSION := $(shell go version)
APP_VERSION := $(shell git describe --tags --abbrev=0)
BUILD_AT := $(shell date "+%Y-%m-%dT%H:%M:%S")
TIMESTAMP := $(shell date +%s)
IMAGE_TAG := ${VERSION}_${GIT_COMMIT}

BUILD_TAGS ?=
LDFLAGS := -s -w

ifneq ($(BUILD_TAGS),)
BUILD_TAGS_FLAG := -tags "$(BUILD_TAGS)"
endif

build:
	go build $(BUILD_TAGS_FLAG) -ldflags="$(LDFLAGS)" -v -o ./bundles/lework ./backend/cmd/lework/

install:
	bash deployments/dev/install.sh

uninstall:
	bash deployments/dev/install.sh --uninstall

docker-build-base:
	docker build -t $(REGISTRY)/$(PROJECT)/lework-base:latest -f deployments/build/Dockerfile.base .

docker-push-base: docker-build-base
	docker push $(REGISTRY)/$(PROJECT)/lework-base:latest

# Worker base: ubuntu + libreoffice + CJK fonts + node + claude-code/codex/opencode + lework user.
# Independent from lework-base; rebuild whenever Dockerfile.worker-base changes.
#
# 版次（Dockerfile.worker-base 顶层 ARG EDITION）：
#   docker-build-worker-base         WORKER_BASE_EDITION=saas     -> :saas     (SaaS 精简版，默认)
#   docker-build-worker-base-private  WORKER_BASE_EDITION=private -> :private  (私有化完整版)
# 不打 :latest 别名，避免 private/saas 共用 tag 在仓库里混淆；
# Dockerfile.worker 的 FROM 默认显式写 :saas，私有化部署需改成 :private。
# 也可显式指定：make docker-build-worker-base WORKER_BASE_EDITION=private
WORKER_BASE_EDITION ?= saas
WORKER_BASE_IMAGE ?= $(REGISTRY)/$(PROJECT)/lework-worker-base:$(WORKER_BASE_EDITION)

docker-build-worker-base:
	docker build --build-arg EDITION=$(WORKER_BASE_EDITION) -t $(REGISTRY)/$(PROJECT)/lework-worker-base:$(WORKER_BASE_EDITION) -f deployments/build/Dockerfile.worker-base .

docker-build-worker-base-private:
	$(MAKE) docker-build-worker-base WORKER_BASE_EDITION=private

docker-push-worker-base: docker-build-worker-base
	docker push $(REGISTRY)/$(PROJECT)/lework-worker-base:$(WORKER_BASE_EDITION)

docker-push-worker-base-private:
	$(MAKE) docker-push-worker-base WORKER_BASE_EDITION=private

docker-build:
	docker build -t $(REGISTRY)/$(PROJECT)/lework:latest -f deployments/build/Dockerfile.lework .

docker-build-worker:
	docker build --build-arg WORKER_BASE_IMAGE=$(WORKER_BASE_IMAGE) -t $(REGISTRY)/$(PROJECT)/worker:latest -f deployments/build/Dockerfile.worker .

docker-build-web:
	docker build -t $(REGISTRY)/$(PROJECT)/web:latest -f deployments/build/Dockerfile.web $(DOCKER_BUILD_ARGS) .

# ── 一体机（AIO）──────────────────────────────────────────────────────────────
# 一体机用 worker 镜像同时跑 server 与内置 worker；Web 以 /v1 同源构建。
AIO_DIR ?= deployments/aio
AIO_IMAGE ?= $(REGISTRY)/$(PROJECT)/worker:latest
AIO_WEB_IMAGE ?= $(REGISTRY)/$(PROJECT)/web:latest

aio-build: docker-build-worker-base docker-build-worker
	docker build -t $(AIO_WEB_IMAGE) \
		--build-arg NEXT_PUBLIC_LEWORK_API_BASE_URL=/v1 \
		-f deployments/build/Dockerfile.web .

aio-images: aio-build
	docker pull postgres:17 nats:2.12 nginx:1.27-alpine
	docker save -o $(AIO_DIR)/lework-aio-images.tar \
		$(AIO_IMAGE) $(AIO_WEB_IMAGE) postgres:17 nats:2.12 nginx:1.27-alpine

aio-up:
	cd $(AIO_DIR) && ./install.sh

aio-down:
	cd $(AIO_DIR) && ./uninstall.sh

aio-backup:
	cd $(AIO_DIR) && ./backup.sh


# SERVICE: 服务名，默认使用 APP
# DOCKERFILE_NAME: Dockerfile 文件名（去掉 lework- 前缀），如 lework -> lework, lework-worker -> worker
SERVICE ?= $(APP)
DOCKERFILE_NAME = $(subst lework-,,$(SERVICE))

docker-build-tag:
	@if [ -z "$(SERVICE)" ]; then \
		echo "ERROR: SERVICE is not set. Usage: make docker-build-tag SERVICE=lework"; \
		exit 1; \
	fi
	docker build \
		--build-arg BUILD_TAGS=$(BUILD_TAGS) \
		-t $(REGISTRY)/$(PROJECT)/$(SERVICE):${IMAGE_TAG} \
		-f deployments/build/Dockerfile.$(DOCKERFILE_NAME) \
	    $(DOCKER_BUILD_ARGS) \
		.

push-image: docker-build-tag docker-push-tag

docker-push-tag:
	docker push $(REGISTRY)/$(PROJECT)/$(SERVICE):${IMAGE_TAG}

# 获取当前 IMAGE_TAG 供 CI 使用
image-tag:
	@echo ${IMAGE_TAG}

docker-dev-build:
	docker build -t $(REGISTRY)/$(PROJECT)/lework-dev:latest -f deployments/build/Dockerfile.lework-dev .

docker-push: docker-build
	docker push $(REGISTRY)/$(PROJECT)/lework:latest

docker-run-lework:
	-docker rm -f $(PROJECT)-lework-dev
	docker run -d --name $(PROJECT)-lework-dev -p 8080:8080 $(REGISTRY)/$(PROJECT)/lework:latest

docker-compose-up: docker-build
	docker tag $(REGISTRY)/$(PROJECT)/lework:latest localhost/env_$(PROJECT):latest
	docker-compose -f deployments/env/docker-compose.yml up -d

docker-compose-down:
	docker-compose -f deployments/env/docker-compose.yml down

.PHONY: run run-foreground run-detached run-build run-foreground-build run-detached-build stop logs

# Default run command - runs docker-compose services in foreground mode (shows logs)
run:
	docker-compose -f deployments/env/docker-compose.yml up

# Alternative for explicit foreground mode
run-foreground:
	docker-compose -f deployments/env/docker-compose.yml up

# Run services in foreground with forced rebuild 
run-build:
	docker-compose -f deployments/env/docker-compose.yml up --build

# Alternative for explicit foreground with forced rebuild
run-foreground-build:
	docker-compose -f deployments/env/docker-compose.yml up --build

# Run services in detached mode (background)
run-detached:
	docker-compose -f deployments/env/docker-compose.yml up -d

# Run services in detached mode with forced build
run-detached-build:
	docker-compose -f deployments/env/docker-compose.yml up -d --build

# Stop services  
stop:
	docker-compose -f deployments/env/docker-compose.yml down

# View service logs
logs:
	docker-compose -f deployments/env/docker-compose.yml logs -f

# Swagger 文档生成
.PHONY: swagger swagger-clean

swagger:
	swag init --generalInfo server.go \
		--dir backend/cmd/lework,backend/internal/api/handler,backend/internal/api,backend/types \
		--output docs/swagger --exclude example \
		--parseDependency --parseDepth 2

swagger-clean:
	rm -rf docs/swagger

.PHONY: dev-setup dev-server dev-worker dev-frontend

dev-setup:
	cd deployments/dev && ./dev-setup.sh

dev-server:
	cd deployments/dev && ./dev-server.sh

dev-worker:
	cd deployments/dev && ./dev-worker.sh

dev-frontend:
	-docker rm -f lework-dev-frontend || true
	docker run -it --name lework-dev-frontend \
	 --network host \
	 -v $(PWD)/frontend:/app \
	 -w /app \
	 registry.yygu.cn/base/node:24 bash 

# 一键重启本地开发栈：server + worker + frontend（后台运行，日志在 deployments/dev/logs）。
# 会先停掉旧的 dev 进程并清理端口；.next 超阈值或前端无响应时自动清缓存重启。
# 前置条件：PostgreSQL/NATS 已就绪（沿用 server/worker config），opencode 2.1.x 在 PATH。
dev-up:
	cd deployments/dev && ./dev-all.sh start

# dev-all 作为 dev-up 的别名，便于记忆
dev-all: dev-up

# 停止 dev-up 启动的进程（只按 pid 文件停自己启动的，不动外部进程）
dev-down:
	cd deployments/dev && ./dev-all.sh stop


dev-status:
	cd deployments/dev && ./dev-all.sh status

dev-logs:
	cd deployments/dev && ./dev-all.sh logs
