#!/bin/sh
set -eu

usage() {
  echo "usage: sudo install-browser-host.sh --wireguard-config FILE [--guest-ip IP]" >&2
  exit 2
}

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
WG_SOURCE=""
GUEST_IP="10.211.55.7"
while [ "$#" -gt 0 ]; do
  case "$1" in
    --wireguard-config) [ "$#" -ge 2 ] || usage; WG_SOURCE="$2"; shift 2 ;;
    --guest-ip) [ "$#" -ge 2 ] || usage; GUEST_IP="$2"; shift 2 ;;
    *) usage ;;
  esac
done
[ -f "$WG_SOURCE" ] || usage
echo "$GUEST_IP" | grep -Eq '^[0-9]+(\.[0-9]+){3}$' || { echo "invalid guest IP" >&2; exit 1; }

TOOLS_DIR="${LIGHTNODE_BROWSER_HOST_TOOLS:-/usr/local/libexec/lightnode-browser-host/bin}"
for command in "$TOOLS_DIR/wg" "$TOOLS_DIR/wg-quick" "$TOOLS_DIR/wireguard-go"; do
  [ -x "$command" ] || { echo "missing packaged dependency: $command" >&2; exit 1; }
done

ROOT="${LIGHTNODE_BROWSER_HOST_ROOT:-$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)}"
APP_DIR="/Library/Application Support/LightNodeBrowserWorkspace"
LOG_DIR="/Library/Logs/LightNodeBrowserWorkspace"
HOST_WG_ADDRESS="$(awk -F= '/^[[:space:]]*Address[[:space:]]*=/ { value=$2; gsub(/[[:space:]]/, "", value); sub(/\/.*/, "", value); print value; exit }' "$WG_SOURCE")"
[ -n "$HOST_WG_ADDRESS" ] || { echo "WireGuard Address is missing" >&2; exit 1; }
BRIDGE_INTERFACE="$(route -n get "$GUEST_IP" 2>/dev/null | awk '$1 == "interface:" { print $2; exit }')"
[ -n "$BRIDGE_INTERFACE" ] && ifconfig "$BRIDGE_INTERFACE" >/dev/null 2>&1 || { echo "cannot resolve UTM bridge for $GUEST_IP" >&2; exit 1; }
GUEST_GATEWAY="$(ifconfig "$BRIDGE_INTERFACE" | awk '$1 == "inet" { print $2; exit }')"
[ -n "$GUEST_GATEWAY" ] || { echo "cannot resolve IPv4 for $BRIDGE_INTERFACE" >&2; exit 1; }
GUEST_SUBNET="$(printf '%s\n' "$GUEST_IP" | awk -F. '{ print $1 "." $2 "." $3 ".0/24" }')"

install -d -o root -g wheel -m 0700 "$APP_DIR"
install -d -o root -g wheel -m 0755 "$LOG_DIR"
if [ ! "$WG_SOURCE" -ef "$APP_DIR/host.conf" ]; then
  install -o root -g wheel -m 0600 "$WG_SOURCE" "$APP_DIR/host.conf"
else
  chown root:wheel "$APP_DIR/host.conf"
  chmod 0600 "$APP_DIR/host.conf"
fi
printf 'HOST_WG_ADDRESS=%s\nGUEST_IP=%s\nGUEST_SUBNET=%s\nBRIDGE_INTERFACE=%s\nGUEST_GATEWAY=%s\n' \
  "$HOST_WG_ADDRESS" "$GUEST_IP" "$GUEST_SUBNET" "$BRIDGE_INTERFACE" "$GUEST_GATEWAY" > "$APP_DIR/relay.conf"
chmod 0600 "$APP_DIR/relay.conf"
chown root:wheel "$APP_DIR/relay.conf"

sed \
  -e "s/10\\.88\\.0\\.2/$HOST_WG_ADDRESS/g" \
  -e "s/10\\.211\\.55\\.7/$GUEST_IP/g" \
  -e "s/10\\.211\\.55\\.1/$GUEST_GATEWAY/g" \
  -e "s#10\\.211\\.55\\.0/24#$GUEST_SUBNET#g" \
  -e "s/bridge100/$BRIDGE_INTERFACE/g" \
  "$ROOT/packaging/pf.browser-route.anchor" > "$APP_DIR/pf.browser-route.anchor"
/sbin/pfctl -vnf "$APP_DIR/pf.browser-route.anchor" >/dev/null

/bin/launchctl bootout system/com.lightnode.browser-route >/dev/null 2>&1 || true
/bin/launchctl bootout system/com.lightnode.browser-host-wireguard >/dev/null 2>&1 || true
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if ! pgrep -f '/usr/local/libexec/lightnode-browser-route|wg-quick up .*/LightNodeBrowserWorkspace/host.conf' >/dev/null 2>&1; then
    break
  fi
  sleep 1
done

install -o root -g wheel -m 0755 "$ROOT/scripts/lightnode-browser-route" /usr/local/libexec/lightnode-browser-route
install -o root -g wheel -m 0644 "$ROOT/packaging/com.lightnode.host-wireguard.plist" /Library/LaunchDaemons/com.lightnode.browser-host-wireguard.plist
install -o root -g wheel -m 0644 "$ROOT/packaging/com.lightnode.browser-route.plist" /Library/LaunchDaemons/com.lightnode.browser-route.plist
install -o root -g wheel -m 0644 "$APP_DIR/pf.browser-route.anchor" /etc/pf.anchors/com.lightnode.browser-route
install -o root -g wheel -m 0644 "$ROOT/packaging/pf.internet-sharing-parent.with-browser" /etc/pf.anchors/com.lightnode.browser-internet-sharing-parent
install -o root -g wheel -m 0644 "$ROOT/packaging/pf.internet-sharing-parent.restore" "$APP_DIR/pf.internet-sharing-parent.restore"

/bin/launchctl bootstrap system /Library/LaunchDaemons/com.lightnode.browser-host-wireguard.plist
WG_READY=0
for _ in 1 2 3 4 5 6 7 8 9 10; do
  if /sbin/ifconfig -l | tr ' ' '\n' | while read -r iface; do /sbin/ifconfig "$iface" 2>/dev/null | grep -Fq "inet $HOST_WG_ADDRESS " && exit 0; done; then
    WG_READY=1
    break
  fi
  sleep 1
done
[ "$WG_READY" -eq 1 ] || { echo "WireGuard interface did not become ready" >&2; exit 1; }
/bin/launchctl bootstrap system /Library/LaunchDaemons/com.lightnode.browser-route.plist

echo "Browser host installed. Run lightnode-browser-host doctor."
