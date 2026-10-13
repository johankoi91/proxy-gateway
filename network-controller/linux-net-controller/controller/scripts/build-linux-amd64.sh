#!/bin/sh
set -eu

root_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
version="$(tr -d '[:space:]' < "$root_dir/VERSION")"
expected_sing_box="$(tr -d '[:space:]' < "$root_dir/SING_BOX_BINARY_SHA256")"
actual_sing_box="$(shasum -a 256 "$root_dir/vendor/linux-amd64/sing-box" | awk '{print $1}')"
[ "$actual_sing_box" = "$expected_sing_box" ] || { echo "sing-box checksum mismatch" >&2; exit 1; }
mkdir -p "$root_dir/dist/linux-amd64"
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
  -trimpath -ldflags "-s -w -X lightnode-linux-controller/internal/system.BuildVersion=$version" \
  -o "$root_dir/dist/linux-amd64/lightnode-linux-controller" \
  "$root_dir/cmd/lightnode-linux-controller"
shasum -a 256 "$root_dir/dist/linux-amd64/lightnode-linux-controller"
