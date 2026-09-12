#!/bin/sh
set -eu

directory="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/certs"
mkdir -p "$directory"
if [ ! -s "$directory/cert.pem" ] || [ ! -s "$directory/key.pem" ]; then
  openssl req -x509 -newkey rsa:3072 -sha256 -nodes -days 3650 \
    -subj '/CN=10.88.0.254' \
    -addext 'subjectAltName=IP:10.88.0.254,IP:10.211.55.7' \
    -keyout "$directory/key.pem" \
    -out "$directory/cert.pem"
fi
chmod 0644 "$directory/cert.pem"
chmod 0600 "$directory/key.pem"
if [ "$(id -u)" -eq 0 ]; then
  chown 1000:1000 "$directory/cert.pem" "$directory/key.pem"
fi
