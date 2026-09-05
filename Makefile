VERSION := $(shell tr -d '[:space:]' < VERSION)

.PHONY: format test web build offline

format:
	gofmt -w cmd internal

test: web
	go test ./...

web:
	cd web && npm ci && npm run build

build: web
	go build -trimpath -ldflags="-X main.version=$(VERSION)" -o vpn-gateway ./cmd/gateway

offline:
	./scripts/build-offline.sh
