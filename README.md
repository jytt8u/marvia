<div align="center">

<img src="docs/shots/readme-hero.svg" alt="Marvia: VPN для Android и Windows, панель и ноды" width="1200">

**VPN для Android и Windows. Свои панель, ноды и протокол VP1.**

[![RU](https://img.shields.io/badge/RU-RUSSIAN-a78bfa?style=flat-square)](README.md)
[![EN](https://img.shields.io/badge/EN-ENGLISH-51d9e3?style=flat-square)](README.en.md)
[![ZH](https://img.shields.io/badge/ZH-CHINESE-f5b765?style=flat-square)](README.zh-CN.md)

[![Релиз](https://img.shields.io/github/v/release/jytt8u/marvia?label=релиз&color=51d9e3)](https://github.com/jytt8u/marvia/releases/latest)
[![Проверки](https://github.com/jytt8u/marvia/actions/workflows/check.yml/badge.svg)](https://github.com/jytt8u/marvia/actions)

[![Android APK](https://img.shields.io/badge/ANDROID-APK-39c9bd?style=for-the-badge)](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
[![Windows EXE](https://img.shields.io/badge/WINDOWS-EXE-9a7af7?style=for-the-badge)](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe)
[![Панель](https://img.shields.io/badge/PANEL-INSTALL-e5aa61?style=for-the-badge)](#установка-панели)
[![Документация](https://img.shields.io/badge/DOCS-GUIDE-6887e8?style=for-the-badge)](docs/guide.md)

</div>

## Архитектура

<img src="docs/shots/readme-system.svg" alt="Техническая схема Marvia: управление доступом отделено от пути VPN-пакетов" width="1200">

Панель управляет доступом; пакеты идут через ноду без панели.
[Границы доверия](docs/architecture.md).

## Скорость

<img src="docs/shots/readme-benchmark.svg" alt="Замеры: 86 Мбит/с напрямую, 83 Мбит/с через VP1 на одном маршруте; отдельный локальный тест ядра VP1" width="1200">

Один [полевой замер](docs/guide.md): Дубай → Хельсинки, один поток, 14.09.2026.
**83 против 86 Мбит/с** — 96,5% скорости прямого пути. Локальный тест ядра
VP1 на Ryzen 7 7700: медиана **638 МБ/с**, это не скорость интернета.
[Методика и исходные результаты](docs/performance.md). Замеров конкурентов
на том же сервере и маршруте пока нет.

## Что внутри

| Клиент | Сервер |
|---|---|
| Android: свои и чужие ключи, выбор ноды, статистика, настройки VPN | Панель и ноды: лимиты, учёт, резервные копии, API бота |
| Windows: TUN-клиент | VP1, VLESS и Trojan; проверяемое обновление нод |

## Чем отличается от других

| Проект | Основной фокус | Сильная сторона |
|---|---|---|
| **Marvia** | Панель + ноды + свои Android/Windows + VP1 | один комплект для покупателя и владельца |
| [Marzban](https://github.com/Gozargah/Marzban) | Xray-панель | периодические квоты, Telegram-бот; есть отдельный [Nabzram](https://github.com/Gozargah/Nabzram) |
| [Remnawave](https://docs.rw/) | Xray-панель и ноды | шаблоны Mihomo/sing-box, контроль устройств |
| [3x-ui](https://docs.sanaei.dev/docs/) | Xray-панель | широкий выбор протоколов и админ-инструментов |

Marvia пока уступает по автоматическому месячному сбросу квот, импорту клиентов
и форматам Clash/sing-box. Сравнение основано на документации проектов по
ссылкам в таблице; сравнимых замеров скорости конкурентов пока нет.

## Начать

> [!TIP]
> **Получили ключ?** Скачайте [Android APK](https://github.com/jytt8u/marvia/releases/latest/download/marvia-android.apk)
> или [Windows-клиент](https://github.com/jytt8u/marvia/releases/latest/download/marvia-windows.exe),
> добавьте ссылку `marvia://…` во вкладке «Серверы» и подключитесь.

Android также принимает VLESS, VMess, Trojan, Shadowsocks, Hysteria2 и WireGuard.
В Google Play приложения пока нет.

### Установка панели

Нужны Linux-сервер и домен с A-записью. Установщик берётся из
[официального релиза](https://github.com/jytt8u/marvia/releases/latest):

```bash
curl -fsSL https://github.com/jytt8u/marvia/releases/latest/download/install-panel.sh -o install-panel.sh
sh install-panel.sh --domain panel.example.com --email you@example.com
```

Сохраните показанный при установке админский токен. Затем создайте ноду и
клиента в панели. [Полный порядок](docs/guide.md) · [API бота](docs/bot.md).

## Что пока не готово

- Google Play: физическая проверка сборки, политика конфиденциальности в
  приложении, декларации и тестирование. [План публикации](docs/google-play.md).
- Автоматический месячный сброс квоты и импорт покупателей с сохранением ссылок.
- iOS, подписки Clash/sing-box, TUIC, плагины Shadowsocks и раздельный отзыв
  устройств с общим ключом.
- До 1.0: стабилизация API и ссылок, проверка отката и поэтапное обновление нод.

Marvia не продаёт доступ, не принимает платежи и не размещает серверы.
Версия ниже 1.0 означает, что API и форматы могут меняться.

## Документы и сборка

[Руководство](docs/guide.md) ·
[Архитектура](docs/architecture.md) ·
[Протокол VP1](docs/protocol.md) ·
[Конфиденциальность](docs/privacy.md) ·
[История версий](CHANGELOG.md)

Для серверных бинарников нужен Go 1.26.6+:

```bash
go test ./...
go vet ./...
go build -o bin/ ./...
```

Android собирается отдельно через [`scripts/publish-apk.ps1`](scripts/publish-apk.ps1);
ключ подписи хранится вне репозитория.
