#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: sudo install-relay.sh --host-public-key KEY" >&2
  exit 2
}

[[ "$(id -u)" == "0" ]] || { echo "run as root" >&2; exit 1; }
host_public_key=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --host-public-key) [[ "$#" -ge 2 ]] || usage; host_public_key="$2"; shift 2 ;;
    *) usage ;;
  esac
done
[[ "$host_public_key" =~ ^[A-Za-z0-9+/]{43}=$ ]] || { echo "invalid host public key" >&2; exit 1; }

for command in wg wg-quick iptables flock; do
  command -v "$command" >/dev/null || { echo "missing command: $command" >&2; exit 1; }
done

relay_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
install -d -m 0700 /etc/wireguard /etc/wireguard/clients /etc/wireguard/revoked
if [[ ! -f /etc/wireguard/server.key ]]; then
  umask 077
  wg genkey > /etc/wireguard/server.key
fi
chmod 0600 /etc/wireguard/server.key
install -m 0755 "$relay_dir/lightnode-relay-rules" /usr/local/sbin/lightnode-relay-rules
install -m 0755 "$relay_dir/add-remote-peer.sh" /usr/local/sbin/lightnode-add-remote-peer
install -m 0755 "$relay_dir/remove-remote-peer.sh" /usr/local/sbin/lightnode-remove-remote-peer

server_private_key="$(cat /etc/wireguard/server.key)"
awk -v server_key="$server_private_key" -v host_key="$host_public_key" '
  { gsub("REPLACE_SERVER_PRIVATE_KEY", server_key); gsub("REPLACE_HOST_PUBLIC_KEY", host_key); print }
' "$relay_dir/wg0.conf.example" > /etc/wireguard/wg0.conf.new
chmod 0600 /etc/wireguard/wg0.conf.new
mv /etc/wireguard/wg0.conf.new /etc/wireguard/wg0.conf
wg-quick strip wg0 >/dev/null
if systemctl is-active --quiet wg-quick@wg0.service; then
  wg syncconf wg0 <(wg-quick strip wg0)
  /usr/local/sbin/lightnode-relay-rules up wg0
else
  systemctl enable --now wg-quick@wg0.service
fi

echo "Relay installed. Server public key: $(wg pubkey < /etc/wireguard/server.key)"
