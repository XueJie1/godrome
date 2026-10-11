#!/usr/bin/env python3
"""Plot saved measurements only, without opening a GUI or forcing GC."""
import argparse
import csv
import json
from pathlib import Path

import matplotlib
matplotlib.use("Agg")
import matplotlib.pyplot as plt

parser = argparse.ArgumentParser()
parser.add_argument("directory")
args = parser.parse_args()
directory = Path(args.directory)
summary = json.loads((directory / "summary.json").read_text())
fig, axes = plt.subplots(3, 1, figsize=(10, 8), sharex=True, layout="constrained")
for run in range(1, 4):
    with (directory / "switch" / f"run-{run}.process.csv").open() as f:
        process = list(csv.DictReader(f))
    with (directory / "switch" / f"run-{run}.heap.csv").open() as f:
        heap = list(csv.DictReader(f))
    for ax, rows, field, scale in ((axes[0], process, "rss_kib", 1024), (axes[1], process, "pss_kib", 1024), (axes[2], heap, "heap_alloc_bytes", 1048576)):
        ax.plot([float(row["elapsed_s"]) / 60 for row in rows], [float(row[field]) / scale for row in rows], label=f"Run {run}", linewidth=1, alpha=.8)
for ax, label in zip(axes, ("RSS (MiB)", "PSS (MiB)", "Go HeapAlloc (MiB)")):
    ax.set_ylabel(label)
    ax.grid(alpha=.2)
    ax.legend(loc="best", ncol=3)
axes[0].set_title("Native UI + in-process libmpv: 3 x 30 min switching\nPrivate headless Sway; null audio; no forced GC")
axes[-1].set_xlabel("Elapsed time (minutes)")
fig.savefig(directory / "switch-trend.png", dpi=160)
plt.close(fig)

scenarios = [name for name in summary if name != "switch"]
fig, axes = plt.subplots(2, 1, figsize=(12, 7), sharex=True, layout="constrained")
for run in range(3):
    x = [i + (run - 1) * .23 for i in range(len(scenarios))]
    for ax, metric in zip(axes, ("rss_median", "pss_median")):
        ax.bar(x, [summary[name][run][metric] for name in scenarios], width=.22, label=f"Run {run + 1}")
for ax, label in zip(axes, ("RSS median (MiB)", "PSS median (MiB)")):
    ax.set_ylabel(label)
    ax.grid(axis="y", alpha=.2)
    ax.legend(ncol=3)
axes[0].set_title("60-second scene comparisons: medians of the second half\nPrivate headless Sway; browser fixture is in process; null audio")
axes[-1].set_xticks(range(len(scenarios)), scenarios, rotation=25, ha="right")
fig.savefig(directory / "scene-comparison.png", dpi=160)
plt.close(fig)
