# Изображения README

- `readme-brand.png` — предоставленный владельцем оригинал знака и надписи
  Marvia от 08.09.2026. Используется целиком, без перерисовки и растяжения.
- `android-home.png`, `android-settings.png`, `android-advanced.png` — прямые
  снимки Android-эмулятора 1080 × 2400, Marvia 0.12.2, 26.09.2026. Стандартная
  серая тема, ключ не добавлен. Изображения интерфейса не перекрашены и не
  дополнены вымышленными данными.
- `windows.png` — сохранённый снимок Windows 0.9.3; `panel-clients.png` —
  сохранённый снимок локального стенда панели. В README они подписаны отдельно.
- `readme-flow.svg` — схема управления доступом и пути трафика.
- `readme-security-ru.svg`, `readme-security-en.svg` — схема Noise внутри TLS
  и сравнение механизмов защиты пяти конфигураций, без баллов безопасности.
  Источники: [security-comparison.md](../security-comparison.md).
  Пересоздание: `python scripts/draw-security-comparison.py`, без зависимостей.
- `readme-vp1-progress.svg` — сравнение VP1 до и после оптимизации для README.
  `readme-protocol-benchmark.svg` — полное сравнение протоколов в `performance.md`.
  Оба графика Matplotlib строятся из
  [`2026-09-26-record-fit/summary.json`](../benchmarks/2026-09-26-record-fit/summary.json).
  Пересоздание: `python scripts/plot-benchmark.py` из корня проекта
  (понадобится `matplotlib`). [Условия измерений](../performance.md).

Палитра оформления — графит, сталь, светло-серый. Ширина и высота изображений
меняются пропорционально. Статусные цвета внутри реального интерфейса не
подменяются цветами оформления документации.
