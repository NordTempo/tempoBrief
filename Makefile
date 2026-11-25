# See docs/stack.md#commands.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
DEV_ENV := TEMPOBRIEF_DATA=./tmp/data TEMPOBRIEF_PASSWORD=tempobrief-dev \
	TEMPOBRIEF_LISTEN=127.0.0.1:8080 TEMPOBRIEF_PUBLIC_URL=http://127.0.0.1:5173

.PHONY: dev test lint types web build image image-cli web-deps

web-deps:
	pnpm -C web install --frozen-lockfile

dev:
	@mkdir -p tmp/data
	@echo "web app: http://127.0.0.1:5173  password: tempobrief-dev"
	@trap 'kill 0' EXIT; $(DEV_ENV) go run ./cmd/tempobrief & pnpm -C web dev --host 127.0.0.1

test:
	go test -race ./...
	pnpm -C web test

lint:
	go vet ./...
	go tool staticcheck ./...
	pnpm -C web lint
	pnpm -C web typecheck

types:
	go tool tygo generate

web:
	pnpm -C web build

build: web
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/tempobrief ./cmd/tempobrief

image:
	docker build --build-arg VERSION=$(VERSION) -t tempobrief:dev .

# The same binary plus the Claude Code and Codex CLIs, for subscription logins.
image-cli:
	docker build --build-arg VERSION=$(VERSION) --target cli -t tempobrief:dev-cli .
