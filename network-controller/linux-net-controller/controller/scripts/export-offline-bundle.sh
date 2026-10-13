#!/bin/sh
set -eu

root_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
version="$(tr -d '[:space:]' < "$root_dir/VERSION")"
bundle_name="lightnode-linux-controller-$version-amd64"
output_dir="$root_dir/dist/offline"
image_archive="$output_dir/$bundle_name.tar.gz"
bundle_archive="$output_dir/$bundle_name-bundle.tar.gz"
staging_root="$(mktemp -d)"
bundle_dir="$staging_root/$bundle_name"

cleanup() {
  find "$staging_root" -depth -type f -exec unlink {} \; 2>/dev/null || true
  find "$staging_root" -depth -type l -exec unlink {} \; 2>/dev/null || true
  find "$staging_root" -depth -type d -exec rmdir {} \; 2>/dev/null || true
}
trap cleanup EXIT HUP INT TERM

[ -s "$image_archive" ] || { echo "missing offline image: $image_archive" >&2; exit 1; }
mkdir -p "$bundle_dir/scripts" "$bundle_dir/packaging" "$bundle_dir/dist/linux-amd64" \
  "$bundle_dir/vendor/linux-amd64" "$bundle_dir/vendor"

for file in VERSION compose.yaml README.md SING_BOX_BINARY_SHA256 SING_BOX_VERSION; do
  install -m 0644 "$root_dir/$file" "$bundle_dir/$file"
done
install -m 0755 "$root_dir/scripts/install.sh" "$bundle_dir/scripts/install.sh"
install -m 0644 "$root_dir/dist/linux-amd64/lightnode-linux-controller" \
  "$bundle_dir/dist/linux-amd64/lightnode-linux-controller"
chmod 0755 "$bundle_dir/dist/linux-amd64/lightnode-linux-controller"
install -m 0755 "$root_dir/vendor/linux-amd64/sing-box" "$bundle_dir/vendor/linux-amd64/sing-box"
install -m 0644 "$root_dir/vendor/ca-certificates.crt" "$bundle_dir/vendor/ca-certificates.crt"
cp -R "$root_dir/packaging/." "$bundle_dir/packaging/"
install -m 0644 "$image_archive" "$bundle_dir/$(basename "$image_archive")"

tar -C "$staging_root" -czf "$bundle_archive" "$bundle_name"
(cd "$output_dir" && sha256sum "$(basename "$bundle_archive")" > "$(basename "$bundle_archive").sha256")
echo "$bundle_archive"
