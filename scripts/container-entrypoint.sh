#!/usr/bin/env bash
set -euo pipefail

install -d -m 0700 "${RUNTIME_DIR:-/run/vpn-gateway}" /data

if [[ -z "${EGRESS_INTERFACE:-}" ]]; then
  EGRESS_INTERFACE="$(ip -4 route show default | awk 'NR==1 {print $5}')"
  export EGRESS_INTERFACE
fi

if [[ -z "${PUBLIC_IP:-}" ]]; then
  PUBLIC_IP="$(curl -4fsS --max-time 10 https://api.ipify.org)"
  export PUBLIC_IP
fi

if [[ -z "${ADMIN_PASSWORD:-}" ]]; then
  echo "ADMIN_PASSWORD is required" >&2
  exit 1
fi

/usr/local/bin/vpn-gateway bootstrap

cleanup() {
  trap - EXIT INT TERM
  /usr/bin/supervisorctl -c /etc/supervisord.conf shutdown >/dev/null 2>&1 || true
  if [[ -n "${supervisor_pid:-}" ]]; then
    wait "$supervisor_pid" 2>/dev/null || true
  fi
  wg-quick down "${RUNTIME_DIR:-/run/vpn-gateway}/wg0.conf" >/dev/null 2>&1 || true
}

trap cleanup EXIT INT TERM
/usr/bin/supervisord -c /etc/supervisord.conf &
supervisor_pid=$!
wait "$supervisor_pid"
