#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
IMAGE_TAR="${IMAGE_TAR:-$SCRIPT_DIR/lightnode-vpn-gateway-amd64.tar}"
INSTALL_DIR="${INSTALL_DIR:-/opt/lightnode-vpn-gateway}"
PUBLIC_ADMIN="${PUBLIC_ADMIN:-false}"

if [[ ${EUID} -ne 0 ]]; then
  echo "Run this installer as root." >&2
  exit 1
fi
if ! command -v docker >/dev/null 2>&1; then
  echo "Docker Engine with the compose plugin must be installed first." >&2
  exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
  echo "The Docker Compose plugin is required." >&2
  exit 1
fi
if [[ ! -f "$IMAGE_TAR" ]]; then
  echo "Offline image not found: $IMAGE_TAR" >&2
  exit 1
fi
if ip link show wg0 >/dev/null 2>&1; then
  echo "A host wg0 interface already exists. Stop the existing WireGuard service before installing." >&2
  exit 1
fi
if ss -lnt | awk '{print $4}' | grep -Eq '(^|:)443$'; then
  echo "TCP port 443 is already in use." >&2
  exit 1
fi
if ss -lnu | awk '{print $4}' | grep -Eq '(^|:)443$'; then
  echo "UDP port 443 is already in use." >&2
  exit 1
fi

install -d -m 0700 "$INSTALL_DIR" "$INSTALL_DIR/data" "$INSTALL_DIR/tls"
install -m 0644 "$SCRIPT_DIR/docker-compose.yml" "$INSTALL_DIR/docker-compose.yml"

if [[ ! -f "$INSTALL_DIR/.env" ]]; then
  password="$(openssl rand -hex 18)"
  public_ip="$(curl -4fsS --max-time 10 https://api.ipify.org)"
  egress_if="$(ip -4 route show default | awk 'NR==1 {print $5}')"
  if [[ "$PUBLIC_ADMIN" == "true" ]]; then
    command -v openssl >/dev/null 2>&1 || { echo "openssl is required for public admin TLS." >&2; exit 1; }
    openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 825 \
      -subj "/CN=$public_ip" \
      -addext "subjectAltName=IP:$public_ip" \
      -keyout "$INSTALL_DIR/tls/server.key" \
      -out "$INSTALL_DIR/tls/server.crt" >/dev/null 2>&1
    chmod 0600 "$INSTALL_DIR/tls/server.key"
    chmod 0644 "$INSTALL_DIR/tls/server.crt"
  fi
  {
    echo "ADMIN_USER=admin"
    echo "ADMIN_PASSWORD=$password"
    echo "PUBLIC_IP=$public_ip"
    echo "EGRESS_INTERFACE=$egress_if"
    if [[ "$PUBLIC_ADMIN" == "true" ]]; then
      echo "LISTEN_ADDR=127.0.0.1:9080"
      echo "TLS_CERT_FILE=/tls/server.crt"
      echo "TLS_KEY_FILE=/tls/server.key"
      echo "ADMIN_SERVER_NAME=$public_ip"
    fi
  } > "$INSTALL_DIR/.env"
  chmod 0600 "$INSTALL_DIR/.env"
  echo "Admin password: $password"
fi

cat > /etc/sysctl.d/99-vpn-gateway.conf <<'EOF'
net.ipv4.ip_forward=1
net.ipv6.conf.all.forwarding=1
net.ipv4.tcp_keepalive_time=60
net.ipv4.tcp_keepalive_intvl=15
net.ipv4.tcp_keepalive_probes=4
EOF
sysctl --system >/dev/null

if command -v ufw >/dev/null 2>&1 && ufw status | grep -q '^Status: active'; then
  ufw allow 443/tcp comment 'VLESS Reality'
  ufw allow 443/udp comment 'WireGuard'
fi

docker load -i "$IMAGE_TAR"
cd "$INSTALL_DIR"
docker compose up -d

public_ip="$(awk -F= '$1=="PUBLIC_IP" {print $2}' .env)"
echo
echo "VPN Gateway is starting."
if [[ "$PUBLIC_ADMIN" == "true" ]]; then
  echo "Public admin: https://$public_ip"
else
  echo "Open an SSH tunnel from your computer:"
  echo "  ssh -L 9080:127.0.0.1:9080 root@$public_ip"
  echo "Then open: http://127.0.0.1:9080"
fi
echo "Credentials are stored in $INSTALL_DIR/.env"
