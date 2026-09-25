<div align="center">

<img src="docs/shots/readme-brand.svg" alt="Original Marvia ribbon mark and wordmark" width="1200">

**A VPN for Android and Windows, with its own panel, nodes and VP1 protocol.**

[![RU](https://img.shields.io/badge/RU-RUSSIAN-a78bfa?style=flat-square)](README.md)
[![EN](https://img.shields.io/badge/EN-ENGLISH-51d9e3?style=flat-square)](README.en.md)
[![ZH](https://img.shields.io/badge/ZH-CHINESE-f5b765?style=flat-square)](README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/jytt8u/marvia?label=release)](https://github.com/jytt8u/marvia/releases/latest)
[![Checks](https://github.com/jytt8u/marvia/actions/workflows/check.yml/badge.svg)](https://github.com/jytt8u/marvia/actions)

[![Android APK](https://img.shields.io/badge/ANDROID-APK-39c9bd?style=for-the-badge)](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
[![Windows EXE](https://img.shields.io/badge/WINDOWS-EXE-9a7af7?style=for-the-badge)](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe)
[![Panel](https://img.shields.io/badge/PANEL-INSTALL-e5aa61?style=for-the-badge)](#install-the-panel)
[![Docs](https://img.shields.io/badge/DOCS-GUIDE-6887e8?style=for-the-badge)](docs/guide.md)

</div>

## Architecture

<img src="docs/shots/readme-system.svg" alt="Technical diagram: access control is separate from the VPN data path" width="1200">

The panel grants access; VPN packets pass through a node, not the panel.
[Trust boundaries](docs/architecture.md).

## Speed

<img src="docs/shots/readme-benchmark.svg" alt="Measurements: 86 Mb/s direct and 83 Mb/s through VP1 on one route; a separate local VP1 benchmark" width="1200">

One [field measurement](docs/guide.md), Dubai → Helsinki, one stream,
14 September 2026: **83 vs 86 Mb/s** through VP1 and directly, respectively
(96.5%). A separate local VP1 code-path benchmark on a Ryzen 7 7700 reached a
**638 MB/s median**; that is not internet speed. [Method and raw runs](docs/performance.md).
No competitor has been measured on the same machine and route.

## Protocols

WireGuard and OpenVPN tunnel IP packets; the others below are proxies. On
Android, Marvia also creates a system VPN interface for third-party proxy keys.

| Protocol | Transport and distinction | Marvia support |
|---|---|---|
| **[VP1](docs/protocol.md)** | Noise proxy over TLS/REALITY, WebSocket or QUIC; falls back to TCP if QUIC/UDP is unavailable | First-party node, Android and Windows |
| [WireGuard](https://www.wireguard.com/protocol/) | IP tunnel over UDP; no built-in HTTPS disguise | Android: third-party key |
| [OpenVPN](https://openvpn.net/community-docs/community-articles/openvpn-2-7-manual.html) | IP tunnel over UDP or TCP with TLS; a separate protocol, not ordinary HTTPS | Not integrated |
| [VLESS + REALITY](https://xtls.github.io/en/config/transports/reality.html) | TCP proxy with TLS handshake disguised as a target site | Node and Android |
| [Trojan](https://github.com/trojan-gfw/trojan/blob/master/docs/protocol.md) | Proxy inside TLS with a cover site | Node and Android |
| [Shadowsocks](https://shadowsocks.org/doc/what-is-shadowsocks.html) | Encrypted TCP/UDP proxy; does not resemble HTTPS on its own | Android: third-party key |
| [Hysteria 2](https://v2.hysteria.network/docs/developers/Protocol/) | QUIC/UDP proxy with HTTP/3 appearance; requires working UDP | Android: third-party key |

**No speed ranking yet:** each protocol needs repeated measurements on the same
server, route and client. Only VP1 has been measured so far; see the
[method](docs/performance.md). VP1 is not compatible with WireGuard or Xray clients.

## What is included

| Client | Server |
|---|---|
| Android: Marvia and third-party keys, node selection, usage and VPN settings | Panel and nodes: limits, accounting, backups and bot API |
| Windows: TUN client | VP1, VLESS and Trojan; verified node updates |

## Compared with other projects

| Project | Focus | Notable capability |
|---|---|---|
| **Marvia** | Panel + nodes + first-party Android/Windows + VP1 | One stack for users and operators |
| [Marzban](https://github.com/Gozargah/Marzban) | Xray panel | Scheduled quotas and Telegram integration |
| [Remnawave](https://docs.rw/) | Xray panel and nodes | Mihomo/sing-box templates, device controls |
| [3x-ui](https://docs.sanaei.dev/docs/) | Xray panel | Broad protocol and administration support |

Marvia still lacks automatic monthly quota resets, buyer migration that keeps
links intact, and Clash/sing-box subscriptions. The linked project documentation
supports the feature comparison; no matched speed ranking is available.

## Get started

> [!TIP]
> **Have a key?** Download the [Android APK](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
> or [Windows client](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe),
> add a `marvia://…` link in **Servers**, and connect.

Android also accepts VLESS, VMess, Trojan, Shadowsocks, Hysteria2 and WireGuard
links. The app is not yet on Google Play.

### Install the panel

Use a Linux server and a domain with an A record. Download the installer from
the [official release](https://github.com/jytt8u/marvia/releases/latest):

```bash
curl -fsSL https://github.com/jytt8u/marvia/releases/latest/download/install-panel.sh -o install-panel.sh
sh install-panel.sh --domain panel.example.com --email you@example.com
```

Save the admin token displayed during installation, then create a node and a
client in the panel. [Full guide (Russian)](docs/guide.md) · [Bot API](docs/bot.md).

## Still to do

- Google Play: physical-device verification, in-app privacy policy, declarations
  and testing. [Publication plan (Russian)](docs/google-play.md).
- Monthly quota resets and buyer migration with stable links.
- iOS, Clash/sing-box subscriptions, TUIC, Shadowsocks plugins, and per-device
  revocation when a key is shared.
- Before 1.0: stabilize APIs and link formats; validate rollback and staged node updates.

Marvia does not sell VPN access, take payments or host servers. Versions below
1.0 may change APIs and link formats.

## Docs and build

[Guide](docs/guide.md) · [Architecture](docs/architecture.md) ·
[VP1 protocol](docs/protocol.md) · [Privacy](docs/privacy.md) ·
[Changelog](CHANGELOG.md)

Server binaries require Go 1.26.6 or newer:

```bash
go test ./...
go vet ./...
go build -o bin/ ./...
```

Android is built separately with [`scripts/publish-apk.ps1`](scripts/publish-apk.ps1);
the signing key is stored outside this repository.
