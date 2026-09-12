#!/bin/sh
set -eu

password="${FILE_PORTAL_PASSWORD:?FILE_PORTAL_PASSWORD is required}"
config=/etc/filebrowser/config.yaml
username="${FILE_PORTAL_USERNAME:-downloads}"
password="${FILE_PORTAL_PASSWORD:?FILE_PORTAL_PASSWORD is required}"
[ "${#password}" -ge 8 ] || { echo "FILE_PORTAL_PASSWORD must contain at least 8 characters" >&2; exit 1; }

# This is idempotent: it creates the download-only account on first boot and
# rotates its password when FILE_PORTAL_PASSWORD changes.
filebrowser set -u "$username,$password" -c "$config"

exec filebrowser -c "$config"
