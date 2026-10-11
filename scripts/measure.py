#!/usr/bin/env python3
"""Start a release binary and sample Linux RSS/PSS/CPU/threads; never force GC."""
import argparse
import csv
import json
import os
from pathlib import Path
import subprocess
import time

parser = argparse.ArgumentParser()
parser.add_argument("--output", default="docs/evidence/memory")
parser.add_argument("--seconds", type=int, default=30)
parser.add_argument("--runs", type=int, default=3)
parser.add_argument("--sway-size", action="store_true", help="fix this process's Sway window to 1080x720")
parser.add_argument("--acceptance-report", action="store_true", help="request safe counters from the separate native acceptance driver")
parser.add_argument("command", nargs=argparse.REMAINDER)
args = parser.parse_args()
command = args.command
if command and command[0] == "--":
    command = command[1:]
if not command or args.seconds < 1 or args.runs < 1:
    parser.error("need a command and positive seconds/runs")
output = Path(args.output)
output.mkdir(parents=True, exist_ok=True)
ticks = os.sysconf("SC_CLK_TCK")
for run in range(1, args.runs + 1):
    prefix = output / f"run-{run}"
    config = prefix.with_suffix(".settings.json")
    extra = ["--report", str(prefix.with_suffix(".acceptance.json"))] if args.acceptance_report else []
    with prefix.with_suffix(".stderr.log").open("w") as stderr:
        proc = subprocess.Popen([command[0], "--config", str(config), "--quit-after", f"{args.seconds}s", "--metrics", str(prefix.with_suffix(".heap.csv")), *extra, *command[1:]], stderr=stderr)
    started = last = time.monotonic()
    old_cpu = 0
    sized = not args.sway_size
    mappings = []
    last_libraries = set()
    try:
        with prefix.with_suffix(".process.csv").open("w") as f:
            writer = csv.writer(f)
            writer.writerow(["elapsed_s", "rss_kib", "pss_kib", "cpu_percent", "threads"])
            while proc.poll() is None:
                now = time.monotonic()
                if not sized and now - started >= .5:
                    reply = json.loads(subprocess.check_output(["swaymsg", f"[pid={proc.pid}] floating enable, resize set width 1080 height 720"], text=True))
                    sized = all(item.get("success") for item in reply)
                try:
                    stat = Path(f"/proc/{proc.pid}/stat").read_text().rsplit(")", 1)[1].split()
                    status = dict(line.split(":", 1) for line in Path(f"/proc/{proc.pid}/status").read_text().splitlines() if ":" in line)
                    smaps = dict(line.split(":", 1) for line in Path(f"/proc/{proc.pid}/smaps_rollup").read_text().splitlines() if ":" in line)
                    now = time.monotonic()
                    cpu = (int(stat[11]) + int(stat[12])) / ticks
                    writer.writerow([round(now-started, 3), status.get("VmRSS", "0").split()[0], smaps.get("Pss", "0").split()[0], round(100*(cpu-old_cpu)/max(.001, now-last), 2), status["Threads"].strip()])
                    f.flush()
                    old_cpu, last = cpu, now
                    libraries = {Path(line.split()[-1]).name for line in Path(f"/proc/{proc.pid}/maps").read_text().splitlines() if ".so" in line and "/" in line}
                    if libraries != last_libraries:
                        mappings.append({"elapsed_s": round(now-started, 3), "rss_kib": int(status.get("VmRSS", "0").split()[0]), "pss_kib": int(smaps.get("Pss", "0").split()[0]), "loaded": sorted(libraries - last_libraries)})
                        last_libraries = libraries
                except (FileNotFoundError, ProcessLookupError):
                    pass
                if now - started > args.seconds + 10:
                    proc.terminate()
                    raise RuntimeError("application did not quit on time")
                time.sleep(.5)
        if proc.wait(timeout=10) != 0:
            raise RuntimeError("application exited with an error")
    finally:
        if proc.poll() is None:
            proc.terminate()
            proc.wait(timeout=10)
    print(f"saved {prefix}.*.csv")
    prefix.with_suffix(".libraries.json").write_text(json.dumps(mappings, indent=2) + "\n")
