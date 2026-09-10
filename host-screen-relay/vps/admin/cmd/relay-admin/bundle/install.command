#!/bin/sh
set -eu

cd "$(dirname "$0")"
if [ ! -f HostScreenRelay.pkg ] || [ ! -f host.conf ]; then
  echo "安装目录中缺少 HostScreenRelay.pkg 或 host.conf"
  printf '按回车键退出...'
  read answer
  exit 1
fi

echo "正在安装 Host Screen Relay..."
sudo /usr/sbin/installer -pkg "$PWD/HostScreenRelay.pkg" -target /
echo "正在连接广州 VPS 并配置 UTM 转发..."
sudo /usr/local/bin/lightnode-host-relay configure "$PWD/host.conf"
echo
echo "安装完成。正在执行检查..."
sudo /usr/local/bin/lightnode-host-relay doctor
echo
printf '完成，按回车键关闭窗口...'
read answer
