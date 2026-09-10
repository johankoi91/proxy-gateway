#!/usr/bin/env bash
set -euo pipefail

[[ "$(id -u)" == "0" ]] || { echo "run as root" >&2; exit 1; }
name="${1:-}"
[[ "$name" =~ ^[A-Za-z0-9._-]+$ ]] || { echo "usage: sudo remove-remote-peer.sh NAME" >&2; exit 2; }
directory="/etc/wireguard/clients/$name"
[[ -f "$directory/public.key" ]] || { echo "peer not found" >&2; exit 1; }
install -d -m 0700 /etc/wireguard/revoked
exec 9>/run/lightnode-wireguard-peer.lock
flock -x 9

awk -v begin="# BEGIN CLIENT $name" -v end="# END CLIENT $name" '
  $0 == begin { skip=1; next }
  $0 == end { skip=0; next }
  !skip { print }
' /etc/wireguard/wg0.conf > /etc/wireguard/wg0.conf.new
chmod 0600 /etc/wireguard/wg0.conf.new
mv /etc/wireguard/wg0.conf.new /etc/wireguard/wg0.conf
wg syncconf wg0 <(wg-quick strip wg0)

stamp="$(date -u +%Y%m%dT%H%M%SZ)"
mv "$directory" "/etc/wireguard/revoked/${name}-${stamp}"
echo "peer revoked: $name"
