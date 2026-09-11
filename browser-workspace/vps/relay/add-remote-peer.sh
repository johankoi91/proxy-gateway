#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: sudo add-remote-peer.sh --name NAME --address 10.88.0.X --endpoint PUBLIC_IP:443" >&2
  exit 2
}

[[ "$(id -u)" == "0" ]] || { echo "run as root" >&2; exit 1; }
name=""
address=""
endpoint=""
while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --name) [[ "$#" -ge 2 ]] || usage; name="$2"; shift 2 ;;
    --address) [[ "$#" -ge 2 ]] || usage; address="$2"; shift 2 ;;
    --endpoint) [[ "$#" -ge 2 ]] || usage; endpoint="$2"; shift 2 ;;
    *) usage ;;
  esac
done
[[ "$name" =~ ^[A-Za-z0-9._-]+$ ]] || { echo "invalid peer name" >&2; exit 1; }
[[ "$address" =~ ^10\.88\.0\.([3-9]|[1-9][0-9]|1[0-9]{2}|2[0-4][0-9]|25[0-3])$ ]] || { echo "address must be an unused 10.88.0.3-253 address" >&2; exit 1; }
[[ "$endpoint" == *:* ]] || usage
exec 9>/run/lightnode-wireguard-peer.lock
flock -x 9
grep -q "AllowedIPs = $address/32" /etc/wireguard/wg0.conf && { echo "address already allocated" >&2; exit 1; }
grep -q "# BEGIN CLIENT $name$" /etc/wireguard/wg0.conf && { echo "peer name already exists" >&2; exit 1; }

directory="/etc/wireguard/clients/$name"
install -d -m 0700 /etc/wireguard/clients /etc/wireguard/revoked
install -d -m 0700 "$directory"
umask 077
wg genkey > "$directory/private.key"
wg pubkey < "$directory/private.key" > "$directory/public.key"
server_public_key="$(wg pubkey < /etc/wireguard/server.key)"
client_public_key="$(cat "$directory/public.key")"

config_tmp="$(mktemp /etc/wireguard/.wg0.conf.XXXXXX)"
cat /etc/wireguard/wg0.conf > "$config_tmp"
cat >> "$config_tmp" <<EOF

# BEGIN CLIENT $name
[Peer]
PublicKey = $client_public_key
AllowedIPs = $address/32
# END CLIENT $name
EOF
chmod 0600 "$config_tmp"
mv "$config_tmp" /etc/wireguard/wg0.conf

cat > "$directory/client.conf" <<EOF
[Interface]
PrivateKey = $(cat "$directory/private.key")
Address = $address/24
MTU = 1380

[Peer]
PublicKey = $server_public_key
Endpoint = $endpoint
AllowedIPs = 10.88.0.0/24
PersistentKeepalive = 25
EOF
chmod 0600 "$directory/client.conf"
wg syncconf wg0 <(wg-quick strip wg0)
echo "$directory/client.conf"
