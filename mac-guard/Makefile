GO_BIN ?= /opt/homebrew/opt/go@1.24/libexec/bin/go

.PHONY: format test web build pkg

format:
	$(GO_BIN) fmt ./...

test: web
	$(GO_BIN) test ./...
	$(GO_BIN) vet ./...

web:
	cd web && npm ci && npm run build

build: web
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 $(GO_BIN) build -trimpath \
		-ldflags="-s -w -X main.version=$$(tr -d '[:space:]' < VERSION)" \
		-o dist/lightnode-controller-darwin-arm64 ./cmd/lightnode-controller
	chmod 0755 dist/lightnode-controller-darwin-arm64

pkg:
	./scripts/package/build-pkg.sh
