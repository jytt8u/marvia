"""Схема механизмов защиты для README. Только стандартная библиотека Python.

Источники и границы сравнения: docs/security-comparison.md.
Запуск: python scripts/draw-security-comparison.py
"""

from html import escape
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
FG, DIM, LINE = "#edf1f5", "#adb7c3", "#46505b"

COPY = {
    "ru": {
        "title": "VP1: защита отдельным слоем",
        "subtitle": "Noise проверяет ключ ноды и шифрует данные внутри TLS.",
        "client": "Клиент", "node": "Нода",
        "outer": "TLS 1.3 · внешний защищённый канал",
        "inner": "Noise IK · ключи клиента и ноды",
        "payload": "ChaCha20-Poly1305 · данные и контроль целостности",
        "scope": "Показан участок клиент → нода. Для защиты до сайта нужен HTTPS самого сайта.",
        "headers": ["СТЕК", "ЗАЩИТА ДАННЫХ", "ПРОВЕРКА СЕРВЕРА", "ДОСТУП КЛИЕНТА"],
        "rows": [
            ["VP1 + TLS", "Noise + TLS", "Ключ ноды +", "TLS-сертификат", "Ключ клиента", "X25519"],
            ["WireGuard", "Noise IKpsk2", "Ключ узла", "X25519", "Ключ клиента", "X25519"],
            ["VLESS + TLS*", "TLS", "TLS-сертификат", "", "UUID внутри TLS", ""],
            ["VLESS + REALITY*", "REALITY / TLS", "Ключ REALITY", "", "UUID + shortId", ""],
            ["Trojan + TLS", "TLS", "TLS-сертификат", "", "Пароль: SHA-224", "внутри TLS"],
        ],
        "foot": [
            '* VLESS показан с encryption="none". Режим VLESS Encryption добавляет собственное шифрование.',
            "Предполагаются корректные ключи и проверка TLS-сертификатов. Маскировка не гарантирует обход DPI.",
            "Сравнение механизмов, не рейтинг безопасности. Дополнительный слой не доказывает превосходство.",
        ],
        "source": "Источники и ограничения: docs/security-comparison.md · 26.09.2026",
    },
    "en": {
        "title": "VP1: a separate security layer",
        "subtitle": "Noise verifies the node key and encrypts payloads inside TLS.",
        "client": "Client", "node": "Node",
        "outer": "TLS 1.3 · outer secure channel",
        "inner": "Noise IK · client and node keys",
        "payload": "ChaCha20-Poly1305 · payload encryption and integrity",
        "scope": "Scope: client → node. Site HTTPS is needed for protection through to the destination.",
        "headers": ["STACK", "DATA PROTECTION", "SERVER VERIFICATION", "CLIENT ACCESS"],
        "rows": [
            ["VP1 + TLS", "Noise + TLS", "Node key +", "TLS certificate", "Client key", "X25519"],
            ["WireGuard", "Noise IKpsk2", "Peer key", "X25519", "Client key", "X25519"],
            ["VLESS + TLS*", "TLS", "TLS certificate", "", "UUID inside TLS", ""],
            ["VLESS + REALITY*", "REALITY / TLS", "REALITY key", "", "UUID + shortId", ""],
            ["Trojan + TLS", "TLS", "TLS certificate", "", "Password: SHA-224", "inside TLS"],
        ],
        "foot": [
            '* VLESS rows use encryption="none". The VLESS Encryption mode adds its own payload encryption.',
            "Assumes correct keys and TLS certificate verification. Camouflage does not guarantee DPI evasion.",
            "Mechanisms, not a security ranking. An extra layer does not establish overall superiority.",
        ],
        "source": "Sources and limits: docs/security-comparison.md · 26 Sep 2026",
    },
}


def draw(lang, copy):
    parts = [
        '<svg xmlns="http://www.w3.org/2000/svg" width="1600" height="1200" viewBox="0 0 1600 1200" role="img" aria-labelledby="title desc">',
        f'<title id="title">{escape(copy["title"])}</title>',
        f'<desc id="desc">{escape(copy["subtitle"] + " " + copy["foot"][2])}</desc>',
        '<rect width="1600" height="1200" fill="#191d22"/>',
        '<g font-family="Segoe UI, Arial, sans-serif">',
    ]

    def text(x, y, content, size=27, color=FG, weight=400, anchor="start"):
        parts.append(f'<text x="{x}" y="{y}" font-size="{size}" fill="{color}" font-weight="{weight}" text-anchor="{anchor}">{escape(content)}</text>')

    def rect(x, y, w, h, fill, stroke=LINE, radius=12):
        parts.append(f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{radius}" fill="{fill}" stroke="{stroke}"/>')

    def line(x1, y1, x2, y2):
        parts.append(f'<path d="M{x1} {y1}H{x2}" fill="none" stroke="{LINE}" stroke-width="2"/>')

    text(56, 58, "MARVIA / SECURITY ARCHITECTURE", 20, DIM, 600)
    text(56, 130, copy["title"], 48, weight=650)
    text(56, 178, copy["subtitle"], 27, DIM)

    # Вложенные прямоугольники обозначают границы шифрования, не рейтинг.
    rect(260, 226, 1080, 208, "#21272e", "#687684")
    text(292, 265, copy["outer"], 25, DIM)
    rect(292, 284, 1016, 122, "#303944", "#a9b6c3")
    text(324, 327, copy["inner"], 28, weight=600)
    text(324, 372, copy["payload"], 25, DIM)
    line(185, 336, 260, 336)
    line(1340, 336, 1415, 336)
    text(114, 346, copy["client"], 29, anchor="middle")
    text(1480, 346, copy["node"], 29, anchor="middle")
    text(56, 483, copy["scope"], 24, DIM)

    xs = [80, 442, 784, 1170]
    for x, header in zip(xs, copy["headers"]):
        text(x, 554, header, 21, DIM, 600)
    for i, row in enumerate(copy["rows"]):
        top = 575 + i * 84
        if i == 0:
            rect(56, top, 1488, 80, "#303944", "#8795a4", 8)
        else:
            line(56, top, 1544, top)
        text(xs[0], top + 49, row[0], 28, weight=650 if i == 0 else 500)
        text(xs[1], top + 49, row[1], 27)
        for x, primary, secondary in [(xs[2], row[2], row[3]), (xs[3], row[4], row[5])]:
            text(x, top + (34 if secondary else 49), primary, 26)
            if secondary:
                text(x, top + 64, secondary, 23, DIM)
    for i, note in enumerate(copy["foot"]):
        text(56, 1040 + i * 34, note, 22, DIM)
    line(56, 1135, 1544, 1135)
    text(56, 1173, copy["source"], 20, DIM)
    parts.extend(["</g>", "</svg>"])
    (ROOT / f"docs/shots/readme-security-{lang}.svg").write_text("\n".join(parts) + "\n", encoding="utf-8", newline="\n")


for language, content in COPY.items():
    draw(language, content)
