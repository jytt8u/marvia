<div align="center">

<img src="docs/shots/readme-brand.png" alt="Marvia" width="1000">

**适用于 Android 和 Windows 的 VPN。面板、节点与 VP1 协议。**

[Русский](README.md) · [English](README.en.md) · [简体中文](README.zh-CN.md)

[![Android APK](https://img.shields.io/badge/ANDROID-APK-687482?style=for-the-badge&labelColor=30353b)](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
[![Windows EXE](https://img.shields.io/badge/WINDOWS-EXE-687482?style=for-the-badge&labelColor=30353b)](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe)
[![Panel](https://img.shields.io/badge/PANEL-INSTALL-687482?style=for-the-badge&labelColor=30353b)](#安装面板)
[![Docs](https://img.shields.io/badge/DOCS-GUIDE-687482?style=for-the-badge&labelColor=30353b)](docs/guide.md)

[Releases](https://github.com/jytt8u/marvia/releases) · [CI](https://github.com/jytt8u/marvia/actions) · [Privacy](docs/privacy.md)

</div>

## 应用界面

Android 0.12.2 · 模拟器真实截图 · 尚未添加密钥

| 主页 | VPN 设置 | 网络与隐私 |
|:---:|:---:|:---:|
| <img src="docs/shots/android-home.png" alt="主页" width="260"> | <img src="docs/shots/android-settings.png" alt="VPN 设置" width="260"> | <img src="docs/shots/android-advanced.png" alt="网络与隐私" width="260"> |

<details>
<summary>Windows 与面板：历史截图</summary>

Windows 0.9.3 与本地面板实例。上方 Android 截图来自当前 0.12.2 版本。

<img src="docs/shots/windows.png" alt="Windows 0.9.3" width="1000">
<img src="docs/shots/panel-clients.png" alt="Marvia Partner" width="1000">

</details>

## 架构

<img src="docs/shots/readme-flow.svg" alt="技术架构图：访问控制与 VPN 数据传输分离" width="1200">

面板管理访问权限；VPN 数据包由节点转发，不经过面板。
[信任边界](docs/architecture.md)。

## 基准测试

<img src="docs/shots/readme-protocol-benchmark.svg" alt="Local Marvia benchmark: VP1 + TLS 427.7 MB/s, VLESS + TLS 924.2 MB/s, Trojan + TLS 906.3 MB/s; median of five runs" width="1200">

**同一台电脑、相同 TLS 1.3，对比 VP1、VLESS 与 Trojan。**
交错运行五轮，每次传输 512 MiB；Ryzen 7 7700、Windows、Go 1.26.6。
测试对象为 Marvia 的本地回环实现，不包含互联网与 TUN。
VP1 额外使用 Noise 加密和分帧，在本次测试中速度较低。

[方法、范围与原始数据](docs/performance.md) · [测试代码](cmd/marvia-bench)

此前一次互联网测试得到 **VP1 83 Mb/s，直连 86 Mb/s**。
该单次结果与本地 MB/s 测量条件不同，不能直接比较。

## 协议对比

WireGuard 和 OpenVPN 传输 IP 数据包；下表其余协议属于代理。Android 上的
Marvia 也会为第三方代理密钥创建系统 VPN 接口。

| 协议 | 传输方式与特点 | Marvia 支持情况 |
|---|---|---|
| **[VP1](docs/protocol.md)** | 基于 Noise 的代理，可运行在 TLS/REALITY、WebSocket 或 QUIC 上；QUIC/UDP 不可用时回退到 TCP | 自有节点、Android 和 Windows |
| [WireGuard](https://www.wireguard.com/protocol/) | 基于 UDP 的 IP 隧道；不自带 HTTPS 伪装 | Android：外部密钥 |
| [OpenVPN](https://openvpn.net/community-docs/community-articles/openvpn-2-7-manual.html) | 基于 UDP 或 TCP、使用 TLS 的 IP 隧道；不是普通 HTTPS | 尚未集成 |
| [VLESS + REALITY](https://xtls.github.io/en/config/transports/reality.html) | TCP 代理，TLS 握手伪装为目标网站 | 节点和 Android |
| [Trojan](https://github.com/trojan-gfw/trojan/blob/master/docs/protocol.md) | TLS 内的代理，带网站伪装 | 节点和 Android |
| [Shadowsocks](https://shadowsocks.org/doc/what-is-shadowsocks.html) | 加密的 TCP/UDP 代理；默认不伪装成 HTTPS | Android：外部密钥 |
| [Hysteria 2](https://v2.hysteria.network/docs/developers/Protocol/) | 基于 QUIC/UDP 的代理，外观类似 HTTP/3；需要 UDP 可用 | Android：外部密钥 |

本次基准测试未运行 WireGuard、OpenVPN、Hysteria 2 或 Xray。
VP1 不兼容 WireGuard 或 Xray 客户端。

## 功能

| 客户端 | 服务器 |
|---|---|
| Android：支持 Marvia 和第三方密钥、节点选择、用量统计及 VPN 设置 | 面板与节点：限额、流量统计、备份和机器人 API |
| Windows：TUN 客户端 | VP1、VLESS 和 Trojan；经过校验的节点更新 |

## 与其他项目比较

| 项目 | 定位 | 主要能力 |
|---|---|---|
| **Marvia** | 面板、节点、自有 Android/Windows 客户端和 VP1 | 同时服务用户和运营者 |
| [Marzban](https://github.com/Gozargah/Marzban) | Xray 面板 | 定期重置流量额度、Telegram 集成 |
| [Remnawave](https://docs.rw/) | Xray 面板和节点 | Mihomo/sing-box 模板、设备控制 |
| [3x-ui](https://docs.sanaei.dev/docs/) | Xray 面板 | 协议和管理功能较丰富 |

Marvia 尚缺少每月自动重置额度、保留原有链接的用户迁移，以及
Clash/sing-box 订阅格式。功能比较以表中的项目文档为依据；目前没有
同等条件下的竞品速度排名。

## 开始使用

> **已有密钥？** 下载 [Android APK](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
> 或 [Windows 客户端](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe)，
> 在「服务器」页添加 `marvia://…` 链接，然后连接。

Android 还支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2 和 WireGuard 链接。
目前尚未上架 Google Play。

### 安装面板

需要 Linux 服务器和已配置 A 记录的域名。从[官方版本](https://github.com/jytt8u/marvia/releases/latest)
下载安装脚本：

```bash
curl -fsSL https://github.com/jytt8u/marvia/releases/latest/download/install-panel.sh -o install-panel.sh
sh install-panel.sh --domain panel.example.com --email you@example.com
```

保存安装时显示的管理员令牌，再在面板中创建节点和用户。
[完整指南（俄语）](docs/guide.md) · [机器人 API](docs/bot.md)。

## 尚待完成

- Google Play：新版实体设备验证、声明及测试。
  [发布计划（俄语）](docs/google-play.md)。
- 每月自动重置额度；保留原有链接的用户迁移。
- iOS、Clash/sing-box 订阅、TUIC、Shadowsocks 插件，以及共享密钥时
  单独撤销设备。
- 1.0 之前：稳定 API 和链接格式，验证回滚与分批更新节点。

Marvia 不销售 VPN 访问权限、不处理付款，也不代管服务器。1.0 之前的
版本可能调整 API 和链接格式。

## 文档与构建

[使用指南](docs/guide.md) · [架构](docs/architecture.md) ·
[VP1 协议](docs/protocol.md) · [隐私](docs/privacy.md) ·
[更新记录](CHANGELOG.md)

服务器程序需要 Go 1.26.6 或更新版本：

```bash
go test ./...
go vet ./...
go build -o bin/ ./...
```

Android 需单独使用 [`scripts/publish-apk.ps1`](scripts/publish-apk.ps1) 构建；
签名密钥不存放在本仓库。
