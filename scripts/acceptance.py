#!/usr/bin/env python3
"""Run three isolated, sequential native-window measurements per scenario."""
import argparse
import csv
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess

ROOT = Path(__file__).resolve().parents[1]


def summarize(directory):
    results = []
    for process_file in sorted(directory.glob("run-*.process.csv")):
        prefix = str(process_file).removesuffix(".process.csv")
        with process_file.open() as f:
            process = list(csv.DictReader(f))
        with open(prefix + ".heap.csv") as f:
            heap = list(csv.DictReader(f))
        if not process or not heap:
            raise RuntimeError("empty measurement")
        duration = float(process[-1]["elapsed_s"])
        stable_start = max(5, duration / 2)
        summary = {"run": process_file.name.split(".")[0], "duration_s": duration, "samples": len(process), "stable_window_start_s": stable_start}
        for rows, columns in ((process, {"rss_kib": "rss", "pss_kib": "pss", "cpu_percent": "cpu", "threads": "threads"}), (heap, {"heap_alloc_bytes": "heap", "goroutines": "goroutines"})):
            for column, name in columns.items():
                scale = 1024 if column.endswith("_kib") else 1024 * 1024 if column.endswith("_bytes") else 1
                values = [float(row[column]) / scale for row in rows]
                stable = [float(row[column]) / scale for row in rows if float(row["elapsed_s"]) >= stable_start]
                summary[name + "_peak"] = max(values)
                summary[name + "_median"] = statistics.median(stable)
                if duration >= 1700:
                    early = [float(row[column]) / scale for row in rows if 300 <= float(row["elapsed_s"]) < 600]
                    late = [float(row[column]) / scale for row in rows if float(row["elapsed_s"]) >= 1500]
                    summary[name + "_early_5min_median"] = statistics.median(early)
                    summary[name + "_last_5min_median"] = statistics.median(late)
                    summary[name + "_delta"] = statistics.median(late) - statistics.median(early)
        counters = Path(prefix + ".acceptance.json")
        if counters.exists():
            summary["acceptance"] = json.loads(counters.read_text())
        results.append(summary)
    return results


def validate(scenario, results, runs, seconds):
    if len(results) != runs:
        raise RuntimeError("missing runs for " + scenario)
    for result in results:
        if result["duration_s"] < seconds - 2:
            raise RuntimeError("short run for " + scenario)
        if scenario == "empty":
            continue
        counters = result.get("acceptance")
        if not counters or counters["error_count"] or counters["elapsed_s"] < seconds - 1:
            raise RuntimeError("failed or unfinished native scenario: " + scenario)
        if counters["max_cached_pages"] > 5:
            raise RuntimeError("page budget exceeded")
        if scenario.startswith("covers") and counters["cover_requests"] < 100:
            raise RuntimeError("cover scrolling workload did not execute")
        if scenario == "library" and counters["max_songs"] != 10000:
            raise RuntimeError("large song library workload did not execute")
        if scenario == "paused" and counters["pause_transitions"] < 1:
            raise RuntimeError("pause workload did not execute")
        if scenario in ("mp3", "flac", "m4a", "opus", "switch") and counters["playing_transitions"] < 1:
            raise RuntimeError("playback workload did not execute")
        if scenario == "switch" and counters["playing_transitions"] < seconds / 2 - 5:
            raise RuntimeError("switch workload did not complete expected playback count")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--output", default="docs/evidence/acceptance")
    parser.add_argument("--seconds", type=int, default=60)
    parser.add_argument("--switch-seconds", type=int, default=1800)
    parser.add_argument("--runs", type=int, default=3)
    parser.add_argument("--scenarios", nargs="+", default=["switch", "empty", "albums", "library", "covers", "mp3", "flac", "m4a", "opus", "paused", "covers-cpu", "covers-gpu"])
    parser.add_argument("--summarize-only", action="store_true")
    args = parser.parse_args()
    os.chdir(ROOT)
    output = Path(args.output)
    output.mkdir(parents=True, exist_ok=True)
    if not args.summarize_only:
        metadata = {"runs": args.runs, "seconds": args.seconds, "switch_seconds": args.switch_seconds, "window_logical": [1080, 720], "sampling_s": 0.5, "heap_sampling_s": 1, "forced_gc": False, "audio_output": "null", "fixture": "in-process generated loopback Subsonic; no personal content", "binary_sha256": hashlib.sha256((ROOT / "build/godrome-acceptance").read_bytes()).hexdigest(), "production_binary_sha256": hashlib.sha256((ROOT / "build/godrome").read_bytes()).hexdigest()}
        metadata["display"] = os.environ.get("GODROME_MEASUREMENT_DISPLAY", "current-desktop")
        metadata["kernel"] = os.uname().release
        metadata["outputs"] = json.loads(subprocess.check_output(["swaymsg", "-t", "get_outputs"], text=True))
        (output / "method.json").write_text(json.dumps(metadata, indent=2) + "\n")
        for scenario in args.scenarios:
            env = os.environ.copy()
            env.pop("MYGO_GPU", None)
            native_scenario = scenario
            if scenario.endswith("-cpu"):
                env["MYGO_GPU"] = "0"
                native_scenario = scenario.removesuffix("-cpu")
            elif scenario.endswith("-gpu"):
                env["MYGO_GPU"] = "1"
                native_scenario = scenario.removesuffix("-gpu")
            directory = output / scenario
            if any(directory.glob("run-*.process.csv")):
                raise RuntimeError("refusing to overwrite existing measurement: " + str(directory))
            seconds = args.switch_seconds if scenario == "switch" else args.seconds
            print(f"START {scenario}: {args.runs} × {seconds}s", flush=True)
            command = ["python3", "scripts/measure.py", "--output", str(directory), "--seconds", str(seconds), "--runs", str(args.runs), "--sway-size"]
            if scenario == "empty":
                command += ["--", "./build/godrome", "--empty"]
            else:
                command += ["--acceptance-report", "--", "./build/godrome-acceptance", "--scenario", native_scenario, "--samples", "testdata/acceptance-audio"]
            subprocess.run(command, check=True, env=env)
            print(f"DONE {scenario}", flush=True)
            results = summarize(directory)
            validate(scenario, results, args.runs, seconds)
            (directory / "summary.json").write_text(json.dumps(results, indent=2) + "\n")
    summaries = {directory.name: summarize(directory) for directory in sorted(output.iterdir()) if directory.is_dir() and any(directory.glob("run-*.process.csv"))}
    missing = set(args.scenarios) - set(summaries)
    if missing:
        raise RuntimeError("missing scenarios: " + ", ".join(sorted(missing)))
    for scenario, results in summaries.items():
        validate(scenario, results, args.runs, args.switch_seconds if scenario == "switch" else args.seconds)
    (output / "summary.json").write_text(json.dumps(summaries, indent=2) + "\n")


if __name__ == "__main__":
    main()
