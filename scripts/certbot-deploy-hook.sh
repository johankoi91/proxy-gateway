#!/usr/bin/env bash
set -euo pipefail

INSTALL_DIR="${INSTALL_DIR:-/opt/lightnode-vpn-gateway}"
LINEAGE="${RENEWED_LINEAGE:?Certbot did not provide RENEWED_LINEAGE}"

install -d -m 0700 "$INSTALL_DIR/tls"
install -m 0644 "$LINEAGE/fullchain.pem" "$INSTALL_DIR/tls/server.crt"
install -m 0600 "$LINEAGE/privkey.pem" "$INSTALL_DIR/tls/server.key"

cd "$INSTALL_DIR"
docker compose restart gateway
