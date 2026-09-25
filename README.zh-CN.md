<div align="center">

<img src="internal/look/assets/marvia-mark.png" alt="Marvia" height="76">

# Marvia

**面向 Android 和 Windows 的 VPN，包含自有面板、节点和 VP1 协议。**

[Русский](README.md) · [English](README.en.md) · 简体中文

[![版本](https://img.shields.io/github/v/release/jytt8u/marvia?label=release)](https://github.com/jytt8u/marvia/releases/latest)
[![检查](https://github.com/jytt8u/marvia/actions/workflows/check.yml/badge.svg)](https://github.com/jytt8u/marvia/actions)

[**下载**](https://github.com/jytt8u/marvia/releases/latest) ·
[**安装面板**](#安装面板) ·
[**使用指南（俄语）**](docs/guide.md)

</div>

## 架构

<img src="docs/shots/readme-system.svg" alt="技术架构图：访问控制与 VPN 数据传输分离" width="1200">

面板管理访问权限；VPN 数据包由节点转发，不经过面板。
[信任边界](docs/architecture.md)。

## 速度

<img src="docs/shots/readme-benchmark.svg" alt="同一路线测速：直连 86 Mb/s、经 VP1 为 83 Mb/s；另有独立的本机 VP1 基准测试" width="1200">

一次[实际路线测试](docs/guide.md)：2026 年 9 月 14 日，迪拜 → 赫尔辛基，
单连接。经 VP1 **83 Mb/s**，直连 **86 Mb/s**，比值为 96.5%。另一次在
Ryzen 7 7700 上运行的 VP1 本机测试中位数为 **638 MB/s**；这不是互联网
连接速度。[测试方法和原始结果](docs/performance.md)。目前没有相同设备和
路线下的竞品测速。

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

**已有密钥？** 下载 [Android APK](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
或 [Windows 客户端](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe)，
在「服务器」页添加 `marvia://…` 链接，然后连接。Android 还支持 VLESS、
VMess、Trojan、Shadowsocks、Hysteria2 和 WireGuard 链接。目前尚未上架
Google Play。

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

- Google Play：实体设备验证、应用内隐私政策、声明及测试。
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
