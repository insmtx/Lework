PROJECT := leros
REGISTRY ?= registry.yygu.cn/insmtx/

.PHONY: build docker-build-base docker-push-base docker-build docker-dev-build docker-push docker-compose-up docker-compose-down run run-foreground run-detached stop logs swagger swagger-clean dev-setup dev-server dev-worker dev-frontend

build:
	go build -v -o ./bundles/leros ./backend/cmd/leros/

docker-build-base:
	docker build -t $(REGISTRY)$(PROJECT)-base:latest -f deployments/build/Dockerfile.base .

docker-push-base: docker-build-base
	docker push $(REGISTRY)$(PROJECT)-base:latest

docker-build:
	docker build -t $(REGISTRY)$(PROJECT):latest -f deployments/build/Dockerfile.leros .

docker-dev-build:
	docker build -t $(REGISTRY)$(PROJECT)-dev:latest -f deployments/build/Dockerfile.leros-dev .

docker-push: docker-build
	docker push $(REGISTRY)$(PROJECT):latest

docker-run-leros:
	-docker rm -f $(PROJECT)-leros-dev
	docker run -d --name $(PROJECT)-leros-dev -p 8080:8080 $(REGISTRY)$(PROJECT):latest

docker-compose-up: docker-build
	docker tag $(REGISTRY)$(PROJECT):latest localhost/env_$(PROJECT):latest
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
