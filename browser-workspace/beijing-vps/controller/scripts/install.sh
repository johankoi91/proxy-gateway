#!/bin/sh
set -eu

usage() {
  echo "usage: sudo ./install.sh --vps-config FILE [--offline-image FILE]" >&2
  exit 2
}

[ "$(id -u)" -eq 0 ] || { echo "run as root" >&2; exit 1; }
vps_config=""
offline_image=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --vps-config) [ "$#" -ge 2 ] || usage; vps_config="$2"; shift 2 ;;
    --offline-image) [ "$#" -ge 2 ] || usage; offline_image="$2"; shift 2 ;;
    *) usage ;;
  esac
done
[ -f "$vps_config" ] || usage
[ -z "$offline_image" ] || [ -f "$offline_image" ] || usage
[ "$(uname -m)" = "x86_64" ] || { echo "this package targets Linux AMD64" >&2; exit 1; }

root_dir="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
for command in /usr/sbin/nft /usr/bin/curl /usr/bin/openssl /usr/bin/resolvectl /usr/bin/systemctl; do
  [ -x "$command" ] || { echo "missing dependency: $command" >&2; exit 1; }
done
if [ ! -x /usr/sbin/chronyd ]; then
  /usr/bin/apt-get update
  DEBIAN_FRONTEND=noninteractive /usr/bin/apt-get install -y chrony
fi
expected_sing_box="$(tr -d '[:space:]' < "$root_dir/SING_BOX_BINARY_SHA256")"
actual_sing_box="$(sha256sum "$root_dir/vendor/linux-amd64/sing-box" | awk '{print $1}')"
[ "$actual_sing_box" = "$expected_sing_box" ] || { echo "sing-box checksum mismatch" >&2; exit 1; }

install -d -o root -g root -m 0755 /usr/local/libexec/lightnode-linux-controller
install -d -o root -g root -m 0700 /etc/lightnode-linux-controller
install -d -o root -g root -m 0755 /run/lightnode-linux-controller
install -o root -g root -m 0755 "$root_dir/dist/linux-amd64/lightnode-linux-controller" /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller
ln -sfn /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller /usr/local/bin/lightnode-linux-controller
install -o root -g root -m 0644 "$root_dir/packaging/lightnode-lockdown.service" /etc/systemd/system/lightnode-lockdown.service
install -o root -g root -m 0644 "$root_dir/packaging/lightnode-container-dns.service" /etc/systemd/system/lightnode-container-dns.service
install -o root -g root -m 0644 "$root_dir/packaging/lightnode-container-dns.timer" /etc/systemd/system/lightnode-container-dns.timer
install -o root -g root -m 0644 "$root_dir/packaging/lightnode-container-refresh.path" /etc/systemd/system/lightnode-container-refresh.path
install -d -o root -g root -m 0755 /etc/chrony/conf.d /etc/systemd/system/chrony.service.d
install -o root -g root -m 0644 "$root_dir/packaging/chrony-lightnode.conf" /etc/chrony/conf.d/lightnode-controller.conf
install -o root -g root -m 0644 "$root_dir/packaging/chrony-lightnode.service.conf" /etc/systemd/system/chrony.service.d/lightnode-controller.conf
if ! grep -Eq '^[[:space:]]*confdir[[:space:]]+/etc/chrony/conf\.d([[:space:]]|$)' /etc/chrony/chrony.conf; then
  printf '\nconfdir /etc/chrony/conf.d\n' >> /etc/chrony/chrony.conf
fi

/usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller provision \
  --vps-config "$vps_config" --output /etc/lightnode-linux-controller --public-browser
if [ ! -s /etc/lightnode-linux-controller/controller.crt ] || [ ! -s /etc/lightnode-linux-controller/controller.key ]; then
  /usr/bin/openssl req -x509 -newkey rsa:2048 -sha256 -nodes -days 825 \
    -subj "/CN=LightNode Controller" \
    -keyout /etc/lightnode-linux-controller/controller.key \
    -out /etc/lightnode-linux-controller/controller.crt
fi
chmod 0600 /etc/lightnode-linux-controller/controller.key
chmod 0644 /etc/lightnode-linux-controller/controller.crt
if [ ! -s "$root_dir/.env" ]; then
  controller_password="$(/usr/bin/openssl rand -hex 20)"
  umask 077
  printf '%s\n' \
    'CONTROLLER_USER=admin' \
    "CONTROLLER_PASSWORD=$controller_password" \
    'CONTROLLER_TLS_CERT=/etc/lightnode-linux-controller/controller.crt' \
    'CONTROLLER_TLS_KEY=/etc/lightnode-linux-controller/controller.key' \
    > "$root_dir/.env"
fi
chmod 0600 "$root_dir/.env"
/usr/bin/systemctl daemon-reload

cd "$root_dir"
if [ -n "$offline_image" ]; then
  /usr/bin/docker load --input "$offline_image"
else
  /usr/bin/docker compose build controller
fi
expected_controller="$(sha256sum "$root_dir/dist/linux-amd64/lightnode-linux-controller" | awk '{print $1}')"
installed_controller="$(sha256sum /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller | awk '{print $1}')"
container_checksums="$(/usr/bin/docker compose run --rm --no-deps --entrypoint /usr/bin/sha256sum controller \
  /usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller \
  /usr/local/libexec/lightnode-linux-controller/sing-box)"
container_controller="$(printf '%s\n' "$container_checksums" | awk '$2 ~ /lightnode-linux-controller$/ {print $1}')"
container_sing_box="$(printf '%s\n' "$container_checksums" | awk '$2 ~ /sing-box$/ {print $1}')"
[ "$installed_controller" = "$expected_controller" ] || { echo "installed Controller checksum mismatch" >&2; exit 1; }
[ "$container_controller" = "$expected_controller" ] || { echo "container Controller checksum mismatch" >&2; exit 1; }
[ "$container_sing_box" = "$expected_sing_box" ] || { echo "container sing-box checksum mismatch" >&2; exit 1; }
/usr/bin/docker compose run --rm --entrypoint /usr/local/libexec/lightnode-linux-controller/sing-box controller check -c /etc/lightnode-linux-controller/sing-box.json
/usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller firewall-apply
/usr/bin/systemctl enable --now lightnode-lockdown.service

/usr/bin/docker compose up -d controller
/usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller dns-wait-apply
/usr/local/libexec/lightnode-linux-controller/lightnode-linux-controller host-refresh
healthy=0
for _ in $(seq 1 30); do
  state="$(/usr/bin/docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}' lightnode-linux-controller 2>/dev/null || true)"
  if [ "$state" = healthy ]; then
    healthy=1
    break
  fi
  sleep 2
done
if [ "$healthy" -ne 1 ]; then
  /usr/bin/docker compose logs --tail=100 controller >&2 || true
  echo "container failed health validation; Kill Switch remains active" >&2
  exit 1
fi
/usr/bin/systemctl disable lightnode-container-dns.service >/dev/null 2>&1 || true
/usr/bin/systemctl enable --now lightnode-container-dns.timer >/dev/null
/usr/bin/systemctl enable --now lightnode-container-refresh.path >/dev/null
/usr/bin/systemctl enable chrony.service >/dev/null
/usr/bin/systemctl restart chrony.service

echo "Docker Controller installed and healthy."
echo "Run: cd $root_dir && docker compose ps"
