#!/bin/sh
set -eu

image="lightnode/selkies-chromium:2.0.0rc0-amd64"
root_dir="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
output_dir="${1:-$root_dir/../dist/offline}"
archive="${output_dir}/beijing-selkies-chromium-2.0.0rc0-amd64.tar.gz"

mkdir -p "${output_dir}"
docker image inspect "${image}" >/dev/null
docker save "${image}" | gzip -n -1 > "${archive}"
(cd "${output_dir}" && sha256sum "$(basename "${archive}")" > "$(basename "${archive}").sha256")
printf 'Exported %s\n' "${archive}"
