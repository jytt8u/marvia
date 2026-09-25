<div align="center">

<img src="internal/look/assets/marvia-mark.png" alt="Marvia" height="76">

# Marvia

**A VPN for Android and Windows, with its own panel, nodes and VP1 protocol.**

[Русский](README.md) · English · [简体中文](README.zh-CN.md)

[![Release](https://img.shields.io/github/v/release/jytt8u/marvia?label=release)](https://github.com/jytt8u/marvia/releases/latest)
[![Checks](https://github.com/jytt8u/marvia/actions/workflows/check.yml/badge.svg)](https://github.com/jytt8u/marvia/actions)

[**Download**](https://github.com/jytt8u/marvia/releases/latest) ·
[**Install panel**](#install-the-panel) ·
[**Guide (Russian)**](docs/guide.md)

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

**Have a key?** Download the [Android APK](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
or [Windows client](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe),
add a `marvia://…` link in **Servers**, and connect. Android also accepts VLESS,
VMess, Trojan, Shadowsocks, Hysteria2 and WireGuard links. The app is not yet on
Google Play.

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
