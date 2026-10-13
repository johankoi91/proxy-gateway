#!/bin/sh
set -eu

root_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
version="$(tr -d '[:space:]' < "$root_dir/VERSION")"
image="lightnode/linux-controller:$version-amd64"
output_dir="$root_dir/dist/offline"
archive="$output_dir/lightnode-linux-controller-$version-amd64.tar.gz"

mkdir -p "$output_dir"
docker image inspect "$image" >/dev/null
docker save "$image" | gzip -n -9 > "$archive"
(cd "$output_dir" && sha256sum "$(basename "$archive")" > "$(basename "$archive").sha256")
echo "$archive"
