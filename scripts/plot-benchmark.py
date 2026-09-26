"""Строит сравнение протоколов и прогресс VP1 из опубликованных результатов.

Зависимость: matplotlib. Запуск из любой папки: python scripts/plot-benchmark.py
"""

import json
from pathlib import Path

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt

root = Path(__file__).resolve().parents[1]
data = json.loads((root / "docs/benchmarks/2026-09-26-record-fit/summary.json").read_text(encoding="utf-8"))
rows = {row["mode"]: row for row in data["results"]["after"]}
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
fig.text(0.04, 0.90, "VP1: fewer TLS records, same encryption.", color=fg, fontsize=19, weight="bold")
fig.text(0.04, 0.83, "Unreleased code · TCP loopback · Ryzen 7 7700 · 26 Sep 2026", color=dim, fontsize=12)
fig.add_artist(plt.Line2D([0.04, 0.96], [0.10, 0.10], color="#414952", linewidth=0.8))
fig.text(0.04, 0.045, "Median + min–max · 512 MiB / run · handshake excluded · not Internet speed", color=dim, fontsize=10)
fig.text(0.96, 0.045, "MB/s ≠ Mb/s", color=dim, fontsize=10, ha="right")
fig.savefig(root / "docs/shots/readme-protocol-benchmark.svg", metadata={"Date": None, "Title": "Marvia local protocol benchmark"})
plt.close(fig)

# Стабильные переводы строк и отсутствие пробелов в конце строк SVG.
svg = root / "docs/shots/readme-protocol-benchmark.svg"
svg.write_text("\n".join(line.rstrip() for line in svg.read_text(encoding="utf-8").splitlines()) + "\n", encoding="utf-8", newline="\n")

# На обложке сравниваются две версии VP1, а не разные протоколы.
# Та же серия и нулевая ось сохраняют смысл измерений при смене подачи.
before = next(row for row in data["results"]["before"] if row["mode"] == "VP1+TLS")
after = rows["VP1+TLS"]
gain = (after["median_MBps"] / before["median_MBps"] - 1) * 100
fig = plt.figure(figsize=(12, 5.0), facecolor=bg)
fig.text(0.045, 0.88, "VP1 / PERFORMANCE UPDATE", color=dim, fontsize=12, fontfamily="DejaVu Sans Mono")
fig.text(0.045, 0.74, f"+{gain:.1f}%", color=fg, fontsize=40, weight="bold")
fig.text(0.39, 0.79, "More throughput. Same encryption.", color=fg, fontsize=17, weight="bold")
fig.text(0.39, 0.73, "Compared with the previous VP1 build", color=dim, fontsize=12)
ax = fig.add_axes([0.16, 0.24, 0.66, 0.36], facecolor=bg)
for y, (label, row) in enumerate([("Before", before), ("Optimized", after)]):
    median = row["median_MBps"]
    ax.barh(y, median, height=0.38, color="#73808e" if y == 0 else "#c4cdd7", zorder=2)
    ax.errorbar(median, y, xerr=[[median - row["min_MBps"]], [row["max_MBps"] - median]],
                fmt="none", ecolor=fg, elinewidth=1.5, capsize=5, capthick=1.5, zorder=3)
    ax.text(1.02, y, f"{median:.1f}", transform=ax.get_yaxis_transform(), va="center",
            color=fg, fontsize=19, fontfamily="DejaVu Sans Mono")
ax.set_yticks([0, 1], ["Before", "Optimized"])
ax.set_ylim(1.65, -0.65)
ax.set_xlim(0, 800)
ax.set_xticks([0, 200, 400, 600, 800])
ax.tick_params(axis="y", colors=fg, length=0, labelsize=13, pad=16)
ax.tick_params(axis="x", colors=dim, length=0, labelsize=10, pad=9)
ax.grid(axis="x", color="#353d46", linewidth=0.7, zorder=0)
for spine in ax.spines.values():
    spine.set_visible(False)
fig.text(0.89, 0.60, "MB/s", color=dim, fontsize=11, ha="center")
fig.add_artist(plt.Line2D([0.045, 0.955], [0.15, 0.15], color="#414952", linewidth=0.8))
fig.text(0.045, 0.09, "5 paired runs · median + min–max · 512 MiB / run · Ryzen 7 7700 · 26 Sep 2026", color=dim, fontsize=10)
fig.text(0.045, 0.045, "Unreleased code · VP1 + TLS · TCP loopback · no TUN · handshake excluded · not Internet speed", color=dim, fontsize=10)
svg = root / "docs/shots/readme-vp1-progress.svg"
fig.savefig(svg, metadata={"Date": None, "Title": "VP1: 55.9% more local throughput versus the previous build"})
plt.close(fig)
svg.write_text("\n".join(line.rstrip() for line in svg.read_text(encoding="utf-8").splitlines()) + "\n", encoding="utf-8", newline="\n")
