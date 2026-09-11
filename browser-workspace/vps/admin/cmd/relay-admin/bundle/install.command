#!/bin/sh
set -eu

cd "$(dirname "$0")"
[ -f BrowserWorkspaceHost.pkg ] && [ -f host.conf ] || {
  echo "安装目录缺少 BrowserWorkspaceHost.pkg 或 host.conf" >&2
  exit 1
}

sudo /usr/sbin/installer -pkg "$PWD/BrowserWorkspaceHost.pkg" -target /
sudo /usr/local/bin/lightnode-browser-host configure "$PWD/host.conf"
sudo /usr/local/bin/lightnode-browser-host doctor
