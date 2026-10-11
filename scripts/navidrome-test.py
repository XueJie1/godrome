#!/usr/bin/env python3
"""Disposable loopback Navidrome library; takes an already installed binary."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import socket
import subprocess
import tempfile
import time
from urllib.parse import urlencode
from urllib.request import urlopen

parser = argparse.ArgumentParser()
parser.add_argument("binary", help="Navidrome 0.64.2 binary (not downloaded by this script)")
args = parser.parse_args()
binary = str(Path(args.binary).resolve())
root = Path(__file__).resolve().parents[1]
with tempfile.TemporaryDirectory(prefix="godrome-navidrome-") as tmp:
    tmp = Path(tmp)
    music = tmp / "music"
    subprocess.run(["sh", str(root / "scripts/audio-samples.sh"), str(music)], check=True)
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
    password = secrets.token_hex(24)
    env = os.environ.copy()
    env.update(ND_DEVAUTOCREATEADMINPASSWORD=password, ND_ENABLEEXTERNALSERVICES="false")
    proc = subprocess.Popen([binary, "--address", "127.0.0.1", "--port", str(port), "--baseurl", "/music", "--musicfolder", str(music), "--datafolder", str(tmp / "data"), "--loglevel", "error", "--nobanner"], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        server = f"http://127.0.0.1:{port}/music"
        salt = secrets.token_hex(16)
        query = urlencode(dict(u="admin", s=salt, t=hashlib.md5((password+salt).encode()).hexdigest(), v="1.16.1", c="godrome-fixture", f="json", type="alphabeticalByName", size=100))
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            if proc.poll() is not None:
                raise RuntimeError("temporary Navidrome failed to start")
            try:
                with urlopen(server + "/rest/getAlbumList2.view?" + query, timeout=2) as response:
                    body = json.load(response)["subsonic-response"]
                    if body.get("status") == "ok" and body.get("albumList2", {}).get("album"):
                        break
            except Exception:
                pass
            time.sleep(.5)
        else:
            raise RuntimeError("temporary Navidrome did not scan the sample library")
        env = os.environ.copy()
        env.update(GODROME_TEST_SERVER=server, GODROME_TEST_USER="admin", GODROME_TEST_PASSWORD=password)
        subprocess.run(["go", "test", "-race", "-run", "^TestRealNavidrome$", "-v", "."], cwd=root, env=env, check=True)
    finally:
        proc.terminate()
        proc.wait(timeout=10)
