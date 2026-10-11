#!/usr/bin/env python3
"""Measure a generated 440 Hz tone in memory; persist statistics, never audio."""
import argparse
import array
import json
import math
from pathlib import Path
import subprocess
import time

parser = argparse.ArgumentParser()
parser.add_argument("--seconds", type=int, default=8)
parser.add_argument("--output", required=True)
args = parser.parse_args()
sink = subprocess.check_output(["pactl", "get-default-sink"], text=True).strip()
proc = subprocess.Popen(["parec", "--device=" + sink + ".monitor", "--format=s16le", "--rate=48000", "--channels=2"], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
try:
    raw = proc.stdout.read(args.seconds * 48000 * 4)
finally:
    proc.terminate()
    proc.wait(timeout=5)
values = array.array("h", raw)
mono = values[::2]
stats = []
for second in range(len(mono) // 48000):
    samples = mono[second * 48000:(second + 1) * 48000]
    energy = sum(value * value for value in samples)
    real = sum(value * math.cos(2 * math.pi * 440 * i / 48000) for i, value in enumerate(samples))
    imaginary = sum(value * math.sin(2 * math.pi * 440 * i / 48000) for i, value in enumerate(samples))
    stats.append({"second": second, "rms_s16": math.sqrt(energy / len(samples)), "440hz_energy_ratio": min(1, 2 * (real * real + imaginary * imaginary) / (len(samples) * energy)) if energy else 0})
Path(args.output).write_text(json.dumps({"output": "default sink monitor", "frames": len(mono), "audio_persisted": False, "seconds": stats}, indent=2) + "\n")
print("saved audio signal statistics (no audio recording)")
