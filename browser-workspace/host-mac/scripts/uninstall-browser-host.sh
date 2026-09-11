#!/bin/sh
set -eu
[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
case "${1:-}" in
  --yes) purge=0 ;;
  --purge) purge=1 ;;
  *) echo "usage: sudo uninstall-browser-host.sh --yes|--purge" >&2; exit 2 ;;
esac
APP_DIR="/Library/Application Support/LightNodeBrowserWorkspace"
/bin/launchctl bootout system/com.lightnode.browser-route >/dev/null 2>&1 || true
/bin/launchctl bootout system/com.lightnode.browser-host-wireguard >/dev/null 2>&1 || true
/sbin/pfctl -a com.apple.internet-sharing/lightnode-browser-route -F all >/dev/null 2>&1 || true
if [ -f "$APP_DIR/pf.internet-sharing-parent.restore" ]; then
  /sbin/pfctl -a com.apple.internet-sharing -f "$APP_DIR/pf.internet-sharing-parent.restore"
fi
find /Library/LaunchDaemons/com.lightnode.browser-host-wireguard.plist \
  /Library/LaunchDaemons/com.lightnode.browser-route.plist \
  /usr/local/libexec/lightnode-browser-route \
  /etc/pf.anchors/com.lightnode.browser-route \
  /etc/pf.anchors/com.lightnode.browser-internet-sharing-parent \
  -maxdepth 0 -type f -delete 2>/dev/null || true
if [ "$purge" -eq 1 ]; then
  find "$APP_DIR" -depth -delete 2>/dev/null || true
  echo "Browser host uninstalled and configuration purged."
else
  echo "Browser host uninstalled; root-only configuration retained in $APP_DIR."
fi
