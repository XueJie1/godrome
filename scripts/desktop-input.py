#!/usr/bin/env python3
"""Target only the native acceptance window on Sway; no account input/logging."""
import argparse
import json
from pathlib import Path
import subprocess
import time

parser = argparse.ArgumentParser()
parser.add_argument("action", choices=["snapshot", "key", "click", "drag", "close"])
parser.add_argument("values", nargs="*")
args = parser.parse_args()


def walk(node):
    if node.get("name", "") in ("Godrome 验收 · desktop", "Godrome 验收 · live"):
        yield node
    for child in node.get("nodes", []) + node.get("floating_nodes", []):
        yield from walk(child)


nodes = list(walk(json.loads(subprocess.check_output(["swaymsg", "-t", "get_tree"], text=True))))
if len(nodes) != 1:
    raise RuntimeError("expected exactly one acceptance window")
node = nodes[0]
subprocess.run(["swaymsg", f"[con_id={node['id']}] floating enable, resize set width 1080 height 720, move position 228 92, focus"], check=True, stdout=subprocess.DEVNULL)
time.sleep(0.3)  # Sway acknowledges commands before the layout/frame is committed.
node = next(walk(json.loads(subprocess.check_output(["swaymsg", "-t", "get_tree"], text=True))))
r = node["rect"]
window = node.get("window_rect", {})
x, y = r["x"] + window.get("x", 0), r["y"] + window.get("y", 0)
if args.action == "snapshot":
    output = Path(args.values[0])
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["grim", "-g", f"{r['x']},{r['y']} {r['width']}x{r['height']}", str(output)], check=True)
    print(json.dumps({"window": r, "content_offset": window, "pid": node["pid"]}))
elif args.action == "key":
    command = ["wtype"]
    for key in args.values:
        command.extend(["-k", key])
    subprocess.run(command, check=True)
elif args.action in ("click", "drag"):
    coords = [int(value) for value in args.values]
    def pointer(command):
        subprocess.run(["swaymsg", "seat seat0 cursor " + command], check=True, stdout=subprocess.DEVNULL)
    pointer(f"set {x + coords[0]} {y + coords[1]}")
    time.sleep(0.1)
    if args.action == "click":
        pointer("press button1")
        pointer("release button1")
    else:
        pointer("press button1")
        try:
            for step in range(1, 11):
                xx = x + round(coords[0] + (coords[2] - coords[0]) * step / 10)
                yy = y + round(coords[1] + (coords[3] - coords[1]) * step / 10)
                pointer(f"set {xx} {yy}")
                time.sleep(0.03)
        finally:
            pointer("release button1")
else:
    subprocess.run(["swaymsg", f"[con_id={node['id']}] kill"], check=True)
