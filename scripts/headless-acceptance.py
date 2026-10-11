#!/usr/bin/env python3
"""Run native acceptance on a private headless Sway, never the user's desktop."""
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time

root = Path(__file__).resolve().parents[1]
runtime = Path(os.environ["XDG_RUNTIME_DIR"])
before = {p.name for p in runtime.glob("wayland-*") if p.is_socket()}
with tempfile.TemporaryDirectory(prefix="godrome-headless-") as tmp:
    tmp = Path(tmp)
    config = tmp / "sway.conf"
    config.write_text("xwayland disable\noutput HEADLESS-1 mode 1920x1080@60Hz scale 1.25\ndefault_border pixel 2\nfor_window [app_id=\"godrome.*\"] floating enable, resize set width 1080 height 720, move position 228 92\n")
    env = os.environ.copy()
    for name in ("DISPLAY", "WAYLAND_DISPLAY", "SWAYSOCK"):
        env.pop(name, None)
    env.update(WLR_BACKENDS="headless", WLR_LIBINPUT_NO_DEVICES="1", WLR_RENDERER="gles2")
    log = tmp / "compositor.log"
    with log.open("w") as stderr:
        compositor = subprocess.Popen(["sway", "-c", str(config)], env=env, stdout=stderr, stderr=stderr)
    socket = runtime / f"sway-ipc.{os.getuid()}.{compositor.pid}.sock"
    try:
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if compositor.poll() is not None:
                raise RuntimeError("private compositor failed: " + log.read_text()[-3000:])
            displays = {p.name for p in runtime.glob("wayland-*") if p.is_socket()} - before
            if socket.exists() and len(displays) == 1:
                break
            time.sleep(.1)
        else:
            raise RuntimeError("could not identify private compositor sockets")
        env.update(SWAYSOCK=str(socket), WAYLAND_DISPLAY=displays.pop(), GDK_BACKEND="wayland", GODROME_MEASUREMENT_DISPLAY="private-headless-sway")
        # The explicit private socket prevents falling back to the real desktop.
        outputs = json.loads(subprocess.check_output(["swaymsg", "-s", str(socket), "-t", "get_outputs"], env=env, text=True))
        if not outputs or any(not out["name"].startswith("HEADLESS-") for out in outputs):
            raise RuntimeError("refusing to operate a physical display")
        print("Private headless display ready; user desktop is untouched", flush=True)
        subprocess.run(["python3", "scripts/acceptance.py", *sys.argv[1:]], cwd=root, env=env, check=True)
    finally:
        compositor.terminate()
        compositor.wait(timeout=10)
