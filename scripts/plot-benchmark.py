"""Строит график README из опубликованных результатов, без ручной подгонки столбцов.

Зависимость: matplotlib. Запуск из любой папки: python scripts/plot-benchmark.py
"""

import json
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt

root = Path(__file__).resolve().parents[1]
data = json.loads((root / "docs/benchmarks/2026-09-26-summary.json").read_text(encoding="utf-8"))
rows = {row["mode"]: row for row in data["results"]}
order = ["VP1+TLS", "VLESS+TLS", "Trojan+TLS", "TLS"]
plt.rcParams.update({"font.family": "DejaVu Sans", "svg.fonttype": "path", "svg.hashsalt": "marvia-benchmark"})
bg, fg, dim = "#1c2025", "#edf1f5", "#a8b2be"
fig = plt.figure(figsize=(12, 5.5), facecolor=bg)
ax = fig.add_axes([0.235, 0.19, 0.525, 0.57], facecolor=bg)
for y, mode in enumerate(order):
    row = rows[mode]
    median = row["median_MBps"]
    ax.barh(y, median, height=0.40, color="#c4cdd7" if y == 0 else "#73808e", zorder=2)
    ax.errorbar(median, y, xerr=[[median - row["min_MBps"]], [row["max_MBps"] - median]],
                fmt="none", ecolor="#f4f6f8", elinewidth=1.5, capsize=5, capthick=1.5, zorder=3)
    ax.text(1.22, y, f"{median:.1f}", transform=ax.get_yaxis_transform(), ha="right", va="center",
            color=fg, fontsize=17, fontfamily="DejaVu Sans Mono")
    ax.text(1.36, y, "MB/s", transform=ax.get_yaxis_transform(), ha="right", va="center", color=dim, fontsize=12)
ax.set_yticks(range(4), ["VP1 + TLS", "VLESS + TLS", "Trojan + TLS", "TLS baseline"])
ax.set_ylim(3.6, -0.6)
ax.set_xlim(0, 1000)
ax.set_xticks([0, 250, 500, 750, 1000])
ax.tick_params(axis="y", colors=fg, length=0, labelsize=15, pad=19)
ax.tick_params(axis="x", colors=dim, length=0, labelsize=10, pad=10)
ax.grid(axis="x", color="#353d46", linewidth=0.7, zorder=0)
for spine in ax.spines.values():
    spine.set_visible(False)
fig.text(0.04, 0.90, "Same machine. Same TLS. Five runs.", color=fg, fontsize=21, weight="semibold")
fig.text(0.04, 0.83, "Marvia implementations · TCP loopback · Ryzen 7 7700 · 26 Sep 2026", color=dim, fontsize=12)
fig.add_artist(plt.Line2D([0.04, 0.96], [0.10, 0.10], color="#414952", linewidth=0.8))
fig.text(0.04, 0.045, "Median + min–max · 512 MiB / run · handshake excluded · not Internet speed", color=dim, fontsize=10)
fig.text(0.96, 0.045, "MB/s ≠ Mb/s", color=dim, fontsize=10, ha="right")
fig.savefig(root / "docs/shots/readme-protocol-benchmark.svg", metadata={"Date": None, "Title": "Marvia local protocol benchmark"})
plt.close(fig)
