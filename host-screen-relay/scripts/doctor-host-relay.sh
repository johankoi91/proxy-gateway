#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }

APP_DIR="/Library/Application Support/LightNodeHostRelay"
HOST_WG_ADDRESS="$(awk -F= '$1 == "HOST_WG_ADDRESS" { print $2 }' "$APP_DIR/relay.conf")"
GUEST_IP="$(awk -F= '$1 == "GUEST_IP" { print $2 }' "$APP_DIR/relay.conf")"
WG_IF="$(ifconfig -l | tr ' ' '\n' | while read -r iface; do ifconfig "$iface" 2>/dev/null | grep -Fq "inet $HOST_WG_ADDRESS " && { echo "$iface"; break; }; done)"
[ -n "$WG_IF" ] || { echo "FAIL wireguard interface"; exit 1; }
echo "OK wireguard interface $WG_IF"

pfctl -a com.apple.internet-sharing -sr 2>/dev/null | grep -q 'lightnode-sunshine-route' || { echo "FAIL PF parent anchor"; exit 1; }
pfctl -a com.apple.internet-sharing/lightnode-sunshine-route -sr 2>/dev/null | grep -Fq "$GUEST_IP" || { echo "FAIL PF relay rules"; exit 1; }
echo "OK PF relay rules"

RELAY_READY=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if ping -c 1 10.88.0.1 >/dev/null 2>&1 && \
     nc -G 2 -z 10.88.0.254 47984 >/dev/null 2>&1 && \
     nc -G 2 -z 10.88.0.254 47989 >/dev/null 2>&1 && \
     nc -G 2 -z 10.88.0.254 48010 >/dev/null 2>&1; then
    RELAY_READY=1
    break
  fi
  sleep 4
done
[ "$RELAY_READY" -eq 1 ] || { echo "FAIL Guangzhou hairpin or Sunshine TCP after 40 seconds"; exit 1; }
echo "OK Guangzhou hairpin and Sunshine TCP"
