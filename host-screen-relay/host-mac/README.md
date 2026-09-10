# Host Mac

本目录只用于运行 UTM 的宿主 Mac，不要安装到 UTM Guest 或远端控制 Mac。

## 目录

```text
host-mac/
├── config/       宿主 WireGuard 配置示例
├── packaging/    LaunchDaemon、PF 模板、离线 WireGuard 组件
├── scripts/      安装、诊断、卸载和 PKG 构建脚本
├── dist/         构建产物，不提交 Git
└── VERSION       PKG 版本
```

## 构建

```bash
./host-mac/scripts/package/build-host-pkg.sh
```

输出：

```text
host-mac/dist/HostScreenRelay-<version>-arm64.pkg
```

构建不依赖 Homebrew。`packaging/vendor/darwin-arm64` 保存经过验证的 `wg`、
`wg-quick`、`wireguard-go` 和 SHA-256 清单。

## 用户安装

正常用户不需要进入本目录。打开广州后台，点击“下载安装包”，解压后双击：

```text
安装 Host Screen Relay.command
```

它会安装 PKG、导入同包的 `host.conf`，自动识别当前登录用户和 Guest 所在 UTM
bridge，然后运行诊断。安装时 Guest 必须已启动且可从宿主访问。

宿主配置固定使用 `10.88.0.2`，只能在一个宿主上运行。迁移到新宿主前必须停用旧宿主。

## 命令

```bash
sudo /usr/local/bin/lightnode-host-relay doctor
sudo /usr/local/bin/lightnode-host-relay configure /path/to/host.conf
sudo /usr/local/bin/lightnode-host-relay uninstall
sudo /usr/local/bin/lightnode-host-relay uninstall --purge
```

`--purge` 会删除宿主配置和私钥。完整运行路径见
[`../docs/operations.md`](../docs/operations.md)。
