.PHONY: help webui-install webui-dev webui-build backend-dev test build run release-check docker-build docker-debug

ifneq (,$(wildcard ./.env))
include .env
export
endif

GO ?= go
NPM ?= npm
PORT ?= 28080
WEBUI_PORT ?= 48081
GOCACHE ?= $(CURDIR)/.gocache
WEBUI_DIR := webui
BINARY := ./bin/minfo

help:
	@printf '%s\n' \
		'make webui-install  # 安装前端依赖' \
		'make webui-dev      # 启动前端 Vite 开发服务器 (48081)' \
		'make backend-dev    # 启动 Go 后端 (28080)' \
		'make test           # 运行 Go 测试' \
		'make build          # 先构建前端，再构建 Go 二进制' \
		'make run            # 运行构建后的二进制' \
		'make release-check  # 发布前检查：前端构建 + Go 测试 + Go 构建' \
		'make docker-debug   # Docker 后端 + 本机前端的本地调试后端' \
		'make docker-build   # 构建与生产更接近的 Docker 镜像'

webui-install:
	./scripts/bootstrap-webui.sh

webui-dev:
	cd $(WEBUI_DIR) && $(NPM) run dev -- --host 0.0.0.0 --port $(WEBUI_PORT)

webui-build:
	cd $(WEBUI_DIR) && $(NPM) run build

backend-dev:
	mkdir -p $(GOCACHE)
	GOCACHE=$(GOCACHE) PORT=$(PORT) $(GO) run ./cmd/minfo

test:
	mkdir -p $(GOCACHE)
	GOCACHE=$(GOCACHE) $(GO) test ./...

build: webui-build
	mkdir -p $(GOCACHE) ./bin
	GOCACHE=$(GOCACHE) $(GO) build -trimpath -buildvcs=false -o $(BINARY) ./cmd/minfo

run: build
	PORT=$(PORT) $(BINARY)

release-check: test build

docker-debug:
	docker compose -f docker-compose.debug.yml up --build --remove-orphans

docker-build:
	docker build --target runtime -t minfo:local .
