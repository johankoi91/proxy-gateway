#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }

APP_DIR="/Library/Application Support/LightNodeBrowserWorkspace"
HOST_WG_ADDRESS="$(awk -F= '$1 == "HOST_WG_ADDRESS" { print $2 }' "$APP_DIR/relay.conf")"
GUEST_IP="$(awk -F= '$1 == "GUEST_IP" { print $2 }' "$APP_DIR/relay.conf")"
WG_IF="$(ifconfig -l | tr ' ' '\n' | while read -r iface; do ifconfig "$iface" 2>/dev/null | grep -Fq "inet $HOST_WG_ADDRESS " && { echo "$iface"; break; }; done)"
[ -n "$WG_IF" ] || { echo "FAIL WireGuard interface"; exit 1; }
echo "OK WireGuard interface $WG_IF"

pfctl -a com.apple.internet-sharing -sn 2>/dev/null | grep -q 'lightnode-browser-route' || { echo "FAIL PF parent anchor"; exit 1; }
pfctl -a com.apple.internet-sharing/lightnode-browser-route -sr 2>/dev/null | grep -Fq "$GUEST_IP" || { echo "FAIL PF browser rules"; exit 1; }
echo "OK PF browser rules"

nc -G 2 -z "$GUEST_IP" 8443 >/dev/null 2>&1 || { echo "FAIL KasmVNC direct endpoint"; exit 1; }
echo "OK KasmVNC direct endpoint"
nc -G 2 -z "$GUEST_IP" 8444 >/dev/null 2>&1 || { echo "FAIL file portal direct endpoint"; exit 1; }
echo "OK file portal direct endpoint"
nc -G 5 -z 10.88.0.254 8443 >/dev/null 2>&1 || { echo "FAIL Guangzhou hairpin"; exit 1; }
nc -G 5 -z 10.88.0.254 8444 >/dev/null 2>&1 || { echo "FAIL file portal hairpin"; exit 1; }
echo "OK Guangzhou hairpin"
