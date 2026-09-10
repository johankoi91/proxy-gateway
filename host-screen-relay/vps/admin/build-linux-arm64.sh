#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
GO_BIN="${GO_BIN:-/opt/homebrew/opt/go@1.24/libexec/bin/go}"
if [[ ! -x "$GO_BIN" ]]; then
  GO_BIN="$(command -v go)"
fi

npm --prefix "$ROOT/web" ci
npm --prefix "$ROOT/web" run build
"$GO_BIN" -C "$ROOT" test ./...
install -d -m 0755 "$ROOT/dist"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 "$GO_BIN" -C "$ROOT" build \
  -trimpath -ldflags='-s -w' \
  -o dist/lightnode-relay-admin-linux-arm64 ./cmd/relay-admin
shasum -a 256 "$ROOT/dist/lightnode-relay-admin-linux-arm64" > \
  "$ROOT/dist/lightnode-relay-admin-linux-arm64.sha256"
echo "$ROOT/dist/lightnode-relay-admin-linux-arm64"
