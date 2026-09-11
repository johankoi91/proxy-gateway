#!/usr/bin/env bash
set -euo pipefail
export COPYFILE_DISABLE=1

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
OUT_DIR="$ROOT/dist"
OUT="$OUT_DIR/BrowserWorkspaceHost-$VERSION-arm64.pkg"
VENDOR_DIR="$ROOT/packaging/vendor/darwin-arm64"
WORK="$(mktemp -d /tmp/browser-workspace-host-pkg.XXXXXX)"
PAYLOAD="$WORK/payload"
PRODUCT_TOOLS="$PAYLOAD/usr/local/libexec/lightnode-browser-host/bin"
PRODUCT_ROOT="$PAYLOAD/usr/local/share/lightnode-browser-host"
trap 'rm -rf "$WORK"' EXIT

for file in "$VENDOR_DIR/wg" "$VENDOR_DIR/wg-quick" "$VENDOR_DIR/wireguard-go"; do
  [ -f "$file" ] || { echo "missing build dependency: $file" >&2; exit 1; }
done
[ "$(uname -m)" = arm64 ] || { echo "this builder currently produces the Apple Silicon package only" >&2; exit 1; }
(cd "$VENDOR_DIR" && shasum -a 256 -c SHA256SUMS)

install -d -m 0755 \
  "$OUT_DIR" "$PRODUCT_TOOLS" "$PRODUCT_ROOT/scripts" "$PRODUCT_ROOT/packaging" \
  "$PAYLOAD/usr/local/bin" "$PAYLOAD/usr/local/libexec" \
  "$PAYLOAD/Library/LaunchDaemons" "$PAYLOAD/usr/local/share/doc/lightnode-browser-host"
install -m 0755 "$VENDOR_DIR/wg" "$PRODUCT_TOOLS/wg"
install -m 0755 "$VENDOR_DIR/wg-quick" "$PRODUCT_TOOLS/wg-quick"
install -m 0755 "$VENDOR_DIR/wireguard-go" "$PRODUCT_TOOLS/wireguard-go"

install -m 0755 "$ROOT/scripts/install-browser-host.sh" "$PRODUCT_ROOT/scripts/install-browser-host.sh"
install -m 0755 "$ROOT/scripts/doctor-browser-host.sh" "$PRODUCT_ROOT/scripts/doctor-browser-host.sh"
install -m 0755 "$ROOT/scripts/uninstall-browser-host.sh" "$PRODUCT_ROOT/scripts/uninstall-browser-host.sh"
install -m 0755 "$ROOT/scripts/lightnode-browser-route" "$PRODUCT_ROOT/scripts/lightnode-browser-route"
install -m 0755 "$ROOT/scripts/lightnode-browser-host" "$PAYLOAD/usr/local/bin/lightnode-browser-host"
install -m 0644 "$ROOT/packaging/pf.browser-route.anchor" "$PRODUCT_ROOT/packaging/pf.browser-route.anchor"
install -m 0644 "$ROOT/packaging/pf.internet-sharing-parent.with-browser" "$PRODUCT_ROOT/packaging/pf.internet-sharing-parent.with-browser"
install -m 0644 "$ROOT/packaging/pf.internet-sharing-parent.restore" "$PRODUCT_ROOT/packaging/pf.internet-sharing-parent.restore"
install -m 0644 "$ROOT/packaging/com.lightnode.host-wireguard.plist" "$PRODUCT_ROOT/packaging/com.lightnode.host-wireguard.plist"
install -m 0644 "$ROOT/packaging/com.lightnode.browser-route.plist" "$PRODUCT_ROOT/packaging/com.lightnode.browser-route.plist"
install -m 0644 "$ROOT/packaging/com.lightnode.host-wireguard.plist" "$PAYLOAD/Library/LaunchDaemons/com.lightnode.browser-host-wireguard.plist"
install -m 0644 "$ROOT/packaging/com.lightnode.browser-route.plist" "$PAYLOAD/Library/LaunchDaemons/com.lightnode.browser-route.plist"
install -m 0644 "$VENDOR_DIR/WIREGUARD_TOOLS_COPYING" "$PAYLOAD/usr/local/share/doc/lightnode-browser-host/WIREGUARD_TOOLS_COPYING"
install -m 0644 "$VENDOR_DIR/WIREGUARD_GO_LICENSE" "$PAYLOAD/usr/local/share/doc/lightnode-browser-host/WIREGUARD_GO_LICENSE"
install -m 0644 "$ROOT/README.md" "$PAYLOAD/usr/local/share/doc/lightnode-browser-host/README.md"
xattr -cr "$PAYLOAD"
find "$PAYLOAD" -type f -name '._*' -delete

rm -f "$OUT" "$OUT.sha256"
args=(
  --root "$PAYLOAD"
  --identifier com.lightnode.browser-workspace-host
  --version "$VERSION"
  --install-location /
  --ownership recommended
  --filter '(^|/)\._'
  --filter '(^|/)\.DS_Store$'
)
if [ -n "${PKG_SIGN_IDENTITY:-}" ]; then
  args+=(--sign "$PKG_SIGN_IDENTITY")
fi
pkgbuild "${args[@]}" "$OUT"
shasum -a 256 "$OUT" > "$OUT.sha256"
echo "$OUT"
