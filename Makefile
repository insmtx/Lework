PROJECT ?= insmtx
APP?= leros
REGISTRY ?= registry.yygu.cn

.PHONY: build install uninstall docker-build-base docker-push-base docker-build docker-dev-build docker-push docker-compose-up docker-compose-down run run-foreground run-detached stop logs swagger swagger-clean dev-setup dev-server dev-worker dev-frontend

# GO='GOOS=windows GOARCH=386 go'
VERSION := $(shell git describe --tags | sed 's/\(.*\)-.*/\1/')
GIT_COMMIT := $(shell git rev-parse --short HEAD || echo unsupported)
GO_VERSION := $(shell go version)
APP_VERSION := $(shell git describe --tags --abbrev=0)
BUILD_AT := $(shell date "+%Y-%m-%dT%H:%M:%S")
TIMESTAMP := $(shell date +%s)

IMAGE_TAG := ${VERSION}_${GIT_COMMIT}




build:
	go build -v -o ./bundles/leros ./backend/cmd/leros/

install:
	bash deployments/dev/install.sh

uninstall:
	bash deployments/dev/install.sh --uninstall

docker-build-base:
	docker build -t $(REGISTRY)/$(PROJECT)/base:latest -f deployments/build/Dockerfile.base .

docker-push-base: docker-build-base
	docker push $(REGISTRY)/$(PROJECT)/base:latest

docker-build:
	docker build -t $(REGISTRY)/$(PROJECT)/leros:latest -f deployments/build/Dockerfile.leros .

docker-build-worker:
	docker build -t $(REGISTRY)/$(PROJECT)/worker:latest -f deployments/build/Dockerfile.worker .

docker-build-web:
	docker build -t $(REGISTRY)/$(PROJECT)/web:latest -f deployments/build/Dockerfile.web $(DOCKER_BUILD_ARGS) .


# SERVICE: 服务名，默认使用 APP
# DOCKERFILE_NAME: Dockerfile 文件名（去掉 lerost- 前缀），如 leros -> leros, leros-worker -> worker
SERVICE ?= $(APP)
DOCKERFILE_NAME = $(subst leros-,,$(SERVICE))

docker-build-tag:
	@if [ -z "$(SERVICE)" ]; then \
		echo "ERROR: SERVICE is not set. Usage: make docker-build-tag SERVICE=leros"; \
		exit 1; \
	fi
	docker build \
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
	docker build -t $(REGISTRY)/$(PROJECT)/leros-dev:latest -f deployments/build/Dockerfile.leros-dev .

docker-push: docker-build
	docker push $(REGISTRY)/$(PROJECT)/leros:latest

docker-run-leros:
	-docker rm -f $(PROJECT)-leros-dev
	docker run -d --name $(PROJECT)-leros-dev -p 8080:8080 $(REGISTRY)/$(PROJECT)/leros:latest

docker-compose-up: docker-build
	docker tag $(REGISTRY)/$(PROJECT)/leros:latest localhost/env_$(PROJECT):latest
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
	swag init --generalInfo server.go --dir backend/cmd/leros,backend/internal/api/handler,backend/internal/api,backend/types --output docs/swagger --exclude example

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
	-docker rm -f leros-dev-frontend || true
	docker run -it --name leros-dev-frontend \
	 --network host \
	 -v $(PWD)/frontend:/app \
	 -w /app \
	 registry.yygu.cn/base/node:24 bash 
