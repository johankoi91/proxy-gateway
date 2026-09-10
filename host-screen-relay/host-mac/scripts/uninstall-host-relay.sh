#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
case "${1:-}" in
  --yes) purge=0 ;;
  --purge) purge=1 ;;
  *) echo "usage: sudo uninstall-host-relay.sh --yes|--purge" >&2; exit 2 ;;
esac
APP_DIR="/Library/Application Support/LightNodeHostRelay"
/bin/launchctl bootout system/com.lightnode.sunshine-route >/dev/null 2>&1 || true
/bin/launchctl bootout system/com.lightnode.host-wireguard >/dev/null 2>&1 || true
/sbin/pfctl -a com.apple.internet-sharing/lightnode-sunshine-route -F all >/dev/null 2>&1 || true
if [ -f "$APP_DIR/pf.internet-sharing-parent.restore" ]; then
  /sbin/pfctl -a com.apple.internet-sharing -f "$APP_DIR/pf.internet-sharing-parent.restore"
fi
find /Library/LaunchDaemons/com.lightnode.host-wireguard.plist \
  /Library/LaunchDaemons/com.lightnode.sunshine-route.plist \
  /usr/local/libexec/lightnode-sunshine-route \
  /etc/pf.anchors/com.lightnode.sunshine-route \
  /etc/pf.anchors/com.lightnode.internet-sharing-parent \
  -maxdepth 0 -type f -delete 2>/dev/null || true
if [ "$purge" -eq 1 ]; then
  find "$APP_DIR" -depth -delete 2>/dev/null || true
  echo "Host relay uninstalled and configuration purged."
else
  echo "Host relay uninstalled; root-only configuration retained in $APP_DIR."
fi
