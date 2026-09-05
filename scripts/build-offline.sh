#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
OUT="$ROOT/dist/lightnode-vpn-gateway-offline-$VERSION"
IMAGE="lightnode-vpn-gateway:$VERSION"

command -v docker >/dev/null 2>&1 || { echo "docker is required" >&2; exit 1; }
docker buildx version >/dev/null 2>&1 || { echo "docker buildx is required" >&2; exit 1; }

cd "$ROOT"
docker buildx build --platform linux/amd64 --load --build-arg VERSION="$VERSION" -t "$IMAGE" .

install -d -m 0755 "$OUT"
docker save -o "$OUT/lightnode-vpn-gateway-amd64.tar" "$IMAGE"
install -m 0644 docker-compose.yml "$OUT/docker-compose.yml"
install -m 0755 scripts/install-offline.sh "$OUT/install.sh"
install -m 0755 scripts/certbot-deploy-hook.sh "$OUT/certbot-deploy-hook.sh"
install -m 0644 packaging/certbot-ip-renew@.service "$OUT/certbot-ip-renew@.service"
install -m 0644 packaging/certbot-ip-renew@.timer "$OUT/certbot-ip-renew@.timer"
install -m 0644 README.md "$OUT/README.md"
(
  cd "$OUT"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum lightnode-vpn-gateway-amd64.tar docker-compose.yml install.sh certbot-deploy-hook.sh certbot-ip-renew@.service certbot-ip-renew@.timer README.md > SHA256SUMS
  else
    shasum -a 256 lightnode-vpn-gateway-amd64.tar docker-compose.yml install.sh certbot-deploy-hook.sh certbot-ip-renew@.service certbot-ip-renew@.timer README.md > SHA256SUMS
  fi
)
echo "$OUT"
