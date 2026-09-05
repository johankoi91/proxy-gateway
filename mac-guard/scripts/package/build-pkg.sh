#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/../.." && pwd)"
VERSION="$(tr -d '[:space:]' < "$ROOT/VERSION")"
PKG_VERSION="${VERSION/-alpha./.}"
SING_BOX_VERSION="$(tr -d '[:space:]' < "$ROOT/SING_BOX_VERSION")"
SING_BOX_SHA256="$(tr -d '[:space:]' < "$ROOT/SING_BOX_SHA256")"
GO_BIN="${GO_BIN:-/opt/homebrew/opt/go@1.24/libexec/bin/go}"
OUT="$ROOT/dist/LightNode-Mac-Guard-$VERSION.pkg"
CACHE="$ROOT/dist/cache"
ARCHIVE="$CACHE/sing-box-$SING_BOX_VERSION-darwin-arm64.tar.gz"
SING_BOX_DIR="$CACHE/sing-box-$SING_BOX_VERSION-darwin-arm64"
SING_BOX="$SING_BOX_DIR/sing-box"
SING_BOX_LICENSE="$SING_BOX_DIR/LICENSE"
URL="https://github.com/SagerNet/sing-box/releases/download/v$SING_BOX_VERSION/sing-box-$SING_BOX_VERSION-darwin-arm64.tar.gz"
WORK="$(mktemp -d /tmp/lightnode-mac-guard-pkg.XXXXXX)"
PAYLOAD="$WORK/payload"
PKG_SCRIPTS="$WORK/scripts"
trap 'rm -rf "$WORK"' EXIT

if [[ ! -x "$GO_BIN" ]]; then
  GO_BIN="$(command -v go)"
fi
mkdir -p "$CACHE"
if [[ ! -x "$SING_BOX" || ! -f "$SING_BOX_LICENSE" ]]; then
  for candidate in "$ROOT"/dist/lightnode-mac-guard-*/sing-box; do
    [[ -x "$candidate" ]] || continue
    license="$(dirname "$candidate")/SING_BOX_LICENSE"
    if [[ -f "$license" ]] && "$candidate" version | head -1 | grep -q "version $SING_BOX_VERSION"; then
      install -d -m 0755 "$SING_BOX_DIR"
      install -m 0755 "$candidate" "$SING_BOX"
      install -m 0644 "$license" "$SING_BOX_LICENSE"
      break
    fi
  done
fi
if [[ ! -x "$SING_BOX" || ! -f "$SING_BOX_LICENSE" ]]; then
  if [[ ! -f "$ARCHIVE" ]] || [[ "$(shasum -a 256 "$ARCHIVE" | awk '{print $1}')" != "$SING_BOX_SHA256" ]]; then
    curl -fL --retry 3 --connect-timeout 15 --max-time 180 -o "$ARCHIVE.download" "$URL"
    actual="$(shasum -a 256 "$ARCHIVE.download" | awk '{print $1}')"
    if [[ "$actual" != "$SING_BOX_SHA256" ]]; then
      echo "sing-box checksum mismatch: expected $SING_BOX_SHA256, got $actual" >&2
      exit 1
    fi
    mv "$ARCHIVE.download" "$ARCHIVE"
  fi
  tar -xzf "$ARCHIVE" -C "$CACHE"
fi
if ! "$SING_BOX" version | head -1 | grep -q "version $SING_BOX_VERSION"; then
  echo "Unexpected sing-box binary version." >&2
  exit 1
fi

cd "$ROOT/web"
npm ci
npm run build
cd "$ROOT"
unset GOROOT
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 "$GO_BIN" build \
  -trimpath -ldflags="-s -w -X main.version=$VERSION" \
  -o "$ROOT/dist/lightnode-guard-darwin-arm64" ./cmd/lightnode-guard
chmod 0755 "$ROOT/dist/lightnode-guard-darwin-arm64"

sed \
  -e 's/REPLACE_VLESS_UUID/8853a21d-d0c5-4047-8c8d-4843e92c5c89/' \
  -e 's/REPLACE_REALITY_PUBLIC_KEY/VEnbXRyOqO98TYo4khJlxFSy0oPzTJsuepq2BNs4oiY/' \
  -e 's/REPLACE_REALITY_SHORT_ID/0123456789abcdef/' \
  -e 's/REPLACE_CLASH_API_SECRET/local-validation-secret/' \
  "$ROOT/packaging/sing-box.v2-global.example.json" > "$WORK/sing-box.validation.json"
"$SING_BOX" check -c "$WORK/sing-box.validation.json"

install -d -m 0755 \
  "$PAYLOAD/usr/local/libexec" \
  "$PAYLOAD/usr/local/share/lightnode-guard/templates" \
  "$PAYLOAD/usr/local/share/doc/lightnode-guard" \
  "$PAYLOAD/Library/LaunchDaemons" \
  "$PKG_SCRIPTS"
install -m 0755 "$ROOT/dist/lightnode-guard-darwin-arm64" "$PAYLOAD/usr/local/libexec/lightnode-guard"
install -m 0755 "$SING_BOX" "$PAYLOAD/usr/local/libexec/sing-box"

for name in config.example.json sing-box.v2-global.example.json v2-policy.example.json pf.anchor.tmpl pf.main.tmpl com.lightnode.vless-killswitch-rollback.plist.tmpl; do
  install -m 0644 "$ROOT/packaging/$name" "$PAYLOAD/usr/local/share/lightnode-guard/templates/$name"
done
install -m 0644 "$ROOT/packaging/com.lightnode.vless-guard.plist" "$PAYLOAD/Library/LaunchDaemons/com.lightnode.vless-guard.plist"
install -m 0644 "$ROOT/packaging/com.lightnode.sing-box.plist" "$PAYLOAD/Library/LaunchDaemons/com.lightnode.sing-box.plist"
install -m 0644 "$ROOT/README.md" "$PAYLOAD/usr/local/share/doc/lightnode-guard/README.md"
install -m 0644 "$ROOT/docs/dns-design.md" "$PAYLOAD/usr/local/share/doc/lightnode-guard/DNS-DESIGN.md"
install -m 0644 "$ROOT/docs/v2-split-routing-design.md" "$PAYLOAD/usr/local/share/doc/lightnode-guard/V2-SPLIT-ROUTING-DESIGN.md"
install -m 0644 "$ROOT/docs/proxy-vpn-conflict-isolation.md" "$PAYLOAD/usr/local/share/doc/lightnode-guard/PROXY-VPN-CONFLICT-ISOLATION.md"
install -m 0644 "$SING_BOX_LICENSE" "$PAYLOAD/usr/local/share/doc/lightnode-guard/SING_BOX_LICENSE"
install -m 0644 "$ROOT/SING_BOX_VERSION" "$PAYLOAD/usr/local/share/doc/lightnode-guard/SING_BOX_VERSION"
install -m 0755 "$ROOT/scripts/package/pkg-postinstall.sh" "$PKG_SCRIPTS/postinstall"

rm -f "$OUT"
args=(
  --root "$PAYLOAD"
  --scripts "$PKG_SCRIPTS"
  --identifier "com.lightnode.mac-guard"
  --version "$PKG_VERSION"
  --install-location /
  --ownership recommended
)
if [[ -n ${PKG_SIGN_IDENTITY:-} ]]; then
  args+=(--sign "$PKG_SIGN_IDENTITY")
fi
pkgbuild "${args[@]}" "$OUT"
shasum -a 256 "$OUT" > "$OUT.sha256"
echo "$OUT"
